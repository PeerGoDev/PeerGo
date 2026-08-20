package seedingevidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/settlementseedingv1"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
)

const snapshotSourceSequenceConstraint = "seeding_swarm_snapshot_inbox_source_stream_source_sequence_key"

var seedingEvidenceEventNamespace = uuid.MustParse("df34c07b-2342-5f03-a9a4-4ba9a1792ab8")

type PostgresRepositoryConfig struct {
	AnnounceStream              string
	SnapshotStream              string
	SnapshotSubject             string
	MaxFutureSkew               time.Duration
	MaximumSnapshotClosureDelay time.Duration
}

type PostgresRepository struct {
	pool   *pgxpool.Pool
	config PostgresRepositoryConfig
	now    func() time.Time
}

func NewPostgresRepository(pool *pgxpool.Pool, config PostgresRepositoryConfig, now func() time.Time) (*PostgresRepository, error) {
	if pool == nil || !trackerannouncev1.ValidStreamName(config.AnnounceStream) ||
		!trackerswarmv1.ValidStreamName(config.SnapshotStream) ||
		!trackerswarmv1.ValidLiteralSubject(config.SnapshotSubject) ||
		config.MaxFutureSkew < 0 || config.MaxFutureSkew > 10*time.Minute ||
		config.MaximumSnapshotClosureDelay < time.Second || config.MaximumSnapshotClosureDelay > time.Hour {
		return nil, ErrInput
	}
	if now == nil {
		now = time.Now
	}
	return &PostgresRepository{pool: pool, config: config, now: now}, nil
}

