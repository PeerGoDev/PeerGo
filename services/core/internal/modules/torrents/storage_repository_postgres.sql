-- name: InsertStorageMigration :one
INSERT INTO torrents.storage_migrations (
    id,
    mode,
    source_backend_id,
    destination_backend_id,
    status,
    requested_by,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(migration_id)::uuid,
    sqlc.arg(migration_mode)::text,
    sqlc.arg(source_backend_id)::text,
    sqlc.arg(destination_backend_id)::text,
    'copying',
    sqlc.arg(requested_by)::uuid,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(occurred_at)::timestamptz
)
RETURNING id, mode, source_backend_id, destination_backend_id, created_at;

-- name: SnapshotStorageMigrationItems :execrows
INSERT INTO torrents.storage_migration_items (
    migration_id,
    object_id,
    source_location_id,
    destination_object_key,
    available_at,
    created_at,
    updated_at
)
SELECT
    sqlc.arg(migration_id)::uuid,
    object.id,
    source.id,
    'torrents/sha256/' || substring(encode(object.content_sha256, 'hex') FROM 1 FOR 2)
        || '/' || encode(object.content_sha256, 'hex') || '.torrent',
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(occurred_at)::timestamptz
FROM torrents.torrent_objects AS object
JOIN torrents.torrent_object_locations AS source
    ON source.object_id = object.id
   AND source.backend_id = sqlc.arg(source_backend_id)::text
   AND source.state = 'verified'
   AND source.is_preferred = true
ORDER BY object.id;

-- name: CompleteEmptyStorageMigration :execrows
UPDATE torrents.storage_migrations
SET status = 'completed',
    completed_at = sqlc.arg(occurred_at)::timestamptz,
    updated_at = sqlc.arg(occurred_at)::timestamptz,
    version = version + 1
WHERE id = sqlc.arg(migration_id)::uuid
  AND status = 'copying'
  AND NOT EXISTS (
      SELECT 1
      FROM torrents.storage_migration_items AS item
      WHERE item.migration_id = torrents.storage_migrations.id
  );

-- name: ClaimStorageCopyTasks :many
WITH candidates AS (
    SELECT
        item.migration_id,
        item.object_id,
        migration.source_backend_id,
        migration.destination_backend_id,
        source.object_key AS source_object_key,
        source.version_id AS source_version_id,
        object.content_sha256,
        object.byte_length
    FROM torrents.storage_migration_items AS item
    JOIN torrents.storage_migrations AS migration
        ON migration.id = item.migration_id
       AND migration.status = 'copying'
       AND migration.id = sqlc.arg(migration_id)::uuid
    JOIN torrents.torrent_objects AS object
        ON object.id = item.object_id
    JOIN torrents.torrent_object_locations AS source
        ON source.id = item.source_location_id
       AND source.state = 'verified'
    WHERE item.state IN ('pending', 'copy_failed')
      AND item.available_at <= sqlc.arg(claimed_at)::timestamptz
      AND (item.lease_until IS NULL OR item.lease_until <= sqlc.arg(claimed_at)::timestamptz)
    ORDER BY item.available_at, item.migration_id, item.object_id
    FOR UPDATE OF item SKIP LOCKED
    LIMIT sqlc.arg(batch_size)::integer
), claimed AS (
    UPDATE torrents.storage_migration_items AS item
    SET state = 'copying',
        attempts = item.attempts + 1,
        lease_until = sqlc.arg(lease_until)::timestamptz,
        lease_token = gen_random_uuid(),
        last_error_code = NULL,
        updated_at = sqlc.arg(claimed_at)::timestamptz
    FROM candidates
    WHERE item.migration_id = candidates.migration_id
      AND item.object_id = candidates.object_id
    RETURNING
        item.migration_id,
        item.object_id,
        item.lease_token,
        item.attempts,
        candidates.source_backend_id,
        candidates.destination_backend_id,
        candidates.source_object_key,
        candidates.source_version_id,
        candidates.content_sha256,
        candidates.byte_length
)
SELECT * FROM claimed;

-- name: LockStorageMigrationItem :one
SELECT
    item.state,
    item.destination_object_key,
    migration.destination_backend_id,
    object.content_sha256,
    object.byte_length
