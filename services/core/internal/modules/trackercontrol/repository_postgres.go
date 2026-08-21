package trackercontrol

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/trackerevent"
	"github.com/peergo/peergo/services/core/internal/generated/trackercontroldb"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

var projectionErrorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// PostgresOutbox can be constructed over a pgx transaction, ensuring the
// torrent state transition and Tracker event are one atomic Core commit.
type PostgresOutbox struct {
	queries *trackercontroldb.Queries
}

func NewPostgresOutbox(db trackercontroldb.DBTX) *PostgresOutbox {
	return &PostgresOutbox{queries: trackercontroldb.New(db)}
}

func (outbox *PostgresOutbox) Append(ctx context.Context, event trackerevent.Event) error {
	if err := trackerevent.Validate(event); err != nil {
		return err
	}
	if err := outbox.queries.AppendTrackerControlEvent(ctx, trackercontroldb.AppendTrackerControlEventParams{
		EventID: event.ID, EventType: event.Type, SchemaVersion: event.SchemaVersion,
		AggregateID: event.AggregateID, AggregateVersion: event.AggregateVersion,
		OccurredAt: controlTimestamp(event.OccurredAt), PayloadJson: string(event.Payload),
		PayloadSha256: event.PayloadSHA256[:], AvailableAt: controlTimestamp(event.OccurredAt),
	}); err != nil {
		return fmt.Errorf("append Tracker control event: %w", err)
	}
	return nil
}

type PostgresRepository struct {
	pool    *pgxpool.Pool
	queries *trackercontroldb.Queries
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("Tracker control PostgreSQL pool is required")
	}
	return &PostgresRepository{pool: pool, queries: trackercontroldb.New(pool)}, nil
}

