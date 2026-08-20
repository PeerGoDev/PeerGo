package objectmigration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("object migration repository requires a database pool")
	}
	return &PostgresRepository{pool: pool}, nil
}

func (repository *PostgresRepository) Plan(ctx context.Context, input PlanInput) (Plan, error) {
	if input.ID == uuid.Nil || input.RequestedBy == uuid.Nil || input.OccurredAt.IsZero() || len(input.Kinds) == 0 {
		return Plan{}, ErrInput
	}
	kinds := make([]string, len(input.Kinds))
	for index, kind := range input.Kinds {
		if !kind.Valid() {
			return Plan{}, ErrInput
		}
		kinds[index] = string(kind)
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Plan{}, fmt.Errorf("begin object migration plan: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var createdAt time.Time
	err = tx.QueryRow(ctx, `
INSERT INTO storage.migrations (
    id, mode, object_kinds, source_backend_id, destination_backend_id,
    status, requested_by, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, 'copying', $6, $7, $7)
RETURNING created_at`,
		input.ID, string(input.Mode), kinds, string(input.SourceBackendID),
		string(input.DestinationBackendID), input.RequestedBy, input.OccurredAt,
	).Scan(&createdAt)
	if uniqueViolation(err) {
		return Plan{}, ErrStateConflict
	}
	if err != nil {
		return Plan{}, fmt.Errorf("insert object migration: %w", err)
	}

	command, err := tx.Exec(ctx, snapshotItemsSQL,
		input.ID, kinds, string(input.SourceBackendID), input.OccurredAt,
	)
	if err != nil {
		return Plan{}, fmt.Errorf("snapshot object migration items: %w", err)
	}
	count := command.RowsAffected()
	if count == 0 {
		if _, err := tx.Exec(ctx, `
UPDATE storage.migrations
SET status = 'completed', completed_at = $2, updated_at = $2, version = version + 1
WHERE id = $1 AND status = 'copying'`, input.ID, input.OccurredAt); err != nil {
			return Plan{}, fmt.Errorf("complete empty object migration: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Plan{}, fmt.Errorf("commit object migration plan: %w", err)
	}
	return Plan{
		ID: input.ID, Mode: input.Mode, Kinds: append([]Kind(nil), input.Kinds...),
		SourceBackendID: input.SourceBackendID, DestinationBackendID: input.DestinationBackendID,
		ObjectCount: count, CreatedAt: createdAt.UTC(),
	}, nil
}

const snapshotItemsSQL = `
WITH candidates AS (
    SELECT
        'torrent'::text AS object_kind,
        object.id AS torrent_object_id, NULL::uuid AS screenshot_object_id,
        NULL::uuid AS avatar_object_id, NULL::uuid AS derivative_object_id,
        source.id AS torrent_source_location_id, NULL::uuid AS screenshot_source_location_id,
        NULL::uuid AS avatar_source_location_id, NULL::uuid AS derivative_source_location_id,
        object.content_sha256, object.byte_length,
        source.object_key AS source_object_key, source.version_id AS source_version_id,
        'torrents/sha256/' || left(encode(object.content_sha256, 'hex'), 2) || '/'
            || encode(object.content_sha256, 'hex') || '.torrent' AS destination_object_key
    FROM torrents.torrent_objects AS object
    JOIN torrents.torrent_object_locations AS source
      ON source.object_id = object.id AND source.backend_id = $3
     AND source.state = 'verified' AND source.is_preferred
    WHERE 'torrent' = ANY($2::text[])

    UNION ALL

    SELECT
        'torrent_screenshot', NULL, object.id, NULL, NULL,
        NULL, source.id, NULL, NULL,
        object.content_sha256, object.byte_length, source.object_key, source.version_id,
        'torrent-screenshots/sha256/' || left(encode(object.content_sha256, 'hex'), 2) || '/'
            || encode(object.content_sha256, 'hex')
            || CASE object.content_type WHEN 'image/jpeg' THEN '.jpg' WHEN 'image/png' THEN '.png'
                 WHEN 'image/webp' THEN '.webp' WHEN 'image/gif' THEN '.gif' END
    FROM torrents.torrent_screenshot_objects AS object
    JOIN torrents.torrent_screenshot_object_locations AS source
      ON source.object_id = object.id AND source.backend_id = $3
     AND source.state = 'verified' AND source.is_preferred
    WHERE 'torrent_screenshot' = ANY($2::text[])

    UNION ALL

    SELECT
        'avatar', NULL, NULL, object.id, NULL,
        NULL, NULL, source.id, NULL,
        object.content_sha256, object.byte_length, source.object_key, source.version_id,
        'avatars/sha256/' || left(encode(object.content_sha256, 'hex'), 2) || '/'
            || encode(object.content_sha256, 'hex') || object.extension
    FROM identity.user_avatar_objects AS object
    JOIN identity.user_avatar_object_locations AS source
      ON source.object_id = object.id AND source.backend_id = $3
     AND source.state = 'verified' AND source.is_preferred
    WHERE 'avatar' = ANY($2::text[])

    UNION ALL

    SELECT
        'image_derivative', NULL, NULL, NULL, object.id,
        NULL, NULL, NULL, source.id,
        object.content_sha256, object.byte_length, source.object_key, source.version_id,
        'image-derivatives/webp-v1/sha256/' || left(encode(object.content_sha256, 'hex'), 2) || '/'
            || encode(object.content_sha256, 'hex') || '.webp'
    FROM media.image_derivative_objects AS object
    JOIN media.image_derivative_object_locations AS source
      ON source.object_id = object.id AND source.backend_id = $3
     AND source.state = 'verified' AND source.is_preferred
    WHERE 'image_derivative' = ANY($2::text[])
)
INSERT INTO storage.migration_items (
    migration_id, object_kind,
    torrent_object_id, screenshot_object_id, avatar_object_id, derivative_object_id,
    torrent_source_location_id, screenshot_source_location_id,
    avatar_source_location_id, derivative_source_location_id,
    content_sha256, byte_length, source_object_key, source_version_id,
    destination_object_key, available_at, created_at, updated_at
)
SELECT
    $1, object_kind,
    torrent_object_id, screenshot_object_id, avatar_object_id, derivative_object_id,
    torrent_source_location_id, screenshot_source_location_id,
    avatar_source_location_id, derivative_source_location_id,
    content_sha256, byte_length, source_object_key, source_version_id,
    destination_object_key, $4, $4, $4
FROM candidates
ORDER BY object_kind,
         COALESCE(torrent_object_id, screenshot_object_id, avatar_object_id, derivative_object_id)`

func (repository *PostgresRepository) ClaimCopyTasks(ctx context.Context, migrationID uuid.UUID, now time.Time, batchSize int32, leaseDuration time.Duration) ([]CopyTask, error) {
	if migrationID == uuid.Nil || now.IsZero() || batchSize < 1 || batchSize > 100 || leaseDuration <= 0 || leaseDuration > 10*time.Minute {
		return nil, ErrInput
	}
	rows, err := repository.pool.Query(ctx, `
WITH candidates AS (
    SELECT item.id
    FROM storage.migration_items AS item
    JOIN storage.migrations AS migration ON migration.id = item.migration_id
    WHERE item.migration_id = $1 AND migration.status = 'copying'
      AND item.state IN ('pending', 'copy_failed') AND item.available_at <= $2
      AND (item.lease_until IS NULL OR item.lease_until <= $2)
    ORDER BY item.available_at, item.object_kind, item.id
    FOR UPDATE OF item SKIP LOCKED
    LIMIT $3
), claimed AS (
    UPDATE storage.migration_items AS item
    SET state = 'copying', attempts = attempts + 1, lease_until = $4,
        lease_token = gen_random_uuid(), last_error_code = NULL, updated_at = $2
    FROM candidates
    WHERE item.id = candidates.id
    RETURNING item.*
)
SELECT claimed.id, claimed.migration_id, claimed.object_kind,
       COALESCE(claimed.torrent_object_id, claimed.screenshot_object_id,
                claimed.avatar_object_id, claimed.derivative_object_id),
       claimed.content_sha256, claimed.byte_length,
       migration.source_backend_id, claimed.source_object_key,
       COALESCE(claimed.source_version_id, ''), migration.destination_backend_id,
       claimed.destination_object_key, claimed.lease_token, claimed.attempts
FROM claimed
JOIN storage.migrations AS migration ON migration.id = claimed.migration_id
ORDER BY claimed.object_kind, claimed.id`,
		migrationID, now, batchSize, now.Add(leaseDuration),
	)
	if err != nil {
		return nil, fmt.Errorf("claim object copy rows: %w", err)
	}
	defer rows.Close()
	var tasks []CopyTask
	for rows.Next() {
		var task CopyTask
		var kind, sourceBackend, sourceKey, destinationBackend, destinationKey string
		var digest []byte
		if err := rows.Scan(
			&task.ItemID, &task.MigrationID, &kind, &task.ObjectID,
			&digest, &task.Descriptor.ByteLength, &sourceBackend, &sourceKey,
			&task.SourceVersionID, &destinationBackend, &destinationKey,
			&task.LeaseToken, &task.Attempts,
		); err != nil {
			return nil, fmt.Errorf("scan object copy row: %w", err)
		}
		task.Kind = Kind(kind)
		var parseErr error
		task.SourceBackendID, parseErr = objectstorage.ParseBackendID(sourceBackend)
		if parseErr == nil {
			task.DestinationBackendID, parseErr = objectstorage.ParseBackendID(destinationBackend)
		}
		if parseErr == nil {
			task.SourceObjectKey, parseErr = objectstorage.ParseKey(sourceKey)
		}
		if parseErr == nil {
			task.DestinationObjectKey, parseErr = objectstorage.ParseKey(destinationKey)
		}
		if parseErr != nil || !task.Kind.Valid() || len(digest) != 32 || task.ItemID == uuid.Nil ||
			task.ObjectID == uuid.Nil || task.LeaseToken == uuid.Nil || task.SourceBackendID == task.DestinationBackendID {
			return nil, errors.New("claimed object copy has invalid immutable metadata")
		}
		copy(task.Descriptor.SHA256[:], digest)
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate object copy rows: %w", err)
	}
	return tasks, nil
}

type locationSpec struct {
	table             string
	objectColumn      string
	sourceColumn      string
	destinationColumn string
	reuseDeleted      bool
}

// locationSpecFor is the only source of SQL identifiers in this package. The
// switch is intentionally closed over Kind so no caller-controlled identifier
// can enter a query while the database retains a typed foreign key per domain.
func locationSpecFor(kind Kind) (locationSpec, error) {
	switch kind {
	case KindTorrent:
		return locationSpec{"torrents.torrent_object_locations", "torrent_object_id", "torrent_source_location_id", "torrent_destination_location_id", false}, nil
	case KindTorrentScreenshot:
		return locationSpec{"torrents.torrent_screenshot_object_locations", "screenshot_object_id", "screenshot_source_location_id", "screenshot_destination_location_id", true}, nil
	case KindAvatar:
		return locationSpec{"identity.user_avatar_object_locations", "avatar_object_id", "avatar_source_location_id", "avatar_destination_location_id", true}, nil
	case KindImageDerivative:
		return locationSpec{"media.image_derivative_object_locations", "derivative_object_id", "derivative_source_location_id", "derivative_destination_location_id", true}, nil
	default:
		return locationSpec{}, ErrInput
	}
}

func (repository *PostgresRepository) MarkCopyVerified(ctx context.Context, task CopyTask, location VerifiedLocation) error {
	if task.ItemID == uuid.Nil || task.MigrationID == uuid.Nil || task.ObjectID == uuid.Nil || task.LeaseToken == uuid.Nil ||
		!task.Kind.Valid() || location.BackendID != task.DestinationBackendID ||
		location.ObjectKey != task.DestinationObjectKey || location.Descriptor != task.Descriptor ||
		!location.Descriptor.Valid() || location.VerifiedAt.IsZero() {
		return ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin verified object location: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var lockedKind string
	var lockedObject uuid.UUID
	var lockedKey string
	var lockedDigest []byte
	var lockedLength int64
	err = tx.QueryRow(ctx, `
SELECT object_kind,
       COALESCE(torrent_object_id, screenshot_object_id, avatar_object_id, derivative_object_id),
       destination_object_key, content_sha256, byte_length
FROM storage.migration_items
WHERE id = $1 AND migration_id = $2 AND state = 'copying' AND lease_token = $3
FOR UPDATE`, task.ItemID, task.MigrationID, task.LeaseToken).Scan(
		&lockedKind, &lockedObject, &lockedKey, &lockedDigest, &lockedLength,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStateConflict
	}
	if err != nil {
		return fmt.Errorf("lock object copy item: %w", err)
	}
	if Kind(lockedKind) != task.Kind || lockedObject != task.ObjectID || lockedKey != string(location.ObjectKey) ||
		lockedLength != location.Descriptor.ByteLength || !bytes.Equal(lockedDigest, location.Descriptor.SHA256[:]) {
		return ErrStateConflict
	}
	locationID, err := ensureVerifiedLocation(ctx, tx, task, location)
	if err != nil {
		return err
	}
	spec, _ := locationSpecFor(task.Kind)
	command, err := tx.Exec(ctx, fmt.Sprintf(`
UPDATE storage.migration_items
SET %s = $1, state = 'verified', lease_until = NULL, lease_token = NULL,
    copied_at = $2, verified_at = $2, last_error_code = NULL, updated_at = $2
WHERE id = $3 AND migration_id = $4 AND state = 'copying' AND lease_token = $5`, spec.destinationColumn),
		locationID, location.VerifiedAt, task.ItemID, task.MigrationID, task.LeaseToken,
	)
	if err != nil {
		return fmt.Errorf("mark object copy verified: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrStateConflict
	}
	if _, err := tx.Exec(ctx, `
UPDATE storage.migrations AS migration
SET status = CASE WHEN mode = 'replicate' THEN 'completed' ELSE 'ready_for_cutover' END,
    completed_at = CASE WHEN mode = 'replicate' THEN $2 ELSE NULL END,
    updated_at = $2, version = version + 1
WHERE id = $1 AND status = 'copying'
  AND NOT EXISTS (
      SELECT 1 FROM storage.migration_items AS item
      WHERE item.migration_id = migration.id AND item.state <> 'verified'
  )`, task.MigrationID, location.VerifiedAt); err != nil {
		return fmt.Errorf("advance object migration after copy: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit verified object location: %w", err)
	}
	return nil
}

func ensureVerifiedLocation(ctx context.Context, tx pgx.Tx, task CopyTask, location VerifiedLocation) (uuid.UUID, error) {
	spec, err := locationSpecFor(task.Kind)
	if err != nil {
		return uuid.Nil, err
	}
	query := fmt.Sprintf(`
SELECT id, object_key, state, COALESCE(version_id, ''),
       observed_byte_length, observed_sha256
FROM %s
WHERE object_id = $1 AND backend_id = $2 %s
ORDER BY verified_at DESC, id
LIMIT 1
FOR UPDATE`, spec.table, map[bool]string{true: "", false: "AND state <> 'deleted'"}[spec.reuseDeleted])
	var id uuid.UUID
	var key, state, version string
	var length int64
	var digest []byte
	err = tx.QueryRow(ctx, query, task.ObjectID, string(location.BackendID)).Scan(
		&id, &key, &state, &version, &length, &digest,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		id = uuid.New()
		_, err = tx.Exec(ctx, fmt.Sprintf(`
INSERT INTO %s (
    id, object_id, backend_id, object_key, state, is_preferred, version_id,
    observed_byte_length, observed_sha256, verified_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, 'verified', false, NULLIF($5, ''), $6, $7, $8, $8, $8)`, spec.table),
			id, task.ObjectID, string(location.BackendID), string(location.ObjectKey),
			location.VersionID, location.Descriptor.ByteLength, location.Descriptor.SHA256[:], location.VerifiedAt,
		)
		if uniqueViolation(err) {
			return uuid.Nil, ErrStateConflict
		}
		if err != nil {
			return uuid.Nil, fmt.Errorf("insert verified %s location: %w", task.Kind, err)
		}
		return id, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lock existing %s location: %w", task.Kind, err)
	}
	if key != string(location.ObjectKey) || length != location.Descriptor.ByteLength || !bytes.Equal(digest, location.Descriptor.SHA256[:]) {
		return uuid.Nil, ErrStateConflict
	}
	if state == "verified" {
		return id, nil
	}
	if state != "retiring" && !(spec.reuseDeleted && state == "deleted") {
		return uuid.Nil, ErrStateConflict
	}
	command, err := tx.Exec(ctx, fmt.Sprintf(`
UPDATE %s
SET state = 'verified', is_preferred = false, version_id = NULLIF($2, ''),
    verified_at = $3, retiring_at = NULL, deleted_at = NULL,
    last_error_code = NULL, updated_at = $3, version = version + 1
WHERE id = $1 AND state = $4`, spec.table), id, location.VersionID, location.VerifiedAt, state)
	if err != nil {
		return uuid.Nil, fmt.Errorf("reactivate verified %s location: %w", task.Kind, err)
	}
	if command.RowsAffected() != 1 {
		return uuid.Nil, ErrStateConflict
	}
	_ = version
	return id, nil
}

func (repository *PostgresRepository) ReleaseCopyTask(ctx context.Context, task CopyTask, retryAt time.Time, errorCode string) error {
	if task.ItemID == uuid.Nil || task.MigrationID == uuid.Nil || task.LeaseToken == uuid.Nil || retryAt.IsZero() || !validErrorCode(errorCode) {
		return ErrInput
	}
	command, err := repository.pool.Exec(ctx, `
UPDATE storage.migration_items
SET state = 'copy_failed', available_at = $1, lease_until = NULL,
    lease_token = NULL, last_error_code = $2, updated_at = $3
WHERE id = $4 AND migration_id = $5 AND state = 'copying' AND lease_token = $6`,
		retryAt, errorCode, time.Now().UTC(), task.ItemID, task.MigrationID, task.LeaseToken,
	)
	if err != nil {
		return fmt.Errorf("release object copy task: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrStateConflict
	}
	return nil
}

func (repository *PostgresRepository) RetryFailures(ctx context.Context, migrationID uuid.UUID, now time.Time) (int64, error) {
	if migrationID == uuid.Nil || now.IsZero() {
		return 0, ErrInput
	}
	command, err := repository.pool.Exec(ctx, `
UPDATE storage.migration_items AS item
SET available_at = $2, last_error_code = NULL, updated_at = $2
FROM storage.migrations AS migration
WHERE item.migration_id = migration.id AND migration.id = $1
  AND ((migration.status = 'copying' AND item.state = 'copy_failed')
       OR (migration.status = 'cleaning' AND item.state = 'cleanup_failed'))`, migrationID, now)
	if err != nil {
		return 0, fmt.Errorf("retry object migration failures: %w", err)
	}
	return command.RowsAffected(), nil
}

func (repository *PostgresRepository) Cutover(ctx context.Context, migrationID uuid.UUID, cutoverAt, retentionUntil time.Time) error {
	if migrationID == uuid.Nil || cutoverAt.IsZero() || !retentionUntil.After(cutoverAt) {
		return ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin object migration cutover: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var mode, status, sourceBackend, destinationBackend string
	err = tx.QueryRow(ctx, `
SELECT mode, status, source_backend_id, destination_backend_id
FROM storage.migrations WHERE id = $1 FOR UPDATE`, migrationID).Scan(
		&mode, &status, &sourceBackend, &destinationBackend,
	)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (mode != string(ModeMove) || status != "ready_for_cutover") {
		return ErrStateConflict
	}
	if err != nil {
		return fmt.Errorf("lock object migration for cutover: %w", err)
	}
	var total, unverified int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM storage.migration_items WHERE migration_id = $1`, migrationID).Scan(&total); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, verifyDestinationItemsSQL, migrationID).Scan(&unverified); err != nil {
		return fmt.Errorf("reconcile destination locations: %w", err)
	}
	if total < 1 || unverified != 0 {
		return ErrStateConflict
	}
	retired, preferred := int64(0), int64(0)
	for _, kind := range AllKinds {
		spec, _ := locationSpecFor(kind)
		command, err := tx.Exec(ctx, fmt.Sprintf(`
UPDATE %s AS location
SET state = 'retiring', is_preferred = false, retiring_at = $2,
    updated_at = $2, version = version + 1
FROM storage.migration_items AS item
WHERE item.migration_id = $1 AND item.object_kind = $3
  AND item.%s = location.id AND item.state = 'verified'
  AND location.state = 'verified' AND location.is_preferred`, spec.table, spec.sourceColumn),
			migrationID, cutoverAt, string(kind))
		if err != nil {
			return fmt.Errorf("retire %s source locations: %w", kind, err)
		}
		retired += command.RowsAffected()
		command, err = tx.Exec(ctx, fmt.Sprintf(`
UPDATE %s AS location
SET state = 'verified', is_preferred = true, retiring_at = NULL,
    deleted_at = NULL, updated_at = $2, version = version + 1
FROM storage.migration_items AS item
WHERE item.migration_id = $1 AND item.object_kind = $3
  AND item.%s = location.id AND item.state = 'verified'
  AND location.state = 'verified' AND NOT location.is_preferred`, spec.table, spec.destinationColumn),
			migrationID, cutoverAt, string(kind))
		if err != nil {
			return fmt.Errorf("prefer %s destination locations: %w", kind, err)
		}
		preferred += command.RowsAffected()
	}
	if retired != total || preferred != total {
		return ErrStateConflict
	}
	command, err := tx.Exec(ctx, `
UPDATE storage.migrations
SET status = 'retaining', cutover_at = $2, retention_until = $3,
    updated_at = $2, version = version + 1
WHERE id = $1 AND mode = 'move' AND status = 'ready_for_cutover'`, migrationID, cutoverAt, retentionUntil)
	if err != nil || command.RowsAffected() != 1 {
		if err != nil {
			return fmt.Errorf("mark object migration retaining: %w", err)
		}
		return ErrStateConflict
	}
	// A verified reverse move makes the earlier retained source current again.
	// Cancel that obsolete cleanup window so it can never delete the restored
	// preferred bytes after a local -> object -> local round trip.
	if _, err := tx.Exec(ctx, `
UPDATE storage.migrations
SET status = 'cancelled', completed_at = $3, updated_at = $3, version = version + 1
WHERE id <> $1 AND status = 'retaining'
  AND source_backend_id = $2 AND destination_backend_id = $4`,
		migrationID, destinationBackend, cutoverAt, sourceBackend); err != nil {
		return fmt.Errorf("cancel superseded reverse storage cleanup: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit object migration cutover: %w", err)
	}
	return nil
}

const verifyDestinationItemsSQL = `
SELECT count(*)
FROM storage.migration_items AS item
LEFT JOIN torrents.torrent_object_locations AS torrent_location
  ON torrent_location.id = item.torrent_destination_location_id
LEFT JOIN torrents.torrent_screenshot_object_locations AS screenshot_location
  ON screenshot_location.id = item.screenshot_destination_location_id
LEFT JOIN identity.user_avatar_object_locations AS avatar_location
  ON avatar_location.id = item.avatar_destination_location_id
LEFT JOIN media.image_derivative_object_locations AS derivative_location
  ON derivative_location.id = item.derivative_destination_location_id
WHERE item.migration_id = $1 AND (
    item.state <> 'verified'
    OR (item.object_kind = 'torrent' AND (
        torrent_location.state <> 'verified'
        OR torrent_location.observed_byte_length IS DISTINCT FROM item.byte_length
        OR torrent_location.observed_sha256 IS DISTINCT FROM item.content_sha256))
    OR (item.object_kind = 'torrent_screenshot' AND (
        screenshot_location.state <> 'verified'
        OR screenshot_location.observed_byte_length IS DISTINCT FROM item.byte_length
        OR screenshot_location.observed_sha256 IS DISTINCT FROM item.content_sha256))
    OR (item.object_kind = 'avatar' AND (
        avatar_location.state <> 'verified'
        OR avatar_location.observed_byte_length IS DISTINCT FROM item.byte_length
        OR avatar_location.observed_sha256 IS DISTINCT FROM item.content_sha256))
    OR (item.object_kind = 'image_derivative' AND (
        derivative_location.state <> 'verified'
        OR derivative_location.observed_byte_length IS DISTINCT FROM item.byte_length
        OR derivative_location.observed_sha256 IS DISTINCT FROM item.content_sha256))
)`

func (repository *PostgresRepository) ApproveCleanup(ctx context.Context, migrationID, approvedBy uuid.UUID, approvedAt time.Time) error {
	if migrationID == uuid.Nil || approvedBy == uuid.Nil || approvedAt.IsZero() {
		return ErrInput
	}
	command, err := repository.pool.Exec(ctx, `
UPDATE storage.migrations
SET status = 'cleaning', cleanup_approved_by = $2, cleanup_approved_at = $3,
    updated_at = $3, version = version + 1
WHERE id = $1 AND mode = 'move' AND status = 'retaining' AND retention_until <= $3`,
		migrationID, approvedBy, approvedAt)
	if err != nil {
		return fmt.Errorf("approve object migration cleanup: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrStateConflict
	}
	return nil
}

func (repository *PostgresRepository) ClaimCleanupTasks(ctx context.Context, migrationID uuid.UUID, now time.Time, batchSize int32, leaseDuration time.Duration) ([]CleanupTask, error) {
	if migrationID == uuid.Nil || now.IsZero() || batchSize < 1 || batchSize > 100 || leaseDuration <= 0 || leaseDuration > 10*time.Minute {
		return nil, ErrInput
	}
	rows, err := repository.pool.Query(ctx, `
WITH candidates AS (
    SELECT item.id
    FROM storage.migration_items AS item
    JOIN storage.migrations AS migration ON migration.id = item.migration_id
    LEFT JOIN torrents.torrent_object_locations AS torrent_location
      ON torrent_location.id = item.torrent_source_location_id
    LEFT JOIN torrents.torrent_screenshot_object_locations AS screenshot_location
      ON screenshot_location.id = item.screenshot_source_location_id
    LEFT JOIN identity.user_avatar_object_locations AS avatar_location
      ON avatar_location.id = item.avatar_source_location_id
    LEFT JOIN media.image_derivative_object_locations AS derivative_location
      ON derivative_location.id = item.derivative_source_location_id
    WHERE item.migration_id = $1 AND migration.status = 'cleaning'
      AND migration.cleanup_approved_at IS NOT NULL AND migration.retention_until <= $2
      AND item.state IN ('verified', 'cleanup_failed') AND item.available_at <= $2
      AND (item.lease_until IS NULL OR item.lease_until <= $2)
      AND CASE item.object_kind
          WHEN 'torrent' THEN torrent_location.state = 'retiring' AND NOT torrent_location.is_preferred
          WHEN 'torrent_screenshot' THEN screenshot_location.state = 'retiring' AND NOT screenshot_location.is_preferred
          WHEN 'avatar' THEN avatar_location.state = 'retiring' AND NOT avatar_location.is_preferred
          WHEN 'image_derivative' THEN derivative_location.state = 'retiring' AND NOT derivative_location.is_preferred
          ELSE false END
    ORDER BY item.available_at, item.object_kind, item.id
    FOR UPDATE OF item SKIP LOCKED
    LIMIT $3
), claimed AS (
    UPDATE storage.migration_items AS item
    SET state = 'deleting_source', attempts = attempts + 1, lease_until = $4,
        lease_token = gen_random_uuid(), last_error_code = NULL, updated_at = $2
    FROM candidates WHERE item.id = candidates.id
    RETURNING item.*
)
SELECT claimed.id, claimed.migration_id, claimed.object_kind,
       COALESCE(claimed.torrent_object_id, claimed.screenshot_object_id,
                claimed.avatar_object_id, claimed.derivative_object_id),
       migration.source_backend_id, claimed.source_object_key,
       COALESCE(claimed.source_version_id, ''), claimed.lease_token, claimed.attempts
FROM claimed JOIN storage.migrations AS migration ON migration.id = claimed.migration_id
ORDER BY claimed.object_kind, claimed.id`, migrationID, now, batchSize, now.Add(leaseDuration))
	if err != nil {
		return nil, fmt.Errorf("claim object cleanup rows: %w", err)
	}
	defer rows.Close()
	var tasks []CleanupTask
	for rows.Next() {
		var task CleanupTask
		var kind, backend, key string
		if err := rows.Scan(
			&task.ItemID, &task.MigrationID, &kind, &task.ObjectID,
			&backend, &key, &task.SourceVersionID, &task.LeaseToken, &task.Attempts,
		); err != nil {
			return nil, err
		}
		task.Kind = Kind(kind)
		var parseErr error
		task.SourceBackendID, parseErr = objectstorage.ParseBackendID(backend)
		if parseErr == nil {
			task.SourceObjectKey, parseErr = objectstorage.ParseKey(key)
		}
		if parseErr != nil || !task.Kind.Valid() || task.ItemID == uuid.Nil || task.ObjectID == uuid.Nil || task.LeaseToken == uuid.Nil {
			return nil, errors.New("claimed object cleanup has invalid metadata")
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (repository *PostgresRepository) MarkSourceDeleted(ctx context.Context, task CleanupTask, deletedAt time.Time) error {
	if task.ItemID == uuid.Nil || task.MigrationID == uuid.Nil || task.LeaseToken == uuid.Nil || !task.Kind.Valid() || deletedAt.IsZero() {
		return ErrInput
	}
	spec, _ := locationSpecFor(task.Kind)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locationID uuid.UUID
	err = tx.QueryRow(ctx, fmt.Sprintf(`
SELECT %s FROM storage.migration_items
WHERE id = $1 AND migration_id = $2 AND state = 'deleting_source' AND lease_token = $3
FOR UPDATE`, spec.sourceColumn), task.ItemID, task.MigrationID, task.LeaseToken).Scan(&locationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStateConflict
	}
	if err != nil {
		return err
	}
	command, err := tx.Exec(ctx, fmt.Sprintf(`
UPDATE %s
SET state = 'deleted', deleted_at = $2, updated_at = $2, version = version + 1
WHERE id = $1 AND state = 'retiring' AND NOT is_preferred`, spec.table), locationID, deletedAt)
	if err != nil {
		return fmt.Errorf("mark %s source location deleted: %w", task.Kind, err)
	}
	if command.RowsAffected() != 1 {
		return ErrStateConflict
	}
	command, err = tx.Exec(ctx, `
UPDATE storage.migration_items
SET state = 'source_deleted', lease_until = NULL, lease_token = NULL,
    source_deleted_at = $2, last_error_code = NULL, updated_at = $2
WHERE id = $1 AND migration_id = $3 AND state = 'deleting_source' AND lease_token = $4`,
		task.ItemID, deletedAt, task.MigrationID, task.LeaseToken)
	if err != nil || command.RowsAffected() != 1 {
		if err != nil {
			return err
		}
		return ErrStateConflict
	}
	if _, err := tx.Exec(ctx, `
UPDATE storage.migrations AS migration
SET status = 'completed', completed_at = $2, updated_at = $2, version = version + 1
WHERE id = $1 AND status = 'cleaning'
  AND NOT EXISTS (
      SELECT 1 FROM storage.migration_items AS item
      WHERE item.migration_id = migration.id AND item.state <> 'source_deleted'
  )`, task.MigrationID, deletedAt); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (repository *PostgresRepository) ReleaseCleanupTask(ctx context.Context, task CleanupTask, retryAt time.Time, errorCode string) error {
	if task.ItemID == uuid.Nil || task.MigrationID == uuid.Nil || task.LeaseToken == uuid.Nil || retryAt.IsZero() || !validErrorCode(errorCode) {
		return ErrInput
	}
	command, err := repository.pool.Exec(ctx, `
UPDATE storage.migration_items
SET state = 'cleanup_failed', available_at = $1, lease_until = NULL,
    lease_token = NULL, last_error_code = $2, updated_at = $3
WHERE id = $4 AND migration_id = $5 AND state = 'deleting_source' AND lease_token = $6`,
		retryAt, errorCode, time.Now().UTC(), task.ItemID, task.MigrationID, task.LeaseToken)
	if err != nil {
		return fmt.Errorf("release object cleanup task: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrStateConflict
	}
	return nil
}

func validErrorCode(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	_, err := objectstorage.ParseBackendID(value)
	return err == nil
}

func uniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

var _ Repository = (*PostgresRepository)(nil)