FROM torrents.storage_migration_items AS item
JOIN torrents.storage_migrations AS migration ON migration.id = item.migration_id
JOIN torrents.torrent_objects AS object ON object.id = item.object_id
WHERE item.migration_id = sqlc.arg(migration_id)::uuid
  AND item.object_id = sqlc.arg(object_id)::uuid
  AND item.lease_token = sqlc.arg(lease_token)::uuid
  AND item.state = 'copying'
FOR UPDATE OF item;

-- name: GetTorrentObjectLocationForUpdate :one
SELECT
    id,
    object_id,
    backend_id,
    object_key,
    state,
    is_preferred,
    version_id,
    observed_byte_length,
    observed_sha256,
    verified_at,
    version
FROM torrents.torrent_object_locations
WHERE object_id = sqlc.arg(object_id)::uuid
  AND backend_id = sqlc.arg(backend_id)::text
  AND state <> 'deleted'
FOR UPDATE;

-- name: InsertVerifiedTorrentObjectLocation :one
INSERT INTO torrents.torrent_object_locations (
    id,
    object_id,
    backend_id,
    object_key,
    state,
    is_preferred,
    version_id,
    observed_byte_length,
    observed_sha256,
    verified_at,
    created_at,
    updated_at
) VALUES (
    sqlc.arg(location_id)::uuid,
    sqlc.arg(object_id)::uuid,
    sqlc.arg(backend_id)::text,
    sqlc.arg(object_key)::text,
    'verified',
    false,
    sqlc.narg(version_id)::text,
    sqlc.arg(observed_byte_length)::bigint,
    sqlc.arg(observed_sha256)::bytea,
    sqlc.arg(verified_at)::timestamptz,
    sqlc.arg(verified_at)::timestamptz,
    sqlc.arg(verified_at)::timestamptz
)
RETURNING id;

-- name: RestoreRetiringTorrentObjectLocation :one
UPDATE torrents.torrent_object_locations
SET state = 'verified',
    retiring_at = NULL,
    last_error_code = NULL,
    updated_at = sqlc.arg(verified_at)::timestamptz,
    version = version + 1
WHERE id = sqlc.arg(location_id)::uuid
  AND state = 'retiring'
  AND is_preferred = false
  AND object_key = sqlc.arg(object_key)::text
  AND observed_byte_length = sqlc.arg(observed_byte_length)::bigint
  AND observed_sha256 = sqlc.arg(observed_sha256)::bytea
  AND version = sqlc.arg(expected_version)::bigint
RETURNING id;

-- name: PromotePendingTorrentObjectLocation :one
UPDATE torrents.torrent_object_locations
SET state = 'verified',
    version_id = sqlc.narg(version_id)::text,
    observed_byte_length = sqlc.arg(observed_byte_length)::bigint,
    observed_sha256 = sqlc.arg(observed_sha256)::bytea,
    verified_at = sqlc.arg(verified_at)::timestamptz,
    last_error_code = NULL,
    updated_at = sqlc.arg(verified_at)::timestamptz,
    version = version + 1
WHERE id = sqlc.arg(location_id)::uuid
  AND state IN ('pending', 'failed')
  AND version = sqlc.arg(expected_version)::bigint
RETURNING id;

-- name: MarkStorageCopyTaskVerified :execrows
UPDATE torrents.storage_migration_items
SET destination_location_id = sqlc.arg(destination_location_id)::uuid,
    state = 'verified',
    lease_until = NULL,
    lease_token = NULL,
    copied_at = sqlc.arg(verified_at)::timestamptz,
    verified_at = sqlc.arg(verified_at)::timestamptz,
    last_error_code = NULL,
    updated_at = sqlc.arg(verified_at)::timestamptz
WHERE migration_id = sqlc.arg(migration_id)::uuid
  AND object_id = sqlc.arg(object_id)::uuid
  AND state = 'copying'
  AND lease_token = sqlc.arg(lease_token)::uuid;

-- name: AdvanceStorageMigrationAfterCopy :execrows
UPDATE torrents.storage_migrations AS migration
SET status = CASE
        WHEN migration.mode = 'replicate' THEN 'completed'
        ELSE 'ready_for_cutover'
    END,
    completed_at = CASE
        WHEN migration.mode = 'replicate' THEN sqlc.arg(occurred_at)::timestamptz
        ELSE NULL
    END,
    updated_at = sqlc.arg(occurred_at)::timestamptz,
    version = migration.version + 1