func (repository *PostgresRepository) ClaimNext(ctx context.Context, now time.Time, leaseDuration time.Duration) (PendingEvent, bool, error) {
	if now.IsZero() || leaseDuration <= 0 || leaseDuration > 5*time.Minute {
		return PendingEvent{}, false, ErrProjectionInput
	}
	leaseToken := uuid.New()
	row, err := repository.queries.ClaimNextTrackerControlEvent(ctx, trackercontroldb.ClaimNextTrackerControlEventParams{
		LeaseToken: leaseToken, LeaseUntil: controlTimestamp(now.Add(leaseDuration)), ClaimedAt: controlTimestamp(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PendingEvent{}, false, nil
	}
	if err != nil {
		return PendingEvent{}, false, fmt.Errorf("claim Tracker control event: %w", err)
	}
	event, err := trackerEventFromValues(
		row.EventID, row.EventType, row.SchemaVersion, row.AggregateID, row.AggregateVersion,
		row.OccurredAt, row.PayloadJson, row.PayloadSha256,
	)
	if err != nil || !row.LeaseToken.Valid || uuid.UUID(row.LeaseToken.Bytes) != leaseToken || row.Sequence < 1 {
		return PendingEvent{}, false, errors.New("claimed Tracker control event has invalid metadata")
	}
	return PendingEvent{Sequence: row.Sequence, LeaseToken: leaseToken, Attempts: row.Attempts, Event: event}, true, nil
}

func (repository *PostgresRepository) Apply(ctx context.Context, pending PendingEvent, projectedAt time.Time) error {
	if pending.Sequence < 1 || pending.LeaseToken == uuid.Nil || pending.Attempts < 1 || projectedAt.IsZero() {
		return ErrProjectionInput
	}
	if err := trackerevent.Validate(pending.Event); err != nil {
		return err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin Tracker control projection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := trackercontroldb.New(tx)
	row, err := queries.GetClaimedTrackerControlEventForUpdate(ctx, trackercontroldb.GetClaimedTrackerControlEventForUpdateParams{
		ControlSequence: pending.Sequence, LeaseToken: pending.LeaseToken,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrProjectionStateConflict
	}
	if err != nil {
		return fmt.Errorf("lock Tracker control event: %w", err)
	}
	persisted, err := trackerEventFromValues(
		row.EventID, row.EventType, row.SchemaVersion, row.AggregateID, row.AggregateVersion,
		row.OccurredAt, row.PayloadJson, row.PayloadSha256,
	)
	if err != nil || persisted.ID != pending.Event.ID || persisted.PayloadSHA256 != pending.Event.PayloadSHA256 {
		return ErrProjectionStateConflict
	}
	payload, err := trackerevent.DecodeTorrentEligibilityChanged(persisted)
	if err != nil {
		return fmt.Errorf("decode Tracker control event: %w", err)
	}
	state, err := queries.GetTrackerProjectionStateForUpdate(ctx)
	if err != nil {
		return fmt.Errorf("lock Tracker projection state: %w", err)
	}
	if state.LastSequence >= pending.Sequence {
		return ErrProjectionStateConflict
	}
	hash, err := torrents.ParseInfoHashV1Hex(payload.InfoHashV1)
	if err != nil {
		return fmt.Errorf("parse projected info hash: %w", err)
	}
	rows, err := queries.UpsertTorrentAllowlistProjection(ctx, trackercontroldb.UpsertTorrentAllowlistProjectionParams{
		TorrentID:  payload.TorrentID,
		InfoHashV1: hash[:], TotalSizeBytes: payload.TotalSizeBytes, Enabled: payload.Enabled,
		TorrentVersion: payload.TorrentVersion, ControlSequence: pending.Sequence,
		UpdatedAt: controlTimestamp(payload.OccurredAt),
	})
	if err != nil {
		return fmt.Errorf("upsert Tracker allowlist projection: %w", err)
	}
	if rows != 1 {
		return ErrProjectionStateConflict
	}
	rows, err = queries.AdvanceTrackerProjectionState(ctx, trackercontroldb.AdvanceTrackerProjectionStateParams{
		ControlSequence: pending.Sequence, ProjectedAt: controlTimestamp(projectedAt),
	})
	if err != nil {
		return fmt.Errorf("advance Tracker projection state: %w", err)
	}
	if rows != 1 {
		return ErrProjectionStateConflict
	}
	rows, err = queries.MarkTrackerControlEventProjected(ctx, trackercontroldb.MarkTrackerControlEventProjectedParams{
		ProjectedAt: controlTimestamp(projectedAt), ControlSequence: pending.Sequence, LeaseToken: pending.LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("mark Tracker control event projected: %w", err)
	}
	if rows != 1 {
		return ErrProjectionStateConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Tracker control projection: %w", err)
	}
	return nil
}

func (repository *PostgresRepository) Release(ctx context.Context, pending PendingEvent, availableAt time.Time, errorCode string) error {
	if pending.Sequence < 1 || pending.LeaseToken == uuid.Nil || availableAt.IsZero() || !projectionErrorCodePattern.MatchString(errorCode) {
		return ErrProjectionInput
	}
	rows, err := repository.queries.ReleaseTrackerControlEvent(ctx, trackercontroldb.ReleaseTrackerControlEventParams{
		AvailableAt: controlTimestamp(availableAt), LastErrorCode: errorCode,
		ControlSequence: pending.Sequence, LeaseToken: pending.LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("release Tracker control event: %w", err)
	}
	if rows != 1 {
		return ErrProjectionStateConflict
	}
	return nil
}

func (repository *PostgresRepository) Status(ctx context.Context) (ProjectionStatus, error) {
	row, err := repository.queries.GetTrackerProjectionStatus(ctx)
	if err != nil {
		return ProjectionStatus{}, fmt.Errorf("read Tracker projection status: %w", err)
	}
	status := ProjectionStatus{LastSequence: row.LastSequence, PendingEvents: row.PendingEvents}
	if row.UpdatedAt.Valid {
		updatedAt := row.UpdatedAt.Time.UTC()
		status.UpdatedAt = &updatedAt
	}
	return status, nil
}

func (repository *PostgresRepository) ListEnabled(ctx context.Context) ([]AllowlistEntry, error) {
	rows, err := repository.queries.ListEnabledTorrentAllowlist(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Tracker allowlist projection: %w", err)
	}
	return allowlistEntries(rows)
}

// ReadSnapshot binds the projection cursor and allowlist rows to one
// repeatable-read PostgreSQL snapshot. Without this transaction a builder could
// sign a new cursor together with an older allowlist (or the inverse).
func (repository *PostgresRepository) ReadSnapshot(ctx context.Context) (ProjectionSnapshot, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return ProjectionSnapshot{}, fmt.Errorf("begin Tracker snapshot read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := trackercontroldb.New(tx)
	state, err := queries.GetTrackerSnapshotProjectionState(ctx)
	if err != nil {
		return ProjectionSnapshot{}, fmt.Errorf("read Tracker snapshot cursor: %w", err)
	}
	rows, err := queries.ListEnabledTorrentAllowlist(ctx)
	if err != nil {
		return ProjectionSnapshot{}, fmt.Errorf("read Tracker snapshot allowlist: %w", err)
	}
	entries, err := allowlistEntries(rows)
	if err != nil {
		return ProjectionSnapshot{}, err
	}
	if state.LastSequence < 0 || state.CompletionSequence < 0 || state.PendingEvents < 0 || (state.LastSequence == 0 && state.UpdatedAt.Valid) ||
		(state.LastSequence > 0 && !state.UpdatedAt.Valid) {
		return ProjectionSnapshot{}, ErrSnapshotProjection
	}
	for _, entry := range entries {
		if entry.ControlSequence > state.LastSequence {
			return ProjectionSnapshot{}, ErrSnapshotProjection
		}
	}
	result := ProjectionSnapshot{
		ControlSequence: state.LastSequence, CompletionSequence: state.CompletionSequence,
		PendingEvents: state.PendingEvents, Torrents: entries,
	}
	if state.UpdatedAt.Valid {
		updatedAt := state.UpdatedAt.Time.UTC()
		result.ProjectionUpdatedAt = &updatedAt
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectionSnapshot{}, fmt.Errorf("commit Tracker snapshot read: %w", err)
	}
	return result, nil
}

func allowlistEntries(rows []trackercontroldb.ListEnabledTorrentAllowlistRow) ([]AllowlistEntry, error) {
	entries := make([]AllowlistEntry, 0, len(rows))
	for _, row := range rows {
		if len(row.InfoHashV1) != 20 || row.TorrentID < 1 || row.CompletedDownloads < 0 ||
			row.TotalSizeBytes < 1 || row.TorrentVersion < 1 || row.ControlSequence < 1 || !row.UpdatedAt.Valid {
			return nil, errors.New("Tracker allowlist projection contains invalid metadata")
		}
		var hash torrents.InfoHashV1
		copy(hash[:], row.InfoHashV1)
		entries = append(entries, AllowlistEntry{
			TorrentID:  torrents.TorrentID(row.TorrentID),
			InfoHashV1: hash, TotalSizeBytes: row.TotalSizeBytes, CompletedDownloads: row.CompletedDownloads,
			TorrentVersion:  row.TorrentVersion,
			ControlSequence: row.ControlSequence, UpdatedAt: row.UpdatedAt.Time.UTC(),
		})
	}
	return entries, nil
}

func trackerEventFromValues(id uuid.UUID, eventType, schemaVersion string, aggregateID, aggregateVersion int64, occurredAt pgtype.Timestamptz, payload string, rawDigest []byte) (trackerevent.Event, error) {
	if !occurredAt.Valid || len(rawDigest) != sha256.Size {
		return trackerevent.Event{}, errors.New("Tracker control event has invalid persisted metadata")
	}
	var digest [sha256.Size]byte
	copy(digest[:], rawDigest)
	event := trackerevent.Event{
		ID: id, Type: eventType, SchemaVersion: schemaVersion,
		AggregateID: aggregateID, AggregateVersion: aggregateVersion,
		OccurredAt: occurredAt.Time.UTC(), Payload: []byte(payload), PayloadSHA256: digest,
	}
	if err := trackerevent.Validate(event); err != nil {
		return trackerevent.Event{}, err
	}
	return event, nil
}

func controlTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

var _ trackerevent.Appender = (*PostgresOutbox)(nil)
var _ SnapshotSource = (*PostgresRepository)(nil)