// ApplySnapshot stores one canonical full-snapshot chunk. It shares the
// trackerswarmv1 contract with Core but retains complete historical entries;
// a snapshot becomes eligible to close evidence only after every chunk commits.
func (repository *PostgresRepository) ApplySnapshot(ctx context.Context, delivery SnapshotDelivery) (SnapshotApplyResult, error) {
	if delivery.Stream != repository.config.SnapshotStream || delivery.Subject != repository.config.SnapshotSubject ||
		delivery.Sequence == 0 || delivery.Sequence > math.MaxInt64 ||
		delivery.DeliveryCount == 0 || delivery.DeliveryCount > math.MaxInt64 {
		return SnapshotApplyResult{}, ErrInput
	}
	chunk, err := trackerswarmv1.Decode(delivery.Payload)
	if err != nil {
		return SnapshotApplyResult{}, ErrInput
	}
	eventID, err := uuid.Parse(chunk.EventID)
	if err != nil {
		return SnapshotApplyResult{}, ErrInput
	}
	snapshotID, err := uuid.Parse(chunk.SnapshotID)
	if err != nil {
		return SnapshotApplyResult{}, ErrInput
	}
	receivedAt := repository.now().UTC().Round(0)
	observedAt := chunk.ObservedAt.UTC().Round(0)
	if receivedAt.IsZero() || observedAt.After(receivedAt.Add(repository.config.MaxFutureSkew)) {
		return SnapshotApplyResult{}, ErrInput
	}
	payloadDigest := sha256.Sum256(delivery.Payload)

	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return SnapshotApplyResult{}, fmt.Errorf("begin seeding snapshot projection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := ledgerdb.New(tx)
	_, err = queries.InsertSeedingSnapshotInbox(ctx, ledgerdb.InsertSeedingSnapshotInboxParams{
		EventID: eventID, SnapshotID: snapshotID, PayloadSha256: payloadDigest[:],
		SourceStream: delivery.Stream, SourceSubject: delivery.Subject,
		SourceSequence: int64(delivery.Sequence), DeliveryCount: int64(delivery.DeliveryCount),
		ObservedAt: timestamp(observedAt), ReceivedAt: timestamp(receivedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := queries.GetSeedingSnapshotInbox(ctx, eventID)
		if getErr != nil {
			return SnapshotApplyResult{}, databaseError("read duplicate seeding snapshot event", getErr)
		}
		if existing.SnapshotID != snapshotID || !bytes.Equal(existing.PayloadSha256, payloadDigest[:]) ||
			existing.SourceStream != delivery.Stream || existing.SourceSubject != delivery.Subject ||
			existing.SourceSequence != int64(delivery.Sequence) || !sameTimestamp(existing.ObservedAt, observedAt) {
			return SnapshotApplyResult{}, ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return SnapshotApplyResult{}, databaseError("commit duplicate seeding snapshot event", err)
		}
		return SnapshotApplyResult{EventID: eventID, SnapshotID: snapshotID, Duplicate: true}, nil
	}
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.ConstraintName == snapshotSourceSequenceConstraint {
			return SnapshotApplyResult{}, ErrConflict
		}
		return SnapshotApplyResult{}, databaseError("insert seeding snapshot inbox", err)
	}
	if err := queries.LockSeedingSnapshotTimeline(ctx); err != nil {
		return SnapshotApplyResult{}, databaseError("lock seeding snapshot timeline", err)
	}
	latest, latestErr := queries.GetLatestSeedingSnapshotForRoute(ctx, ledgerdb.GetLatestSeedingSnapshotForRouteParams{
		SourceID: chunk.SourceID, RoutingEpoch: chunk.RoutingEpoch,
	})
	if latestErr == nil {
		if !latest.ObservedAt.Valid || chunk.SnapshotSequence < latest.SnapshotSequence ||
			(chunk.SnapshotSequence == latest.SnapshotSequence && latest.SnapshotID != snapshotID) ||
			(chunk.SnapshotSequence > latest.SnapshotSequence && observedAt.Before(latest.ObservedAt.Time)) {
			return SnapshotApplyResult{}, ErrConflict
		}
	} else if !errors.Is(latestErr, pgx.ErrNoRows) {
		return SnapshotApplyResult{}, databaseError("read latest seeding snapshot route", latestErr)
	}
	_, err = queries.InsertSeedingSnapshotRun(ctx, ledgerdb.InsertSeedingSnapshotRunParams{
		SnapshotID: snapshotID, SourceID: chunk.SourceID, RoutingEpoch: chunk.RoutingEpoch,
		SnapshotSequence: chunk.SnapshotSequence, ObservedAt: timestamp(observedAt),
		ChunkCount: chunk.ChunkCount, CreatedAt: timestamp(receivedAt),
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		if isConstraintError(err) {
			return SnapshotApplyResult{}, ErrConflict
		}
		return SnapshotApplyResult{}, databaseError("insert seeding snapshot run", err)
	}
	run, err := queries.GetSeedingSnapshotRunForUpdate(ctx, snapshotID)
	if err != nil {
		return SnapshotApplyResult{}, databaseError("lock seeding snapshot run", err)
	}
	if run.SourceID != chunk.SourceID || run.RoutingEpoch != chunk.RoutingEpoch ||
		run.SnapshotSequence != chunk.SnapshotSequence || !sameTimestamp(run.ObservedAt, observedAt) ||
		run.ChunkCount != chunk.ChunkCount || run.Status != "collecting" {
		return SnapshotApplyResult{}, ErrConflict
	}
	_, err = queries.InsertSeedingSnapshotChunk(ctx, ledgerdb.InsertSeedingSnapshotChunkParams{
		SnapshotID: snapshotID, ChunkIndex: chunk.ChunkIndex, EventID: eventID, PayloadSha256: payloadDigest[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := queries.GetSeedingSnapshotChunk(ctx, ledgerdb.GetSeedingSnapshotChunkParams{
			SnapshotID: snapshotID, ChunkIndex: chunk.ChunkIndex,
		})
		if getErr != nil {
			return SnapshotApplyResult{}, databaseError("read duplicate seeding snapshot chunk", getErr)
		}
		if existing.EventID != eventID || !bytes.Equal(existing.PayloadSha256, payloadDigest[:]) {
			return SnapshotApplyResult{}, ErrConflict
		}
		return SnapshotApplyResult{}, ErrConflict
	}
	if err != nil {
		return SnapshotApplyResult{}, databaseError("insert seeding snapshot chunk", err)
	}
	for _, entry := range chunk.Entries {
		infoHash, decodeErr := hex.DecodeString(entry.InfoHashV1)
		if decodeErr != nil || len(infoHash) != 20 {
			return SnapshotApplyResult{}, ErrInput
		}
		if err := queries.InsertSeedingSnapshotEntry(ctx, ledgerdb.InsertSeedingSnapshotEntryParams{
			SnapshotID: snapshotID, InfoHashV1: infoHash, Seeders: entry.Seeders, Leechers: entry.Leechers,
		}); err != nil {
			if isConstraintError(err) {
				return SnapshotApplyResult{}, ErrConflict
			}
			return SnapshotApplyResult{}, databaseError("insert seeding snapshot entry", err)
		}
	}
	progress, err := queries.IncrementSeedingSnapshotReceivedChunks(ctx, snapshotID)
	if err != nil {
		return SnapshotApplyResult{}, databaseError("advance seeding snapshot chunks", err)
	}
	complete := progress.ReceivedChunkCount == progress.ChunkCount
	if progress.ReceivedChunkCount > progress.ChunkCount {
		return SnapshotApplyResult{}, ErrInvariant
	}
	if complete {
		rows, completeErr := queries.CompleteSeedingSnapshot(ctx, ledgerdb.CompleteSeedingSnapshotParams{
			CompletedAt: timestamp(receivedAt), SnapshotID: snapshotID,
		})
		if completeErr != nil || rows != 1 {
			return SnapshotApplyResult{}, rowsError("complete seeding snapshot", rows, completeErr)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return SnapshotApplyResult{}, databaseError("commit seeding snapshot chunk", err)
	}
	return SnapshotApplyResult{EventID: eventID, SnapshotID: snapshotID, Complete: complete}, nil
}

func (repository *PostgresRepository) NextWindowStart(ctx context.Context, initial time.Time) (time.Time, error) {
	if !validWindowStart(initial) {
		return time.Time{}, ErrInput
	}
	value, err := ledgerdb.New(repository.pool).GetNextSeedingEvidenceWindowStart(ctx, timestamp(initial))
	if err != nil || !value.Valid {
		return time.Time{}, databaseError("read next seeding evidence window", err)
	}
	return value.Time.UTC().Round(0), nil
}

// BuildHour closes one UTC hour against two independent watermarks: a terminal
// announce event at/after window end and a complete swarm snapshot at/after
// window end. The chosen counts come from the latest complete snapshot at or
// before the boundary on that same route.
func (repository *PostgresRepository) BuildHour(ctx context.Context, windowStart, builtAt time.Time) (BuildResult, error) {
	if !validWindowStart(windowStart) || builtAt.IsZero() {
		return BuildResult{}, ErrInput
	}
	windowStart = windowStart.UTC().Round(0)
	windowEnd := windowStart.Add(time.Hour)
	builtAt = builtAt.UTC().Round(0)
	if builtAt.Before(windowEnd) {
		return BuildResult{}, ErrCoveragePending
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return BuildResult{}, fmt.Errorf("begin seeding evidence window: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := ledgerdb.New(tx)
	if err := queries.LockSeedingEvidenceWindow(ctx, timestamp(windowStart)); err != nil {
		return BuildResult{}, databaseError("lock seeding evidence window", err)
	}
	if existing, existingErr := queries.GetSeedingEvidenceWindow(ctx, timestamp(windowStart)); existingErr == nil {
		result, convertErr := existingWindowResult(existing)
		if convertErr != nil {
			return BuildResult{}, convertErr
		}
		result.Duplicate = true
		result.AnomalyCount, err = queries.CountSeedingEvidenceAnomalies(ctx, timestamp(windowStart))
		if err != nil {
			return BuildResult{}, databaseError("count seeding evidence anomalies", err)
		}
		if result.AnomalyCount == 0 {
			if err := repository.ensureEvidenceOutbox(ctx, queries, result); err != nil {
				return BuildResult{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return BuildResult{}, databaseError("commit duplicate seeding evidence window", err)
		}
		if result.AnomalyCount > 0 {
			return result, ErrEvidenceDrift
		}
		return result, nil
	} else if !errors.Is(existingErr, pgx.ErrNoRows) {
		return BuildResult{}, databaseError("read seeding evidence window", existingErr)
	}

	announceFence, err := queries.GetSeedingAnnounceFence(ctx, ledgerdb.GetSeedingAnnounceFenceParams{
		SourceStream: repository.config.AnnounceStream, WindowEnd: timestamp(windowEnd),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return BuildResult{}, ErrCoveragePending
	}
	if err != nil || !announceFence.ReceivedAt.Valid {
		return BuildResult{}, databaseError("read seeding announce fence", err)
	}
	snapshotFence, err := queries.GetSeedingSnapshotFence(ctx, ledgerdb.GetSeedingSnapshotFenceParams{
		WindowEnd: timestamp(windowEnd), SourceStream: repository.config.SnapshotStream,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return BuildResult{}, ErrCoveragePending
	}
	if err != nil || !snapshotFence.ObservedAt.Valid {
		return BuildResult{}, databaseError("read seeding snapshot fence", err)
	}
	if snapshotFence.ObservedAt.Time.After(windowEnd.Add(repository.config.MaximumSnapshotClosureDelay)) {
		return BuildResult{}, ErrCoveragePending
	}
	selected, err := queries.GetSelectedSeedingSnapshot(ctx, ledgerdb.GetSelectedSeedingSnapshotParams{
		SourceID: snapshotFence.SourceID, RoutingEpoch: snapshotFence.RoutingEpoch,
		MaxSnapshotSequence: snapshotFence.SnapshotSequence, WindowEnd: timestamp(windowEnd),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return BuildResult{}, ErrCoveragePending
	}
	if err != nil || !selected.ObservedAt.Valid {
		return BuildResult{}, databaseError("select closing seeding snapshot", err)
	}
	rows, err := queries.ListSeedingIntervalsForWindow(ctx, ledgerdb.ListSeedingIntervalsForWindowParams{
		WindowStart: timestamp(windowStart), WindowEnd: timestamp(windowEnd),
		SourceStream: repository.config.AnnounceStream, AnnounceFenceSequence: announceFence.SourceSequence,
	})
	if err != nil {
		return BuildResult{}, databaseError("list seeding intervals", err)
	}
	facts, err := intervalFactsFromRows(rows)
	if err != nil {
		return BuildResult{}, err
	}
	snapshotRows, err := queries.ListSeedingSnapshotEntries(ctx, selected.SnapshotID)
	if err != nil {
		return BuildResult{}, databaseError("list selected seeding snapshot entries", err)
	}
	snapshot, err := snapshotCountsFromRows(snapshotRows)
	if err != nil {
		return BuildResult{}, err
	}
	items, err := assembleItems(facts, snapshot)
	if err != nil || len(items) > math.MaxInt32 {
		return BuildResult{}, ErrInvariant
	}
	digestItems := make([]windowDigestItem, len(items))
	for index, item := range items {
		digestItems[index] = windowDigestItem{
			UserID: item.UserID.String(), TorrentID: item.TorrentID,
			EvidenceSHA256: hex.EncodeToString(item.EvidenceSHA256[:]),
		}
	}
	document := windowDigestDocument{
		SchemaVersion: SchemaVersion, WindowStart: windowStart, WindowEnd: windowEnd,
		AnnounceSourceStream:    repository.config.AnnounceStream,
		AnnounceFenceSequence:   announceFence.SourceSequence,
		AnnounceFenceReceivedAt: announceFence.ReceivedAt.Time.UTC().Round(0),
		SelectedSnapshotID:      selected.SnapshotID.String(), SelectedSnapshotSequence: selected.SnapshotSequence,
		SelectedSnapshotObservedAt: selected.ObservedAt.Time.UTC().Round(0),
		SnapshotFenceID:            snapshotFence.SnapshotID.String(), SnapshotFenceSequence: snapshotFence.SnapshotSequence,
		SnapshotFenceObservedAt: snapshotFence.ObservedAt.Time.UTC().Round(0), Items: digestItems,
	}
	windowDigest, err := digestWindow(document)
	if err != nil {
		return BuildResult{}, fmt.Errorf("encode seeding window evidence: %w", err)
	}
	if err := queries.InsertSeedingEvidenceWindow(ctx, ledgerdb.InsertSeedingEvidenceWindowParams{
		WindowStart: timestamp(windowStart), WindowEnd: timestamp(windowEnd),
		AnnounceSourceStream:  repository.config.AnnounceStream,
		AnnounceFenceSequence: announceFence.SourceSequence, AnnounceFenceReceivedAt: announceFence.ReceivedAt,
		SelectedSnapshotID: selected.SnapshotID, SelectedSnapshotSequence: selected.SnapshotSequence,
		SelectedSnapshotObservedAt: selected.ObservedAt,
		SnapshotFenceID:            snapshotFence.SnapshotID, SnapshotFenceSequence: snapshotFence.SnapshotSequence,
		SnapshotFenceObservedAt: snapshotFence.ObservedAt,
		ItemCount:               int32(len(items)), EvidenceSha256: windowDigest[:], BuiltAt: timestamp(builtAt),
	}); err != nil {
		return BuildResult{}, databaseError("insert seeding evidence window", err)
	}
	for _, item := range items {
		if err := insertEvidenceItem(ctx, queries, windowStart, item); err != nil {
			return BuildResult{}, err
		}
	}
	result := BuildResult{
		WindowStart: windowStart, WindowEnd: windowEnd,
		AnnounceFenceSequence: announceFence.SourceSequence,
		SelectedSnapshotID:    selected.SnapshotID, SelectedSnapshotSequence: selected.SnapshotSequence,
		SnapshotFenceID: snapshotFence.SnapshotID, SnapshotFenceSequence: snapshotFence.SnapshotSequence,
		ItemCount: int32(len(items)), EvidenceSHA256: windowDigest, BuiltAt: builtAt,
	}
	if err := enqueueEvidenceProjection(ctx, queries, result, selected.ObservedAt.Time, transportItems(items)); err != nil {
		return BuildResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BuildResult{}, databaseError("commit seeding evidence window", err)
	}
	return result, nil
}

// ensureEvidenceOutbox repairs the only safe upgrade gap: a window committed
// by the immediately preceding binary before this outbox existed. It rebuilds
// only the privacy-minimized projection from immutable closed rows. A partial
// existing chunk set is an invariant violation and is never silently filled.
func (repository *PostgresRepository) ensureEvidenceOutbox(ctx context.Context, queries *ledgerdb.Queries, result BuildResult) error {
	count, err := queries.CountSeedingEvidenceOutboxEvents(ctx, timestamp(result.WindowStart))
	if err != nil {
		return databaseError("count seeding evidence outbox events", err)
	}
	expected := int64(1)
	if result.ItemCount > 0 {
		expected = (int64(result.ItemCount) + settlementseedingv1.MaxChunkEntries - 1) / settlementseedingv1.MaxChunkEntries
	}
	if count == expected {
		return nil
	}
	if count != 0 {
		return ErrInvariant
	}
	rows, err := queries.ListSeedingEvidenceItemsForTransport(ctx, timestamp(result.WindowStart))
	if err != nil {
		return databaseError("list closed seeding evidence for outbox", err)
	}
	items := make([]settlementseedingv1.Item, len(rows))
	for index, row := range rows {
		if row.UserID == uuid.Nil || len(row.EvidenceSha256) != sha256.Size {
			return ErrInvariant
		}
		items[index] = settlementseedingv1.Item{
			UserID: row.UserID.String(), TorrentID: row.TorrentID, ActiveSeconds: row.ActiveSeconds,
			RawUploadedBytes: row.RawUploaded, SnapshotSeeders: row.SnapshotSeeders,
			SnapshotLeechers: row.SnapshotLeechers, EvidenceSHA256: hex.EncodeToString(row.EvidenceSha256),
		}
	}
	if int32(len(items)) != result.ItemCount {
		return ErrInvariant
	}
	row, err := queries.GetSeedingEvidenceWindow(ctx, timestamp(result.WindowStart))
	if err != nil || !row.SelectedSnapshotObservedAt.Valid {
		return databaseError("read seeding evidence transport header", err)
	}
	return enqueueEvidenceProjection(ctx, queries, result, row.SelectedSnapshotObservedAt.Time, items)
}

func transportItems(items []evidenceItem) []settlementseedingv1.Item {
	result := make([]settlementseedingv1.Item, len(items))
	for index, item := range items {
		result[index] = settlementseedingv1.Item{
			UserID: item.UserID.String(), TorrentID: item.TorrentID, ActiveSeconds: item.ActiveSeconds,
			RawUploadedBytes: item.RawUploaded, SnapshotSeeders: item.Snapshot.Seeders,
			SnapshotLeechers: item.Snapshot.Leechers,
			EvidenceSHA256:   hex.EncodeToString(item.EvidenceSHA256[:]),
		}
	}
	return result
}

func enqueueEvidenceProjection(ctx context.Context, queries *ledgerdb.Queries, result BuildResult, snapshotObservedAt time.Time, items []settlementseedingv1.Item) error {
	header := settlementseedingv1.Event{
		SchemaVersion:        settlementseedingv1.SchemaVersion,
		WindowStart:          result.WindowStart.UTC().Truncate(time.Microsecond),
		WindowEnd:            result.WindowEnd.UTC().Truncate(time.Microsecond),
		BuiltAt:              result.BuiltAt.UTC().Truncate(time.Microsecond),
		WindowEvidenceSHA256: hex.EncodeToString(result.EvidenceSHA256[:]),
		SnapshotID:           result.SelectedSnapshotID.String(), SnapshotSequence: result.SelectedSnapshotSequence,
		SnapshotObservedAt: snapshotObservedAt.UTC().Truncate(time.Microsecond), ItemCount: result.ItemCount,
	}
	projectionDigest, err := settlementseedingv1.ProjectionDigest(header, items)
	if err != nil {
		return ErrInvariant
	}
	header.ProjectionSHA256 = settlementseedingv1.DigestHex(projectionDigest)
	chunkCount := 1
	if len(items) > 0 {
		chunkCount = (len(items) + settlementseedingv1.MaxChunkEntries - 1) / settlementseedingv1.MaxChunkEntries
	}
	for index := 0; index < chunkCount; index++ {
		start, end := index*settlementseedingv1.MaxChunkEntries, (index+1)*settlementseedingv1.MaxChunkEntries
		if start > len(items) {
			return ErrInvariant
		}
		if end > len(items) {
			end = len(items)
		}
		event := header
		event.EventID = uuid.NewSHA1(seedingEvidenceEventNamespace, []byte(header.ProjectionSHA256+":"+strconv.Itoa(index))).String()
		event.ChunkIndex, event.ChunkCount = int32(index), int32(chunkCount)
		event.Items = items[start:end]
		payload, encodeErr := settlementseedingv1.Encode(event)
		if encodeErr != nil {
			return ErrInvariant
		}
		payloadDigest := sha256.Sum256(payload)
		if err := queries.InsertSeedingEvidenceOutboxEvent(ctx, ledgerdb.InsertSeedingEvidenceOutboxEventParams{
			EventID: uuid.MustParse(event.EventID), WindowStart: timestamp(result.WindowStart), ChunkIndex: int32(index),
			OccurredAt: timestamp(result.BuiltAt), PayloadJson: string(payload), PayloadSha256: payloadDigest[:],
			AvailableAt: timestamp(result.BuiltAt), CreatedAt: timestamp(result.BuiltAt),
		}); err != nil {
			return databaseError("insert seeding evidence outbox event", err)
		}
	}
	return nil
}

func insertEvidenceItem(ctx context.Context, queries *ledgerdb.Queries, windowStart time.Time, item evidenceItem) error {
	if err := queries.InsertSeedingEvidenceItem(ctx, ledgerdb.InsertSeedingEvidenceItemParams{
		WindowStart: timestamp(windowStart), UserID: item.UserID, TorrentID: item.TorrentID,
		InfoHashV1: item.InfoHashV1[:], ActiveSeconds: item.ActiveSeconds, RawUploaded: item.RawUploaded,
		SourceIntervalCount: int32(len(item.Sources)), FirstActiveAt: timestamp(item.FirstActiveAt),
		LastActiveAt: timestamp(item.LastActiveAt), SnapshotSeeders: item.Snapshot.Seeders,
		SnapshotLeechers: item.Snapshot.Leechers, EvidenceSha256: item.EvidenceSHA256[:],
	}); err != nil {
		return databaseError("insert seeding evidence item", err)
	}
	for _, source := range item.Sources {
		if err := queries.InsertSeedingEvidenceSource(ctx, ledgerdb.InsertSeedingEvidenceSourceParams{
			WindowStart: timestamp(windowStart), UserID: item.UserID, TorrentID: item.TorrentID,
			IntervalEventID: source.EventID, SourceSequence: source.SourceSequence,
			ClippedStartsAt: timestamp(source.StartsAt), ClippedEndsAt: timestamp(source.EndsAt),
		}); err != nil {
			return databaseError("insert seeding evidence source", err)
		}
	}
	return nil
}

func intervalFactsFromRows(rows []ledgerdb.ListSeedingIntervalsForWindowRow) ([]intervalFact, error) {
	result := make([]intervalFact, len(rows))
	for index, row := range rows {
		if len(row.InfoHashV1) != 20 || !row.ClippedStartsAt.Valid || !row.ClippedEndsAt.Valid {
			return nil, ErrInvariant
		}
		result[index] = intervalFact{
			EventID: row.EventID, UserID: row.UserID, TorrentID: row.TorrentID,
			StartsAt: row.ClippedStartsAt.Time.UTC().Round(0), EndsAt: row.ClippedEndsAt.Time.UTC().Round(0),
			RawUploaded: row.RawUploaded, SourceSequence: row.SourceSequence,
		}
		copy(result[index].InfoHashV1[:], row.InfoHashV1)
	}
	return result, nil
}

func snapshotCountsFromRows(rows []ledgerdb.ListSeedingSnapshotEntriesRow) (map[[20]byte]snapshotCounts, error) {
	result := make(map[[20]byte]snapshotCounts, len(rows))
	for _, row := range rows {
		if len(row.InfoHashV1) != 20 || row.Seeders < 0 || row.Leechers < 0 {
			return nil, ErrInvariant
		}
		var infoHash [20]byte
		copy(infoHash[:], row.InfoHashV1)
		if _, duplicate := result[infoHash]; duplicate {
			return nil, ErrInvariant
		}
		result[infoHash] = snapshotCounts{Seeders: row.Seeders, Leechers: row.Leechers}
	}
	return result, nil
}

func existingWindowResult(row ledgerdb.GetSeedingEvidenceWindowRow) (BuildResult, error) {
	if !row.WindowStart.Valid || !row.WindowEnd.Valid || !row.BuiltAt.Valid || len(row.EvidenceSha256) != sha256.Size {
		return BuildResult{}, ErrInvariant
	}
	result := BuildResult{
		WindowStart: row.WindowStart.Time.UTC().Round(0), WindowEnd: row.WindowEnd.Time.UTC().Round(0),
		AnnounceFenceSequence: row.AnnounceFenceSequence,
		SelectedSnapshotID:    row.SelectedSnapshotID, SelectedSnapshotSequence: row.SelectedSnapshotSequence,
		SnapshotFenceID: row.SnapshotFenceID, SnapshotFenceSequence: row.SnapshotFenceSequence,
		ItemCount: row.ItemCount, BuiltAt: row.BuiltAt.Time.UTC().Round(0),
	}
	copy(result.EvidenceSHA256[:], row.EvidenceSha256)
	return result, nil
}

func validWindowStart(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	_, offset := value.Zone()
	return offset == 0 && value.Equal(value.Truncate(time.Hour))
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC().Round(0), Valid: true}
}

func sameTimestamp(value pgtype.Timestamptz, expected time.Time) bool {
	return value.Valid && value.Time.UTC().Round(0).Equal(expected.UTC().Round(0))
}

func isConstraintError(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && len(postgresError.Code) >= 2 && postgresError.Code[:2] == "23"
}

func databaseError(operation string, err error) error {
	if err == nil {
		return ErrInvariant
	}
	if isConstraintError(err) {
		return fmt.Errorf("%w: %s: %v", ErrInvariant, operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func rowsError(operation string, rows int64, err error) error {
	if err != nil {
		return databaseError(operation, err)
	}
	if rows != 1 {
		return fmt.Errorf("%w: %s affected %d rows", ErrInvariant, operation, rows)
	}
	return nil
}