WHERE migration.id = sqlc.arg(migration_id)::uuid
  AND migration.status = 'copying'
  AND NOT EXISTS (
      SELECT 1
      FROM torrents.storage_migration_items AS item
      WHERE item.migration_id = migration.id
        AND item.state <> 'verified'
  );

-- name: ReleaseStorageCopyTask :execrows
UPDATE torrents.storage_migration_items
SET state = 'copy_failed',
    available_at = sqlc.arg(available_at)::timestamptz,
    lease_until = NULL,
    lease_token = NULL,
    last_error_code = sqlc.arg(last_error_code)::text,
    updated_at = sqlc.arg(released_at)::timestamptz
WHERE migration_id = sqlc.arg(migration_id)::uuid
  AND object_id = sqlc.arg(object_id)::uuid
  AND state = 'copying'
  AND lease_token = sqlc.arg(lease_token)::uuid;

-- name: LockStorageMigrationForCutover :one
SELECT id, mode, status, source_backend_id, destination_backend_id
FROM torrents.storage_migrations
WHERE id = sqlc.arg(migration_id)::uuid
FOR UPDATE;

-- name: CountUnverifiedStorageMigrationItems :one
SELECT count(*)::bigint
FROM torrents.storage_migration_items AS item
LEFT JOIN torrents.torrent_object_locations AS destination
    ON destination.id = item.destination_location_id
JOIN torrents.torrent_objects AS object
    ON object.id = item.object_id
WHERE item.migration_id = sqlc.arg(migration_id)::uuid
  AND (
      item.state <> 'verified'
      OR destination.state <> 'verified'
      OR destination.observed_byte_length IS DISTINCT FROM object.byte_length
      OR destination.observed_sha256 IS DISTINCT FROM object.content_sha256
  );

-- name: CountStorageMigrationItems :one
SELECT count(*)::bigint
FROM torrents.storage_migration_items
WHERE migration_id = sqlc.arg(migration_id)::uuid;

-- name: RetireStorageMigrationSources :execrows
UPDATE torrents.torrent_object_locations AS source
SET state = 'retiring',
    is_preferred = false,
    retiring_at = sqlc.arg(cutover_at)::timestamptz,
    updated_at = sqlc.arg(cutover_at)::timestamptz,
    version = source.version + 1
FROM torrents.storage_migration_items AS item
WHERE item.migration_id = sqlc.arg(migration_id)::uuid
  AND item.source_location_id = source.id
  AND item.state = 'verified'
  AND source.state = 'verified'
  AND source.is_preferred = true;

-- name: PreferStorageMigrationDestinations :execrows
UPDATE torrents.torrent_object_locations AS destination
SET is_preferred = true,
    updated_at = sqlc.arg(cutover_at)::timestamptz,
    version = destination.version + 1
FROM torrents.storage_migration_items AS item
WHERE item.migration_id = sqlc.arg(migration_id)::uuid
  AND item.destination_location_id = destination.id
  AND item.state = 'verified'
  AND destination.state = 'verified'
  AND destination.is_preferred = false;

-- name: MarkStorageMigrationRetaining :execrows
UPDATE torrents.storage_migrations
SET status = 'retaining',
    cutover_at = sqlc.arg(cutover_at)::timestamptz,
    retention_until = sqlc.arg(retention_until)::timestamptz,
    updated_at = sqlc.arg(cutover_at)::timestamptz,
    version = version + 1
WHERE id = sqlc.arg(migration_id)::uuid
  AND mode = 'move'
  AND status = 'ready_for_cutover';

-- name: ApproveStorageMigrationCleanup :execrows
UPDATE torrents.storage_migrations
SET status = 'cleaning',
    cleanup_approved_by = sqlc.arg(approved_by)::uuid,
    cleanup_approved_at = sqlc.arg(approved_at)::timestamptz,
    updated_at = sqlc.arg(approved_at)::timestamptz,
    version = version + 1
WHERE id = sqlc.arg(migration_id)::uuid
  AND mode = 'move'
  AND status = 'retaining'
  AND retention_until <= sqlc.arg(approved_at)::timestamptz;

