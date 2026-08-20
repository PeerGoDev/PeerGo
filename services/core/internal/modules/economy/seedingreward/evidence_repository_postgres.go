package seedingreward

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/settlementseedingv1"
	"github.com/peergo/peergo/services/core/internal/generated/seedingrewarddb"
)

type PostgresEvidenceRepository struct {
	pool          *pgxpool.Pool
	now           func() time.Time
	maxFutureSkew time.Duration
}

func NewPostgresEvidenceRepository(pool *pgxpool.Pool, now func() time.Time, maxFutureSkew time.Duration) (*PostgresEvidenceRepository, error) {
	if pool == nil || maxFutureSkew < 0 || maxFutureSkew > 10*time.Minute {
		return nil, ErrInput
	}
	if now == nil {
		now = time.Now
	}
	return &PostgresEvidenceRepository{pool: pool, now: now, maxFutureSkew: maxFutureSkew}, nil
}

// ApplyEvidence persists one canonical transport chunk. It acknowledges no
// usable evidence until every chunk, item and the chunk-independent projection
// digest agree in one Core transaction.
func (repository *PostgresEvidenceRepository) ApplyEvidence(ctx context.Context, payload []byte, receivedAt time.Time) (EvidenceApplyResult, error) {
	event, err := settlementseedingv1.Decode(payload)
	if err != nil || receivedAt.IsZero() {
		return EvidenceApplyResult{}, ErrInput
	}
	eventID, err := uuid.Parse(event.EventID)
	if err != nil {
		return EvidenceApplyResult{}, ErrInput
	}
	snapshotID, err := uuid.Parse(event.SnapshotID)
	if err != nil {
		return EvidenceApplyResult{}, ErrInput
	}
	windowDigest, err := hex.DecodeString(event.WindowEvidenceSHA256)
	if err != nil || len(windowDigest) != sha256.Size {
		return EvidenceApplyResult{}, ErrInput
	}
	projectionDigest, err := hex.DecodeString(event.ProjectionSHA256)
	if err != nil || len(projectionDigest) != sha256.Size {
		return EvidenceApplyResult{}, ErrInput
	}
	receivedAt = receivedAt.UTC().Truncate(time.Microsecond)
	if event.BuiltAt.After(receivedAt.Add(repository.maxFutureSkew)) {
		return EvidenceApplyResult{}, ErrInput
	}
	appliedAt := repository.now().UTC().Truncate(time.Microsecond)
	if appliedAt.Before(receivedAt) {
		appliedAt = receivedAt
	}
	payloadDigest := sha256.Sum256(payload)

	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return EvidenceApplyResult{}, fmt.Errorf("begin Core seeding evidence projection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := seedingrewarddb.New(tx)
	_, err = queries.InsertSeedingRewardEvidenceInbox(ctx, seedingrewarddb.InsertSeedingRewardEvidenceInboxParams{
		EventID: eventID, WindowStart: evidenceTimestamp(event.WindowStart), ChunkIndex: event.ChunkIndex,
		PayloadSha256: payloadDigest[:], PayloadJson: string(payload),
		ReceivedAt: evidenceTimestamp(receivedAt), AppliedAt: evidenceTimestamp(appliedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := queries.GetSeedingRewardEvidenceInbox(ctx, eventID)
		if getErr != nil {
			return EvidenceApplyResult{}, classifyEvidenceDatabaseError("read duplicate Core seeding evidence inbox", getErr)
		}
		if !existing.WindowStart.Valid || !existing.WindowStart.Time.Equal(event.WindowStart) ||
			existing.ChunkIndex != event.ChunkIndex || !bytes.Equal(existing.PayloadSha256, payloadDigest[:]) ||
			existing.PayloadJson != string(payload) {
			return EvidenceApplyResult{}, ErrEvidenceConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return EvidenceApplyResult{}, classifyEvidenceDatabaseError("commit duplicate Core seeding evidence", err)
		}
		return EvidenceApplyResult{EventID: eventID, WindowStart: event.WindowStart, Duplicate: true}, nil
	}
	if err != nil {
		return EvidenceApplyResult{}, classifyEvidenceDatabaseError("insert Core seeding evidence inbox", err)
	}

	_, err = queries.InsertSeedingRewardEvidenceWindow(ctx, seedingrewarddb.InsertSeedingRewardEvidenceWindowParams{
		WindowStart: evidenceTimestamp(event.WindowStart), WindowEnd: evidenceTimestamp(event.WindowEnd),
		BuiltAt: evidenceTimestamp(event.BuiltAt), WindowEvidenceSha256: windowDigest,
		ProjectionSha256: projectionDigest, SnapshotID: snapshotID, SnapshotSequence: event.SnapshotSequence,
		SnapshotObservedAt: evidenceTimestamp(event.SnapshotObservedAt), ItemCount: event.ItemCount,
		ChunkCount: event.ChunkCount, CreatedAt: evidenceTimestamp(appliedAt),
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return EvidenceApplyResult{}, classifyEvidenceDatabaseError("insert Core seeding evidence window", err)
	}
	window, err := queries.GetSeedingRewardEvidenceWindowForUpdate(ctx, evidenceTimestamp(event.WindowStart))
	if err != nil {
		return EvidenceApplyResult{}, classifyEvidenceDatabaseError("lock Core seeding evidence window", err)
	}
	if !sameEvidenceHeader(window, event, windowDigest, projectionDigest, snapshotID) {
		return EvidenceApplyResult{}, ErrEvidenceConflict
	}

	_, err = queries.InsertSeedingRewardEvidenceChunk(ctx, seedingrewarddb.InsertSeedingRewardEvidenceChunkParams{
		WindowStart: evidenceTimestamp(event.WindowStart), ChunkIndex: event.ChunkIndex,
		EventID: eventID, PayloadSha256: payloadDigest[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := queries.GetSeedingRewardEvidenceChunk(ctx, seedingrewarddb.GetSeedingRewardEvidenceChunkParams{
			WindowStart: evidenceTimestamp(event.WindowStart), ChunkIndex: event.ChunkIndex,
		})
		if getErr != nil {
			return EvidenceApplyResult{}, classifyEvidenceDatabaseError("read duplicate Core seeding evidence chunk", getErr)
		}
		if existing.EventID != eventID || !bytes.Equal(existing.PayloadSha256, payloadDigest[:]) {
			return EvidenceApplyResult{}, ErrEvidenceConflict
		}
		return EvidenceApplyResult{}, ErrInvariant
	}
	if err != nil {
		return EvidenceApplyResult{}, classifyEvidenceDatabaseError("insert Core seeding evidence chunk", err)
	}
	for _, item := range event.Items {
		userID, parseErr := uuid.Parse(item.UserID)
		itemDigest, digestErr := hex.DecodeString(item.EvidenceSHA256)
		if parseErr != nil || digestErr != nil || len(itemDigest) != sha256.Size {
			return EvidenceApplyResult{}, ErrInput
		}
		if err := queries.InsertSeedingRewardEvidenceItem(ctx, seedingrewarddb.InsertSeedingRewardEvidenceItemParams{
			WindowStart: evidenceTimestamp(event.WindowStart), UserID: userID, TorrentID: item.TorrentID,
			ActiveSeconds: item.ActiveSeconds, RawUploadedBytes: item.RawUploadedBytes,
			SnapshotSeeders: item.SnapshotSeeders, SnapshotLeechers: item.SnapshotLeechers,
			TrackerEvidenceSha256: itemDigest,
		}); err != nil {
			return EvidenceApplyResult{}, classifyEvidenceDatabaseError("insert Core seeding evidence item", err)
		}
	}
	progress, err := queries.IncrementSeedingRewardEvidenceChunks(ctx, evidenceTimestamp(event.WindowStart))
	if err != nil {
		return EvidenceApplyResult{}, classifyEvidenceDatabaseError("advance Core seeding evidence chunk count", err)
	}
	if progress.ReceivedChunkCount < progress.ChunkCount {
		if err := tx.Commit(ctx); err != nil {
			return EvidenceApplyResult{}, classifyEvidenceDatabaseError("commit partial Core seeding evidence", err)
		}
		return EvidenceApplyResult{EventID: eventID, WindowStart: event.WindowStart}, nil
	}
	if progress.ReceivedChunkCount != progress.ChunkCount {
		return EvidenceApplyResult{}, ErrInvariant
	}
	rows, err := queries.ListSeedingRewardEvidenceItems(ctx, evidenceTimestamp(event.WindowStart))
	if err != nil || len(rows) != int(event.ItemCount) {
		if err != nil {
			return EvidenceApplyResult{}, classifyEvidenceDatabaseError("list complete Core seeding evidence", err)
		}
		return EvidenceApplyResult{}, ErrInvariant
	}
	items := make([]settlementseedingv1.Item, len(rows))
	for index, row := range rows {
		if row.UserID == uuid.Nil || len(row.TrackerEvidenceSha256) != sha256.Size {
			return EvidenceApplyResult{}, ErrInvariant
		}
		items[index] = settlementseedingv1.Item{
			UserID: row.UserID.String(), TorrentID: row.TorrentID, ActiveSeconds: row.ActiveSeconds,
			RawUploadedBytes: row.RawUploadedBytes, SnapshotSeeders: row.SnapshotSeeders,
			SnapshotLeechers: row.SnapshotLeechers,
			EvidenceSHA256:   hex.EncodeToString(row.TrackerEvidenceSha256),
		}
	}
	verified, err := settlementseedingv1.ProjectionDigest(event, items)
	if err != nil || !bytes.Equal(verified[:], projectionDigest) {
		return EvidenceApplyResult{}, ErrEvidenceConflict
	}
	changed, err := queries.MarkSeedingRewardEvidenceComplete(ctx, seedingrewarddb.MarkSeedingRewardEvidenceCompleteParams{
		CompletedAt: evidenceTimestamp(appliedAt), WindowStart: evidenceTimestamp(event.WindowStart),
	})
	if err != nil || changed != 1 {
		return EvidenceApplyResult{}, classifyEvidenceRowsError("complete Core seeding evidence", changed, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return EvidenceApplyResult{}, classifyEvidenceDatabaseError("commit complete Core seeding evidence", err)
	}
	return EvidenceApplyResult{EventID: eventID, WindowStart: event.WindowStart, Complete: true}, nil
}

func sameEvidenceHeader(row seedingrewarddb.GetSeedingRewardEvidenceWindowForUpdateRow, event settlementseedingv1.Event, windowDigest, projectionDigest []byte, snapshotID uuid.UUID) bool {
	return row.WindowStart.Valid && row.WindowStart.Time.Equal(event.WindowStart) && row.WindowEnd.Valid && row.WindowEnd.Time.Equal(event.WindowEnd) &&
		row.BuiltAt.Valid && row.BuiltAt.Time.Equal(event.BuiltAt) && bytes.Equal(row.WindowEvidenceSha256, windowDigest) &&
		bytes.Equal(row.ProjectionSha256, projectionDigest) && row.SnapshotID == snapshotID && row.SnapshotSequence == event.SnapshotSequence &&
		row.SnapshotObservedAt.Valid && row.SnapshotObservedAt.Time.Equal(event.SnapshotObservedAt) &&
		row.ItemCount == event.ItemCount && row.ChunkCount == event.ChunkCount && row.Status == "collecting"
}

func evidenceTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC().Truncate(time.Microsecond), Valid: true}
}

func classifyEvidenceRowsError(operation string, rows int64, err error) error {
	if err != nil {
		return classifyEvidenceDatabaseError(operation, err)
	}
	if rows != 1 {
		return fmt.Errorf("%w: %s affected %d rows", ErrInvariant, operation, rows)
	}
	return nil
}

func classifyEvidenceDatabaseError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		if postgresError.Code == "23505" {
			return fmt.Errorf("%w: %s: %v", ErrEvidenceConflict, operation, err)
		}
		if strings.HasPrefix(postgresError.Code, "22") || strings.HasPrefix(postgresError.Code, "23") || postgresError.Code == "P0001" {
			return fmt.Errorf("%w: %s: %v", ErrInvariant, operation, err)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ EvidenceProjector = (*PostgresEvidenceRepository)(nil)
