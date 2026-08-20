-- +goose Up

-- Object identity and physical placement are intentionally separate. A single
-- immutable .torrent may be verified on multiple backends while a migration is
-- copied, cut over, retained for rollback, and eventually cleaned explicitly.
CREATE TABLE torrents.torrent_object_locations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    object_id uuid NOT NULL
        REFERENCES torrents.torrent_objects (id) ON DELETE RESTRICT,
    backend_id text NOT NULL CHECK (
        backend_id ~ '^[a-z0-9][a-z0-9._-]{0,62}$'
    ),
    object_key text NOT NULL CHECK (
        char_length(object_key) BETWEEN 1 AND 1024
        AND object_key !~ '(^/|\\\\|(^|/)\.\.(/|$)|//)'
    ),
    state text NOT NULL CHECK (
        state IN ('pending', 'verified', 'failed', 'retiring', 'deleted')
    ),
    is_preferred boolean NOT NULL DEFAULT false,
    version_id text CHECK (version_id IS NULL OR char_length(version_id) <= 1024),
    observed_byte_length bigint CHECK (observed_byte_length > 0),
    observed_sha256 bytea CHECK (
        observed_sha256 IS NULL OR octet_length(observed_sha256) = 32
    ),
    verified_at timestamptz,
    retiring_at timestamptz,
    deleted_at timestamptz,
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at),
    CHECK (NOT is_preferred OR state = 'verified'),
    CHECK (
        state NOT IN ('verified', 'retiring', 'deleted') OR (
            observed_byte_length IS NOT NULL
            AND observed_sha256 IS NOT NULL
            AND verified_at IS NOT NULL
        )
    ),
    CHECK (state NOT IN ('retiring', 'deleted') OR retiring_at IS NOT NULL),
    CHECK ((state = 'deleted') = (deleted_at IS NOT NULL)),
    CHECK (verified_at IS NULL OR verified_at >= created_at),
    CHECK (retiring_at IS NULL OR retiring_at >= verified_at),
    CHECK (deleted_at IS NULL OR deleted_at >= retiring_at)
);

CREATE UNIQUE INDEX torrent_object_one_preferred_location_idx
    ON torrents.torrent_object_locations (object_id)
    WHERE is_preferred;

-- Deleted rows remain as migration evidence. Partial uniqueness permits a
-- later, separately verified copy to return to the same backend/key without
-- mutating or resurrecting that terminal historical row.
CREATE UNIQUE INDEX torrent_object_one_active_backend_location_idx
    ON torrents.torrent_object_locations (object_id, backend_id)
    WHERE state <> 'deleted';

CREATE UNIQUE INDEX torrent_object_active_backend_key_idx
    ON torrents.torrent_object_locations (backend_id, object_key)
    WHERE state <> 'deleted';

CREATE INDEX torrent_object_locations_backend_state_idx
    ON torrents.torrent_object_locations (backend_id, state, object_id);

-- Existing rows predate multi-location storage. Their old storage_key is
-- preserved as a verified legacy-default location before the column is removed
-- from the immutable content record.
INSERT INTO torrents.torrent_object_locations (
    object_id,
    backend_id,
    object_key,
    state,
    is_preferred,
    observed_byte_length,
    observed_sha256,
    verified_at,
    created_at,
    updated_at
)
SELECT
    object.id,
    'legacy-default',
    object.storage_key,
    'verified',
    true,
    object.byte_length,
    object.content_sha256,
    object.created_at,
    object.created_at,
    object.created_at
FROM torrents.torrent_objects AS object;

ALTER TABLE torrents.torrent_objects DROP COLUMN storage_key;

-- A run snapshots source locations once. New uploads can immediately use the
-- destination backend without changing the finite set reconciled by this run.
CREATE TABLE torrents.storage_migrations (
    id uuid PRIMARY KEY,
    mode text NOT NULL CHECK (mode IN ('replicate', 'move')),
    source_backend_id text NOT NULL CHECK (
        source_backend_id ~ '^[a-z0-9][a-z0-9._-]{0,62}$'
    ),
    destination_backend_id text NOT NULL CHECK (
        destination_backend_id ~ '^[a-z0-9][a-z0-9._-]{0,62}$'
    ),
    status text NOT NULL CHECK (
        status IN (
            'copying',
            'ready_for_cutover',
            'retaining',
            'cleaning',
            'completed',
            'cancelled'
        )
    ),
    requested_by uuid REFERENCES identity.users (id) ON DELETE SET NULL,
    cutover_at timestamptz,
    retention_until timestamptz,
    cleanup_approved_by uuid REFERENCES identity.users (id) ON DELETE SET NULL,
    cleanup_approved_at timestamptz,
    completed_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (source_backend_id <> destination_backend_id),
    CHECK (updated_at >= created_at),
    CHECK ((cutover_at IS NULL) = (retention_until IS NULL)),
    CHECK (retention_until IS NULL OR retention_until >= cutover_at),
    CHECK ((cleanup_approved_at IS NULL) = (cleanup_approved_by IS NULL)),
    CHECK (cleanup_approved_at IS NULL OR cleanup_approved_at >= retention_until),
    CHECK (completed_at IS NULL OR completed_at >= created_at),
    CHECK (mode = 'move' OR status NOT IN ('ready_for_cutover', 'retaining', 'cleaning'))
);

