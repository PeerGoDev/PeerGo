package torrents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/generated/torrentdb"
)

type PostgresStorageMigrationRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresStorageMigrationRepository(pool *pgxpool.Pool) (*PostgresStorageMigrationRepository, error) {
	if pool == nil {
		return nil, errors.New("storage migration repository requires a database pool")
	}
	return &PostgresStorageMigrationRepository{pool: pool}, nil
}

// Plan snapshots only source locations that are both verified and preferred.
// New uploads may switch to the destination immediately after this transaction
// without changing the finite reconciliation manifest owned by the run.
func (repository *PostgresStorageMigrationRepository) Plan(ctx context.Context, input PlanStorageMigrationInput) (StorageMigrationPlan, error) {
	if input.ID == uuid.Nil || input.RequestedBy == uuid.Nil || input.OccurredAt.IsZero() ||
		(input.Mode != StorageMigrationReplicate && input.Mode != StorageMigrationMove) ||
		input.SourceBackendID == "" || input.DestinationBackendID == "" || input.SourceBackendID == input.DestinationBackendID {
		return StorageMigrationPlan{}, ErrStorageInputInvalid
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return StorageMigrationPlan{}, fmt.Errorf("begin storage migration plan: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := torrentdb.New(tx)
	timestamp := storageTimestamp(input.OccurredAt)
	row, err := queries.InsertStorageMigration(ctx, torrentdb.InsertStorageMigrationParams{
		MigrationID: input.ID, MigrationMode: string(input.Mode),
		SourceBackendID: string(input.SourceBackendID), DestinationBackendID: string(input.DestinationBackendID),
		RequestedBy: input.RequestedBy, OccurredAt: timestamp,
	})
	if storageUniqueViolation(err) {
		return StorageMigrationPlan{}, ErrStorageStateConflict
	}
	if err != nil {
		return StorageMigrationPlan{}, fmt.Errorf("insert storage migration: %w", err)
	}
	objectCount, err := queries.SnapshotStorageMigrationItems(ctx, torrentdb.SnapshotStorageMigrationItemsParams{
		MigrationID: input.ID, OccurredAt: timestamp, SourceBackendID: string(input.SourceBackendID),
	})
	if err != nil {
		return StorageMigrationPlan{}, fmt.Errorf("snapshot storage migration objects: %w", err)
	}
	if objectCount == 0 {
		if _, err := queries.CompleteEmptyStorageMigration(ctx, torrentdb.CompleteEmptyStorageMigrationParams{
			OccurredAt: timestamp, MigrationID: input.ID,
		}); err != nil {
			return StorageMigrationPlan{}, fmt.Errorf("complete empty storage migration: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return StorageMigrationPlan{}, fmt.Errorf("commit storage migration plan: %w", err)
	}
	if !row.CreatedAt.Valid {
		return StorageMigrationPlan{}, errors.New("storage migration has invalid creation time")
	}
	return StorageMigrationPlan{
		ID: row.ID, Mode: StorageMigrationMode(row.Mode),
		SourceBackendID: StorageBackendID(row.SourceBackendID), DestinationBackendID: StorageBackendID(row.DestinationBackendID),
		ObjectCount: objectCount, CreatedAt: row.CreatedAt.Time.UTC(),
	}, nil
}

func (repository *PostgresStorageMigrationRepository) ClaimCopyTasks(ctx context.Context, migrationID uuid.UUID, now time.Time, batchSize int32, leaseDuration time.Duration) ([]StorageCopyTask, error) {
	if migrationID == uuid.Nil || now.IsZero() || batchSize < 1 || batchSize > 100 || leaseDuration <= 0 || leaseDuration > 10*time.Minute {
		return nil, ErrStorageInputInvalid
	}
	rows, err := torrentdb.New(repository.pool).ClaimStorageCopyTasks(ctx, torrentdb.ClaimStorageCopyTasksParams{
		MigrationID: migrationID, ClaimedAt: storageTimestamp(now), BatchSize: batchSize, LeaseUntil: storageTimestamp(now.Add(leaseDuration)),
	})
	if err != nil {
		return nil, fmt.Errorf("claim storage copy rows: %w", err)
	}
	tasks := make([]StorageCopyTask, 0, len(rows))
	for _, row := range rows {
		sourceBackend, sourceKey, destinationBackend, digest, leaseToken, err := storageCopyRowValues(row)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, StorageCopyTask{
			MigrationID: row.MigrationID, ObjectID: row.ObjectID,
			Descriptor:      StoredObjectDescriptor{SHA256: digest, ByteLength: row.ByteLength},
			SourceBackendID: sourceBackend, SourceObjectKey: sourceKey, SourceVersionID: nullableStorageString(row.SourceVersionID),
			DestinationBackendID: destinationBackend, LeaseToken: leaseToken, Attempts: row.Attempts,
		})
	}
	return tasks, nil
}

func storageCopyRowValues(row torrentdb.ClaimStorageCopyTasksRow) (StorageBackendID, ObjectKey, StorageBackendID, ObjectSHA256, uuid.UUID, error) {
	sourceBackend, err := ParseStorageBackendID(row.SourceBackendID)
	if err != nil {
		return "", "", "", ObjectSHA256{}, uuid.Nil, errors.New("claimed storage copy has invalid source backend")
	}
	destinationBackend, err := ParseStorageBackendID(row.DestinationBackendID)
	if err != nil || sourceBackend == destinationBackend {
		return "", "", "", ObjectSHA256{}, uuid.Nil, errors.New("claimed storage copy has invalid destination backend")
	}
	sourceKey, err := ParseObjectKey(row.SourceObjectKey)
	if err != nil || len(row.ContentSha256) != 32 || row.ByteLength <= 0 || !row.LeaseToken.Valid {
		return "", "", "", ObjectSHA256{}, uuid.Nil, errors.New("claimed storage copy has invalid immutable metadata")
	}
	var digest ObjectSHA256
	copy(digest[:], row.ContentSha256)
	return sourceBackend, sourceKey, destinationBackend, digest, uuid.UUID(row.LeaseToken.Bytes), nil
}

func (repository *PostgresStorageMigrationRepository) MarkCopyVerified(ctx context.Context, task StorageCopyTask, location VerifiedObjectLocation) error {
	if err := validateVerifiedStorageLocation(task, location); err != nil {
		return err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin verified storage location: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := torrentdb.New(tx)
	locked, err := queries.LockStorageMigrationItem(ctx, torrentdb.LockStorageMigrationItemParams{
		MigrationID: task.MigrationID, ObjectID: task.ObjectID, LeaseToken: task.LeaseToken,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStorageStateConflict
	}
	if err != nil {
		return fmt.Errorf("lock storage copy item: %w", err)
	}
	if locked.DestinationBackendID != string(location.BackendID) || locked.DestinationObjectKey != string(location.ObjectKey) ||
		locked.ByteLength != location.Descriptor.ByteLength || len(locked.ContentSha256) != 32 || !bytes.Equal(locked.ContentSha256, location.Descriptor.SHA256[:]) {
		return ErrStorageStateConflict
	}

	locationID, err := repository.ensureVerifiedLocation(ctx, queries, task.ObjectID, location)
	if err != nil {
		return err
	}
	rows, err := queries.MarkStorageCopyTaskVerified(ctx, torrentdb.MarkStorageCopyTaskVerifiedParams{
		DestinationLocationID: locationID, VerifiedAt: storageTimestamp(location.VerifiedAt),
		MigrationID: task.MigrationID, ObjectID: task.ObjectID, LeaseToken: task.LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("mark storage copy verified: %w", err)
	}
	if rows != 1 {
		return ErrStorageStateConflict
	}
	if _, err := queries.AdvanceStorageMigrationAfterCopy(ctx, torrentdb.AdvanceStorageMigrationAfterCopyParams{
		OccurredAt: storageTimestamp(location.VerifiedAt), MigrationID: task.MigrationID,
	}); err != nil {
		return fmt.Errorf("advance storage migration after verification: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit verified storage location: %w", err)
	}
	return nil
}

func validateVerifiedStorageLocation(task StorageCopyTask, location VerifiedObjectLocation) error {
	if task.MigrationID == uuid.Nil || task.ObjectID == uuid.Nil || task.LeaseToken == uuid.Nil ||
		location.BackendID == "" || location.BackendID != task.DestinationBackendID || location.ObjectKey != TorrentObjectKey(task.Descriptor.SHA256) ||
		location.Descriptor != task.Descriptor || !location.Descriptor.Valid() || location.VerifiedAt.IsZero() {
		return ErrStorageInputInvalid
	}
	return nil
}

func (repository *PostgresStorageMigrationRepository) ensureVerifiedLocation(ctx context.Context, queries *torrentdb.Queries, objectID uuid.UUID, location VerifiedObjectLocation) (uuid.UUID, error) {
	row, err := queries.GetTorrentObjectLocationForUpdate(ctx, torrentdb.GetTorrentObjectLocationForUpdateParams{
		ObjectID: objectID, BackendID: string(location.BackendID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		locationID, insertErr := queries.InsertVerifiedTorrentObjectLocation(ctx, torrentdb.InsertVerifiedTorrentObjectLocationParams{
			LocationID: uuid.New(), ObjectID: objectID, BackendID: string(location.BackendID), ObjectKey: string(location.ObjectKey),
			VersionID: nullableStorageText(location.VersionID), ObservedByteLength: location.Descriptor.ByteLength,
			ObservedSha256: location.Descriptor.SHA256[:], VerifiedAt: storageTimestamp(location.VerifiedAt),
		})
		if storageUniqueViolation(insertErr) {
			return uuid.Nil, ErrStorageStateConflict
		}
		if insertErr != nil {
			return uuid.Nil, fmt.Errorf("insert verified storage location: %w", insertErr)
		}
		return locationID, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("read destination storage location: %w", err)
	}
	if row.ObjectKey != string(location.ObjectKey) {
		return uuid.Nil, ErrStoredObjectConflict
	}

	switch StorageLocationState(row.State) {
	case StorageLocationVerified:
		if !row.ObservedByteLength.Valid || row.ObservedByteLength.Int64 != location.Descriptor.ByteLength ||
			len(row.ObservedSha256) != 32 || !bytes.Equal(row.ObservedSha256, location.Descriptor.SHA256[:]) || !row.VerifiedAt.Valid {
			return uuid.Nil, ErrStoredObjectConflict
		}
		return row.ID, nil
	case StorageLocationPending, StorageLocationFailed:
		locationID, promoteErr := queries.PromotePendingTorrentObjectLocation(ctx, torrentdb.PromotePendingTorrentObjectLocationParams{
			VersionID: nullableStorageText(location.VersionID), ObservedByteLength: location.Descriptor.ByteLength,
			ObservedSha256: location.Descriptor.SHA256[:], VerifiedAt: storageTimestamp(location.VerifiedAt),
			LocationID: row.ID, ExpectedVersion: row.Version,
		})
		if errors.Is(promoteErr, pgx.ErrNoRows) {
			return uuid.Nil, ErrStorageStateConflict
		}
		if promoteErr != nil {
			return uuid.Nil, fmt.Errorf("promote verified storage location: %w", promoteErr)
		}
		return locationID, nil
	case StorageLocationRetiring:
		locationID, restoreErr := queries.RestoreRetiringTorrentObjectLocation(ctx, torrentdb.RestoreRetiringTorrentObjectLocationParams{
			VerifiedAt: storageTimestamp(location.VerifiedAt), LocationID: row.ID, ObjectKey: string(location.ObjectKey),
			ObservedByteLength: location.Descriptor.ByteLength, ObservedSha256: location.Descriptor.SHA256[:], ExpectedVersion: row.Version,
		})
		if errors.Is(restoreErr, pgx.ErrNoRows) {
			return uuid.Nil, ErrStorageStateConflict
		}
		if restoreErr != nil {
			return uuid.Nil, fmt.Errorf("restore verified storage location: %w", restoreErr)
		}
		return locationID, nil
	default:
		return uuid.Nil, ErrStorageStateConflict
	}
}

func (repository *PostgresStorageMigrationRepository) ReleaseCopyTask(ctx context.Context, task StorageCopyTask, retryAt time.Time, errorCode string) error {
	if task.MigrationID == uuid.Nil || task.ObjectID == uuid.Nil || task.LeaseToken == uuid.Nil || retryAt.IsZero() || !validStorageErrorCode(errorCode) {
		return ErrStorageInputInvalid
	}
	releasedAt := time.Now().UTC()
	rows, err := torrentdb.New(repository.pool).ReleaseStorageCopyTask(ctx, torrentdb.ReleaseStorageCopyTaskParams{
		AvailableAt: storageTimestamp(retryAt), LastErrorCode: errorCode, ReleasedAt: storageTimestamp(releasedAt),
		MigrationID: task.MigrationID, ObjectID: task.ObjectID, LeaseToken: task.LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("release storage copy task: %w", err)
	}
	if rows != 1 {
		return ErrStorageStateConflict
	}
	return nil
}

func (repository *PostgresStorageMigrationRepository) Cutover(ctx context.Context, migrationID uuid.UUID, cutoverAt, retentionUntil time.Time) error {
	if migrationID == uuid.Nil || cutoverAt.IsZero() || !retentionUntil.After(cutoverAt) {
		return ErrStorageInputInvalid
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin storage migration cutover: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := torrentdb.New(tx)
	run, err := queries.LockStorageMigrationForCutover(ctx, migrationID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (run.Mode != string(StorageMigrationMove) || run.Status != "ready_for_cutover") {
		return ErrStorageStateConflict
	}
	if err != nil {
		return fmt.Errorf("lock storage migration for cutover: %w", err)
	}
	total, err := queries.CountStorageMigrationItems(ctx, migrationID)
	if err != nil {
		return fmt.Errorf("count storage migration items: %w", err)
	}
	unverified, err := queries.CountUnverifiedStorageMigrationItems(ctx, migrationID)
	if err != nil {
		return fmt.Errorf("reconcile storage migration verification: %w", err)
	}
	if total < 1 || unverified != 0 {
		return ErrStorageStateConflict
	}
	retired, err := queries.RetireStorageMigrationSources(ctx, torrentdb.RetireStorageMigrationSourcesParams{
		CutoverAt: storageTimestamp(cutoverAt), MigrationID: migrationID,
	})
	if err != nil {
		return fmt.Errorf("retire source storage locations: %w", err)
	}
	preferred, err := queries.PreferStorageMigrationDestinations(ctx, torrentdb.PreferStorageMigrationDestinationsParams{
		CutoverAt: storageTimestamp(cutoverAt), MigrationID: migrationID,
	})
	if err != nil {
		return fmt.Errorf("prefer destination storage locations: %w", err)
	}
	if retired != total || preferred != total {
		return ErrStorageStateConflict
	}
	rows, err := queries.MarkStorageMigrationRetaining(ctx, torrentdb.MarkStorageMigrationRetainingParams{
		CutoverAt: storageTimestamp(cutoverAt), RetentionUntil: storageTimestamp(retentionUntil), MigrationID: migrationID,
	})
	if err != nil {
		return fmt.Errorf("mark storage migration retaining: %w", err)
	}
	if rows != 1 {
		return ErrStorageStateConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit storage migration cutover: %w", err)
	}
	return nil
}

func (repository *PostgresStorageMigrationRepository) ApproveCleanup(ctx context.Context, migrationID, approvedBy uuid.UUID, approvedAt time.Time) error {
	if migrationID == uuid.Nil || approvedBy == uuid.Nil || approvedAt.IsZero() {
		return ErrStorageInputInvalid
	}
	rows, err := torrentdb.New(repository.pool).ApproveStorageMigrationCleanup(ctx, torrentdb.ApproveStorageMigrationCleanupParams{
		ApprovedBy: approvedBy, ApprovedAt: storageTimestamp(approvedAt), MigrationID: migrationID,
	})
	if err != nil {
		return fmt.Errorf("approve storage migration cleanup: %w", err)
	}
	if rows != 1 {
		return ErrStorageStateConflict
	}
	return nil
}

func (repository *PostgresStorageMigrationRepository) ClaimCleanupTasks(ctx context.Context, migrationID uuid.UUID, now time.Time, batchSize int32, leaseDuration time.Duration) ([]StorageCleanupTask, error) {
	if migrationID == uuid.Nil || now.IsZero() || batchSize < 1 || batchSize > 100 || leaseDuration <= 0 || leaseDuration > 10*time.Minute {
		return nil, ErrStorageInputInvalid
	}
	rows, err := torrentdb.New(repository.pool).ClaimStorageCleanupTasks(ctx, torrentdb.ClaimStorageCleanupTasksParams{
		MigrationID: migrationID, ClaimedAt: storageTimestamp(now), BatchSize: batchSize, LeaseUntil: storageTimestamp(now.Add(leaseDuration)),
	})
	if err != nil {
		return nil, fmt.Errorf("claim storage cleanup rows: %w", err)
	}
	tasks := make([]StorageCleanupTask, 0, len(rows))
	for _, row := range rows {
		backendID, parseErr := ParseStorageBackendID(row.SourceBackendID)
		objectKey, keyErr := ParseObjectKey(row.SourceObjectKey)
		if parseErr != nil || keyErr != nil || !row.LeaseToken.Valid {
			return nil, errors.New("claimed storage cleanup has invalid metadata")
		}
		tasks = append(tasks, StorageCleanupTask{
			MigrationID: row.MigrationID, ObjectID: row.ObjectID, SourceBackendID: backendID,
			SourceObjectKey: objectKey, SourceVersionID: nullableStorageString(row.SourceVersionID),
			LeaseToken: uuid.UUID(row.LeaseToken.Bytes), Attempts: row.Attempts,
		})
	}
	return tasks, nil
}

func (repository *PostgresStorageMigrationRepository) MarkSourceDeleted(ctx context.Context, task StorageCleanupTask, deletedAt time.Time) error {
	if task.MigrationID == uuid.Nil || task.ObjectID == uuid.Nil || task.LeaseToken == uuid.Nil || deletedAt.IsZero() {
		return ErrStorageInputInvalid
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin source storage deletion record: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := torrentdb.New(tx)
	locationID, err := queries.LockStorageCleanupItem(ctx, torrentdb.LockStorageCleanupItemParams{
		MigrationID: task.MigrationID, ObjectID: task.ObjectID, LeaseToken: task.LeaseToken,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStorageStateConflict
	}
	if err != nil {
		return fmt.Errorf("lock storage cleanup item: %w", err)
	}
	rows, err := queries.MarkTorrentObjectLocationDeleted(ctx, torrentdb.MarkTorrentObjectLocationDeletedParams{
		DeletedAt: storageTimestamp(deletedAt), LocationID: locationID,
	})
	if err != nil {
		return fmt.Errorf("mark source storage location deleted: %w", err)
	}
	if rows != 1 {
		return ErrStorageStateConflict
	}
	rows, err = queries.MarkStorageCleanupTaskDeleted(ctx, torrentdb.MarkStorageCleanupTaskDeletedParams{
		DeletedAt: storageTimestamp(deletedAt), MigrationID: task.MigrationID,
		ObjectID: task.ObjectID, LeaseToken: task.LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("mark storage cleanup item deleted: %w", err)
	}
	if rows != 1 {
		return ErrStorageStateConflict
	}
	if _, err := queries.CompleteStorageMigrationAfterCleanup(ctx, torrentdb.CompleteStorageMigrationAfterCleanupParams{
		CompletedAt: storageTimestamp(deletedAt), MigrationID: task.MigrationID,
	}); err != nil {
		return fmt.Errorf("complete storage migration cleanup: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit source storage deletion record: %w", err)
	}
	return nil
}

func (repository *PostgresStorageMigrationRepository) ReleaseCleanupTask(ctx context.Context, task StorageCleanupTask, retryAt time.Time, errorCode string) error {
	if task.MigrationID == uuid.Nil || task.ObjectID == uuid.Nil || task.LeaseToken == uuid.Nil || retryAt.IsZero() || !validStorageErrorCode(errorCode) {
		return ErrStorageInputInvalid
	}
	releasedAt := time.Now().UTC()
	rows, err := torrentdb.New(repository.pool).ReleaseStorageCleanupTask(ctx, torrentdb.ReleaseStorageCleanupTaskParams{
		AvailableAt: storageTimestamp(retryAt), LastErrorCode: errorCode, ReleasedAt: storageTimestamp(releasedAt),
		MigrationID: task.MigrationID, ObjectID: task.ObjectID, LeaseToken: task.LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("release storage cleanup task: %w", err)
	}
	if rows != 1 {
		return ErrStorageStateConflict
	}
	return nil
}

func nullableStorageText(value string) pgtype.Text {
	value = string(bytes.TrimSpace([]byte(value)))
	return pgtype.Text{String: value, Valid: value != ""}
}

func nullableStorageString(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func storageTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func validStorageErrorCode(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	_, err := ParseStorageBackendID(value)
	return err == nil
}

func storageUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

var _ StorageMigrationRepository = (*PostgresStorageMigrationRepository)(nil)
