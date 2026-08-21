package swarmprojection

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/core/internal/generated/swarmdb"
)

type PostgresRepository struct {
	pool          *pgxpool.Pool
	now           func() time.Time
	maxFutureSkew time.Duration
}

func NewPostgresRepository(pool *pgxpool.Pool, now func() time.Time, maxFutureSkew time.Duration) (*PostgresRepository, error) {
	if pool == nil || maxFutureSkew < 0 || maxFutureSkew > 10*time.Minute {
		return nil, ErrInput
	}
	if now == nil {
		now = time.Now
	}
	return &PostgresRepository{pool: pool, now: now, maxFutureSkew: maxFutureSkew}, nil
}

// ApplySnapshot stores one canonical chunk and applies the public peer-count
// projection only when every chunk of a strictly newer full snapshot exists in
// the same transaction. A missing or corrupt chunk therefore cannot zero the
// last known good projection.
func (repository *PostgresRepository) ApplySnapshot(ctx context.Context, payload []byte, receivedAt time.Time) (ApplyResult, error) {
	chunk, err := trackerswarmv1.Decode(payload)
	if err != nil || receivedAt.IsZero() {
		return ApplyResult{}, ErrInput
	}
	eventID, err := uuid.Parse(chunk.EventID)
	if err != nil {
		return ApplyResult{}, ErrInput
	}
	snapshotID, err := uuid.Parse(chunk.SnapshotID)
	if err != nil {
		return ApplyResult{}, ErrInput
	}
	receivedAt = canonicalSwarmTime(receivedAt)
	observedAt := canonicalSwarmTime(chunk.ObservedAt)
	// Events published before Tracker canonicalized its clock may contain
	// nanoseconds. Keep ordering and persistence on the same database-safe value.
	chunk.ObservedAt = observedAt
	if observedAt.After(receivedAt.Add(repository.maxFutureSkew)) {
		return ApplyResult{}, ErrInput
	}
	appliedAt := canonicalSwarmTime(repository.now())
	if appliedAt.Before(receivedAt) {
		appliedAt = receivedAt
	}
	payloadDigest := sha256.Sum256(payload)

	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ApplyResult{}, fmt.Errorf("begin Core swarm snapshot projection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := swarmdb.New(tx)
	_, err = queries.InsertSwarmSnapshotInbox(ctx, swarmdb.InsertSwarmSnapshotInboxParams{
		EventID: eventID, SnapshotID: snapshotID, PayloadSha256: payloadDigest[:], ReceivedAt: swarmTimestamp(receivedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := queries.GetSwarmSnapshotInbox(ctx, eventID)
		if getErr != nil {
			return ApplyResult{}, classifyDatabaseError("read duplicate Core swarm snapshot inbox", getErr)
		}
		if existing.SnapshotID != snapshotID || !bytes.Equal(existing.PayloadSha256, payloadDigest[:]) {
			return ApplyResult{}, ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return ApplyResult{}, classifyDatabaseError("commit duplicate Core swarm snapshot", err)
		}
		return ApplyResult{EventID: eventID, SnapshotID: snapshotID, Duplicate: true}, nil
	}
	if err != nil {
		return ApplyResult{}, classifyDatabaseError("insert Core swarm snapshot inbox", err)
	}

	state, err := queries.GetSwarmProjectionStateForUpdate(ctx)
	if err != nil {
		return ApplyResult{}, classifyDatabaseError("lock Core swarm projection state", err)
	}
	obsolete, orderErr := snapshotOrder(state, chunk, snapshotID)
	if orderErr != nil {
		return ApplyResult{}, orderErr
	}
	if obsolete {
		if err := tx.Commit(ctx); err != nil {
			return ApplyResult{}, classifyDatabaseError("commit obsolete Core swarm snapshot chunk", err)
		}
		return ApplyResult{EventID: eventID, SnapshotID: snapshotID, Obsolete: true}, nil
	}

	_, err = queries.InsertSwarmSnapshotRun(ctx, swarmdb.InsertSwarmSnapshotRunParams{
		SnapshotID: snapshotID, SourceID: chunk.SourceID, RoutingEpoch: chunk.RoutingEpoch,
		SnapshotSequence: chunk.SnapshotSequence, ObservedAt: swarmTimestamp(observedAt),
		ChunkCount: chunk.ChunkCount, CreatedAt: swarmTimestamp(appliedAt),
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ApplyResult{}, classifyDatabaseError("insert Core swarm snapshot run", err)
	}
	run, err := queries.GetSwarmSnapshotRunForUpdate(ctx, snapshotID)
	if err != nil {
		return ApplyResult{}, classifyDatabaseError("lock Core swarm snapshot run", err)
	}
	if run.SourceID != chunk.SourceID || run.RoutingEpoch != chunk.RoutingEpoch ||
		run.SnapshotSequence != chunk.SnapshotSequence || !run.ObservedAt.Valid ||
		!run.ObservedAt.Time.Equal(observedAt) || run.ChunkCount != chunk.ChunkCount {
		return ApplyResult{}, ErrConflict
	}
	if run.Status != "collecting" {
		if err := tx.Commit(ctx); err != nil {
			return ApplyResult{}, classifyDatabaseError("commit finished Core swarm snapshot chunk", err)
		}
		return ApplyResult{EventID: eventID, SnapshotID: snapshotID, Obsolete: true}, nil
	}

	_, err = queries.InsertSwarmSnapshotChunk(ctx, swarmdb.InsertSwarmSnapshotChunkParams{
		SnapshotID: snapshotID, ChunkIndex: chunk.ChunkIndex, EventID: eventID, PayloadSha256: payloadDigest[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := queries.GetSwarmSnapshotChunk(ctx, swarmdb.GetSwarmSnapshotChunkParams{SnapshotID: snapshotID, ChunkIndex: chunk.ChunkIndex})
		if getErr != nil {
			return ApplyResult{}, classifyDatabaseError("read duplicate Core swarm snapshot chunk", getErr)
		}
		if existing.EventID != eventID || !bytes.Equal(existing.PayloadSha256, payloadDigest[:]) {
			return ApplyResult{}, ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return ApplyResult{}, classifyDatabaseError("commit duplicate Core swarm snapshot chunk", err)
		}
		return ApplyResult{EventID: eventID, SnapshotID: snapshotID, Duplicate: true}, nil
	}
	if err != nil {
		return ApplyResult{}, classifyDatabaseError("insert Core swarm snapshot chunk", err)
	}
	for _, entry := range chunk.Entries {
		infoHash, decodeErr := hex.DecodeString(entry.InfoHashV1)
		if decodeErr != nil || len(infoHash) != 20 {
			return ApplyResult{}, ErrInput
		}
		if err := queries.InsertSwarmSnapshotEntry(ctx, swarmdb.InsertSwarmSnapshotEntryParams{
			SnapshotID: snapshotID, InfoHashV1: infoHash, Seeders: entry.Seeders, Leechers: entry.Leechers,
		}); err != nil {
			return ApplyResult{}, classifyDatabaseError("insert Core swarm snapshot entry", err)
		}
	}
	progress, err := queries.IncrementSwarmSnapshotReceivedChunks(ctx, snapshotID)
	if err != nil {
		return ApplyResult{}, classifyDatabaseError("advance Core swarm snapshot chunk count", err)
	}
	if progress.ReceivedChunkCount < progress.ChunkCount {
		if err := tx.Commit(ctx); err != nil {
			return ApplyResult{}, classifyDatabaseError("commit partial Core swarm snapshot", err)
		}
		return ApplyResult{EventID: eventID, SnapshotID: snapshotID}, nil
	}
	if progress.ReceivedChunkCount != progress.ChunkCount {
		return ApplyResult{}, ErrInvariant
	}
	if err := queries.ApplyCompleteSwarmSnapshot(ctx, swarmdb.ApplyCompleteSwarmSnapshotParams{
		ObservedAt: swarmTimestamp(observedAt), SnapshotID: snapshotID,
	}); err != nil {
		return ApplyResult{}, classifyDatabaseError("apply complete Core swarm snapshot", err)
	}
	rows, err := queries.AdvanceSwarmProjectionState(ctx, swarmdb.AdvanceSwarmProjectionStateParams{
		SourceID: chunk.SourceID, RoutingEpoch: chunk.RoutingEpoch, SnapshotSequence: chunk.SnapshotSequence,
		SnapshotID: snapshotID, ObservedAt: swarmTimestamp(observedAt), AppliedAt: swarmTimestamp(appliedAt),
	})
	if err != nil || rows != 1 {
		return ApplyResult{}, classifyRowsError("advance Core swarm projection state", rows, err)
	}
	rows, err = queries.MarkSwarmSnapshotApplied(ctx, swarmdb.MarkSwarmSnapshotAppliedParams{AppliedAt: swarmTimestamp(appliedAt), SnapshotID: snapshotID})
	if err != nil || rows != 1 {
		return ApplyResult{}, classifyRowsError("mark Core swarm snapshot applied", rows, err)
	}
	if err := queries.SupersedeOlderSwarmSnapshotRuns(ctx, swarmdb.SupersedeOlderSwarmSnapshotRunsParams{
		RoutingEpoch: chunk.RoutingEpoch, SourceID: chunk.SourceID, SnapshotSequence: chunk.SnapshotSequence,
	}); err != nil {
		return ApplyResult{}, classifyDatabaseError("supersede older Core swarm snapshot runs", err)
	}
	if err := queries.DeleteFinishedSwarmSnapshotEntries(ctx); err != nil {
		return ApplyResult{}, classifyDatabaseError("delete finished Core swarm snapshot entries", err)
	}
	if err := queries.DeleteFinishedSwarmSnapshotChunks(ctx); err != nil {
		return ApplyResult{}, classifyDatabaseError("delete finished Core swarm snapshot chunks", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplyResult{}, classifyDatabaseError("commit complete Core swarm snapshot", err)
	}
	return ApplyResult{EventID: eventID, SnapshotID: snapshotID, Applied: true}, nil
}

// ApplyCompletion increments the lifetime completion counter only for announce
// events whose completion identity was derived by the Swarm Engine. A client
// retry has a new event ID but the same completion ID, so it is acknowledged as
// a duplicate without incrementing again.
func (repository *PostgresRepository) ApplyCompletion(ctx context.Context, payload []byte, receivedAt time.Time) (ApplyResult, error) {
	event, err := trackerannouncev1.Decode(payload)
	if err != nil || receivedAt.IsZero() {
		return ApplyResult{}, ErrInput
	}
	eventID, err := uuid.Parse(event.EventID)
	if err != nil {
		return ApplyResult{}, ErrInput
	}
	if event.CompletionID == "" {
		return ApplyResult{EventID: eventID, Noop: true}, nil
	}
	receivedAt = canonicalSwarmTime(receivedAt)
	occurredAt := canonicalSwarmTime(event.ReceivedAt)
	if occurredAt.After(receivedAt.Add(repository.maxFutureSkew)) {
		return ApplyResult{}, ErrInput
	}
	completionID, err := hex.DecodeString(event.CompletionID)
	if err != nil || len(completionID) != sha256.Size {
		return ApplyResult{}, ErrInput
	}
	infoHash, err := trackerannouncev1.DecodeInfoHashV1(event.InfoHashV1)
	if err != nil {
		return ApplyResult{}, ErrInput
	}
	payloadDigest := sha256.Sum256(payload)
	appliedAt := canonicalSwarmTime(repository.now())
	if appliedAt.Before(receivedAt) {
		appliedAt = receivedAt
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ApplyResult{}, fmt.Errorf("begin Core swarm completion projection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := swarmdb.New(tx)
	if existing, getErr := queries.GetSwarmCompletionByIdentity(ctx, completionID); getErr == nil {
		if existing.TorrentID != event.TorrentID {
			return ApplyResult{}, ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return ApplyResult{}, classifyDatabaseError("commit duplicate Core swarm completion", err)
		}
		return ApplyResult{EventID: eventID, Duplicate: true}, nil
	} else if !errors.Is(getErr, pgx.ErrNoRows) {
		return ApplyResult{}, classifyDatabaseError("read Core swarm completion identity", getErr)
	}
	publicTorrentID, err := queries.ResolvePublishedTorrentForCompletion(ctx, swarmdb.ResolvePublishedTorrentForCompletionParams{
		TorrentID: event.TorrentID, InfoHashV1: infoHash[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ApplyResult{}, ErrInvariant
	}
	if err != nil {
		return ApplyResult{}, classifyDatabaseError("resolve Core swarm completion torrent", err)
	}
	_, err = queries.InsertSwarmCompletionInbox(ctx, swarmdb.InsertSwarmCompletionInboxParams{
		EventID: eventID, CompletionID: completionID, PayloadSha256: payloadDigest[:], TorrentID: event.TorrentID,
		OccurredAt: swarmTimestamp(occurredAt), AppliedAt: swarmTimestamp(appliedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := queries.GetSwarmCompletionByEvent(ctx, eventID)
		if getErr != nil {
			return ApplyResult{}, classifyDatabaseError("read duplicate Core swarm completion event", getErr)
		}
		if existing.TorrentID != event.TorrentID || !bytes.Equal(existing.CompletionID, completionID) ||
			!bytes.Equal(existing.PayloadSha256, payloadDigest[:]) {
			return ApplyResult{}, ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return ApplyResult{}, classifyDatabaseError("commit duplicate Core swarm completion event", err)
		}
		return ApplyResult{EventID: eventID, Duplicate: true}, nil
	}
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return ApplyResult{}, ErrConflict
		}
		return ApplyResult{}, classifyDatabaseError("insert Core swarm completion inbox", err)
	}
	completed, err := queries.IncrementTorrentCompletionStats(ctx, swarmdb.IncrementTorrentCompletionStatsParams{
		TorrentID: publicTorrentID, ObservedAt: swarmTimestamp(occurredAt),
	})
	if err != nil {
		return ApplyResult{}, classifyDatabaseError("increment Core torrent completion stats", err)
	}
	if err := queries.SyncTorrentSwarmCompleted(ctx, swarmdb.SyncTorrentSwarmCompletedParams{
		Completed: completed, TorrentID: publicTorrentID,
	}); err != nil {
		return ApplyResult{}, classifyDatabaseError("synchronize Core public swarm completion count", err)
	}
	completionSequence, err := queries.AdvanceTrackerCompletionSequence(ctx)
	if err != nil || completionSequence < 1 {
		if err == nil {
			err = ErrInvariant
		}
		return ApplyResult{}, classifyDatabaseError("advance Tracker completion sequence", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplyResult{}, classifyDatabaseError("commit Core swarm completion projection", err)
	}
	return ApplyResult{EventID: eventID, Applied: true}, nil
}

func snapshotOrder(state swarmdb.GetSwarmProjectionStateForUpdateRow, chunk trackerswarmv1.SnapshotChunk, snapshotID uuid.UUID) (bool, error) {
	if state.RoutingEpoch < 0 || state.SnapshotSequence < 0 || (state.SnapshotSequence == 0) != !state.SnapshotID.Valid ||
		(state.SnapshotSequence == 0) != !state.ObservedAt.Valid {
		return false, ErrInvariant
	}
	if state.SnapshotSequence == 0 {
		return false, nil
	}
	if chunk.RoutingEpoch < state.RoutingEpoch {
		return true, nil
	}
	if chunk.RoutingEpoch > state.RoutingEpoch {
		return false, nil
	}
	if state.SourceID != chunk.SourceID {
		return false, ErrConflict
	}
	if chunk.SnapshotSequence < state.SnapshotSequence {
		return true, nil
	}
	if chunk.SnapshotSequence == state.SnapshotSequence {
		if uuid.UUID(state.SnapshotID.Bytes) != snapshotID {
			return false, ErrConflict
		}
		return true, nil
	}
	if state.ObservedAt.Valid && chunk.ObservedAt.Before(state.ObservedAt.Time) {
		return false, ErrConflict
	}
	return false, nil
}

func swarmTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: canonicalSwarmTime(value), Valid: true}
}

// PostgreSQL timestamptz and pgx preserve microseconds, not Go's full
// nanosecond precision. All values that cross this boundary must use the same
// representation or an insert-then-read replay can be mistaken for a conflict.
func canonicalSwarmTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func classifyRowsError(operation string, rows int64, err error) error {
	if err != nil {
		return classifyDatabaseError(operation, err)
	}
	if rows != 1 {
		return fmt.Errorf("%s: %w", operation, ErrInvariant)
	}
	return nil
}

func classifyDatabaseError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return fmt.Errorf("%s: %w", operation, ErrConflict)
		case "23503", "23514", "22001", "22003":
			return fmt.Errorf("%s: %w", operation, ErrInvariant)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