CREATE TABLE torrents.storage_migration_items (
    migration_id uuid NOT NULL
        REFERENCES torrents.storage_migrations (id) ON DELETE RESTRICT,
    object_id uuid NOT NULL
        REFERENCES torrents.torrent_objects (id) ON DELETE RESTRICT,
    source_location_id uuid NOT NULL
        REFERENCES torrents.torrent_object_locations (id) ON DELETE RESTRICT,
    destination_location_id uuid
        REFERENCES torrents.torrent_object_locations (id) ON DELETE RESTRICT,
    destination_object_key text NOT NULL CHECK (
        char_length(destination_object_key) BETWEEN 1 AND 1024
        AND destination_object_key !~ '(^/|\\\\|(^|/)\.\.(/|$)|//)'
    ),
    state text NOT NULL DEFAULT 'pending' CHECK (
        state IN (
            'pending',
            'copying',
            'copy_failed',
            'verified',
            'deleting_source',
            'cleanup_failed',
            'source_deleted'
        )
    ),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at timestamptz NOT NULL,
    lease_until timestamptz,
    lease_token uuid,
    copied_at timestamptz,
    verified_at timestamptz,
    source_deleted_at timestamptz,
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (migration_id, object_id),
    UNIQUE (migration_id, source_location_id),
    CHECK (updated_at >= created_at),
    CHECK ((lease_until IS NULL) = (lease_token IS NULL)),
    CHECK (state NOT IN ('copying', 'deleting_source') OR lease_token IS NOT NULL),
    CHECK (state NOT IN ('verified', 'deleting_source', 'cleanup_failed', 'source_deleted') OR verified_at IS NOT NULL),
    CHECK ((state = 'source_deleted') = (source_deleted_at IS NOT NULL))
);

CREATE UNIQUE INDEX storage_one_active_backend_pair_idx
    ON torrents.storage_migrations (source_backend_id, destination_backend_id)
    WHERE status NOT IN ('completed', 'cancelled');

CREATE INDEX storage_migration_copy_claim_idx
    ON torrents.storage_migration_items (available_at, migration_id, object_id)
    WHERE state IN ('pending', 'copy_failed');

CREATE INDEX storage_migration_cleanup_claim_idx
    ON torrents.storage_migration_items (available_at, migration_id, object_id)
    WHERE state IN ('verified', 'cleanup_failed');

-- Location identity is immutable, while preference and lifecycle state are
-- versioned. Verified observations cannot be rewritten to disguise corruption.
-- +goose StatementBegin
CREATE FUNCTION torrents.protect_torrent_object_location()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'torrent object locations must be retired, not deleted';
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.object_id IS DISTINCT FROM NEW.object_id
        OR OLD.backend_id IS DISTINCT FROM NEW.backend_id
        OR OLD.object_key IS DISTINCT FROM NEW.object_key
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'torrent object location identity is immutable';
    END IF;

    IF NEW.version <> OLD.version + 1 THEN
        RAISE EXCEPTION 'torrent object location version must increment exactly once';
    END IF;

    IF OLD.state IN ('verified', 'retiring', 'deleted') AND (
        OLD.observed_byte_length IS DISTINCT FROM NEW.observed_byte_length
        OR OLD.observed_sha256 IS DISTINCT FROM NEW.observed_sha256
        OR OLD.verified_at IS DISTINCT FROM NEW.verified_at
        OR OLD.version_id IS DISTINCT FROM NEW.version_id
    ) THEN
        RAISE EXCEPTION 'verified torrent object observation is immutable';
    END IF;

    IF OLD.state IS DISTINCT FROM NEW.state AND NOT (
        (OLD.state = 'pending' AND NEW.state IN ('verified', 'failed'))
        OR (OLD.state = 'failed' AND NEW.state IN ('pending', 'verified'))
        OR (OLD.state = 'verified' AND NEW.state = 'retiring')
        OR (OLD.state = 'retiring' AND NEW.state IN ('verified', 'deleted'))
    ) THEN
        RAISE EXCEPTION 'torrent object location transition from % to % is invalid', OLD.state, NEW.state;
    END IF;

    IF OLD.state = 'deleted' THEN
        RAISE EXCEPTION 'deleted torrent object location is terminal';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_object_location_lifecycle
BEFORE UPDATE OR DELETE ON torrents.torrent_object_locations
FOR EACH ROW EXECUTE FUNCTION torrents.protect_torrent_object_location();

-- +goose Down
DROP TRIGGER IF EXISTS torrent_object_location_lifecycle
    ON torrents.torrent_object_locations;
DROP FUNCTION IF EXISTS torrents.protect_torrent_object_location();

-- A down migration is intentionally blocked by NOT NULL if an object has no
-- non-deleted location; silently manufacturing an unusable legacy key would be
-- worse than requiring the operator to restore a verified copy first.
DROP TRIGGER IF EXISTS torrent_objects_immutable ON torrents.torrent_objects;
ALTER TABLE torrents.torrent_objects ADD COLUMN storage_key text;
UPDATE torrents.torrent_objects AS object
SET storage_key = chosen.object_key
FROM (
    SELECT DISTINCT ON (location.object_id)
        location.object_id,
        location.object_key
    FROM torrents.torrent_object_locations AS location
    WHERE location.state <> 'deleted'
    ORDER BY location.object_id, location.is_preferred DESC, location.verified_at DESC NULLS LAST
) AS chosen
WHERE chosen.object_id = object.id;
ALTER TABLE torrents.torrent_objects
    ALTER COLUMN storage_key SET NOT NULL,
    ADD CONSTRAINT torrent_objects_storage_key_key UNIQUE (storage_key),
    ADD CONSTRAINT torrent_objects_storage_key_check
        CHECK (char_length(storage_key) BETWEEN 1 AND 1024);
CREATE TRIGGER torrent_objects_immutable
BEFORE UPDATE OR DELETE ON torrents.torrent_objects
FOR EACH ROW EXECUTE FUNCTION torrents.reject_immutable_evidence_mutation();

DROP TABLE IF EXISTS torrents.storage_migration_items;
DROP TABLE IF EXISTS torrents.storage_migrations;
DROP TABLE IF EXISTS torrents.torrent_object_locations;