-- name: ClaimStorageCleanupTasks :many
WITH candidates AS (
    SELECT
        item.migration_id,
        item.object_id,
        migration.source_backend_id,
        source.object_key AS source_object_key,
        source.version_id AS source_version_id
    FROM torrents.storage_migration_items AS item
    JOIN torrents.storage_migrations AS migration
        ON migration.id = item.migration_id
       AND migration.status = 'cleaning'
       AND migration.id = sqlc.arg(migration_id)::uuid
       AND migration.cleanup_approved_at IS NOT NULL
       AND migration.retention_until <= sqlc.arg(claimed_at)::timestamptz
    JOIN torrents.torrent_object_locations AS source
        ON source.id = item.source_location_id
       AND source.state = 'retiring'
       AND source.is_preferred = false
    WHERE item.state IN ('verified', 'cleanup_failed')
      AND item.available_at <= sqlc.arg(claimed_at)::timestamptz
      AND (item.lease_until IS NULL OR item.lease_until <= sqlc.arg(claimed_at)::timestamptz)
    ORDER BY item.available_at, item.migration_id, item.object_id
    FOR UPDATE OF item SKIP LOCKED
    LIMIT sqlc.arg(batch_size)::integer
), claimed AS (
    UPDATE torrents.storage_migration_items AS item
    SET state = 'deleting_source',
        attempts = item.attempts + 1,
        lease_until = sqlc.arg(lease_until)::timestamptz,
        lease_token = gen_random_uuid(),
        last_error_code = NULL,
        updated_at = sqlc.arg(claimed_at)::timestamptz
    FROM candidates
    WHERE item.migration_id = candidates.migration_id
      AND item.object_id = candidates.object_id
    RETURNING
        item.migration_id,
        item.object_id,
        item.lease_token,
        item.attempts,
        candidates.source_backend_id,
        candidates.source_object_key,
        candidates.source_version_id
)
SELECT * FROM claimed;

-- name: LockStorageCleanupItem :one
SELECT item.source_location_id
FROM torrents.storage_migration_items AS item
WHERE item.migration_id = sqlc.arg(migration_id)::uuid
  AND item.object_id = sqlc.arg(object_id)::uuid
  AND item.lease_token = sqlc.arg(lease_token)::uuid
  AND item.state = 'deleting_source'
FOR UPDATE;

-- name: MarkTorrentObjectLocationDeleted :execrows
UPDATE torrents.torrent_object_locations
SET state = 'deleted',
    deleted_at = sqlc.arg(deleted_at)::timestamptz,
    updated_at = sqlc.arg(deleted_at)::timestamptz,
    version = version + 1
WHERE id = sqlc.arg(location_id)::uuid
  AND state = 'retiring'
  AND is_preferred = false;

-- name: MarkStorageCleanupTaskDeleted :execrows
UPDATE torrents.storage_migration_items
SET state = 'source_deleted',
    lease_until = NULL,
    lease_token = NULL,
    source_deleted_at = sqlc.arg(deleted_at)::timestamptz,
    last_error_code = NULL,
    updated_at = sqlc.arg(deleted_at)::timestamptz
WHERE migration_id = sqlc.arg(migration_id)::uuid
  AND object_id = sqlc.arg(object_id)::uuid
  AND state = 'deleting_source'
  AND lease_token = sqlc.arg(lease_token)::uuid;

-- name: CompleteStorageMigrationAfterCleanup :execrows
UPDATE torrents.storage_migrations AS migration
SET status = 'completed',
    completed_at = sqlc.arg(completed_at)::timestamptz,
    updated_at = sqlc.arg(completed_at)::timestamptz,
    version = migration.version + 1
WHERE migration.id = sqlc.arg(migration_id)::uuid
  AND migration.status = 'cleaning'
  AND NOT EXISTS (
      SELECT 1
      FROM torrents.storage_migration_items AS item
      WHERE item.migration_id = migration.id
        AND item.state <> 'source_deleted'
  );

-- name: ReleaseStorageCleanupTask :execrows
UPDATE torrents.storage_migration_items
SET state = 'cleanup_failed',
    available_at = sqlc.arg(available_at)::timestamptz,
    lease_until = NULL,
    lease_token = NULL,
    last_error_code = sqlc.arg(last_error_code)::text,
    updated_at = sqlc.arg(released_at)::timestamptz
WHERE migration_id = sqlc.arg(migration_id)::uuid
  AND object_id = sqlc.arg(object_id)::uuid
  AND state = 'deleting_source'
  AND lease_token = sqlc.arg(lease_token)::uuid;
