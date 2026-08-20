-- +goose Up

CREATE SCHEMA IF NOT EXISTS storage;

-- Image locations originally recorded only a successful write. Bring them to
-- the same read-preference and retirement contract as .torrent locations so a
-- single migration can switch every stored object atomically. The immutable
-- object tables remain owned by their existing domains.
ALTER TABLE torrents.torrent_screenshot_object_locations
    ADD COLUMN state text NOT NULL DEFAULT 'verified',
    ADD COLUMN is_preferred boolean NOT NULL DEFAULT false,
    ADD COLUMN retiring_at timestamptz,
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN last_error_code text,
    ADD COLUMN version bigint NOT NULL DEFAULT 1,
    ADD COLUMN updated_at timestamptz DEFAULT now();

UPDATE torrents.torrent_screenshot_object_locations
SET updated_at = verified_at;

WITH preferred AS (
    SELECT DISTINCT ON (object_id) id
    FROM torrents.torrent_screenshot_object_locations
    ORDER BY object_id, verified_at DESC, id
)
UPDATE torrents.torrent_screenshot_object_locations AS location
SET is_preferred = true
FROM preferred
WHERE location.id = preferred.id;

ALTER TABLE torrents.torrent_screenshot_object_locations
    ALTER COLUMN updated_at SET NOT NULL,
    ADD CONSTRAINT torrent_screenshot_location_state_check
        CHECK (state IN ('verified', 'retiring', 'deleted')),
    ADD CONSTRAINT torrent_screenshot_location_preferred_check
        CHECK (NOT is_preferred OR state = 'verified'),
    ADD CONSTRAINT torrent_screenshot_location_retiring_check
        CHECK (state NOT IN ('retiring', 'deleted') OR retiring_at IS NOT NULL),
    ADD CONSTRAINT torrent_screenshot_location_deleted_check
        CHECK ((state = 'deleted') = (deleted_at IS NOT NULL)),
    ADD CONSTRAINT torrent_screenshot_location_error_check
        CHECK (last_error_code IS NULL OR last_error_code ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    ADD CONSTRAINT torrent_screenshot_location_version_check CHECK (version > 0),
    ADD CONSTRAINT torrent_screenshot_location_updated_check CHECK (updated_at >= created_at);

CREATE UNIQUE INDEX torrent_screenshot_one_preferred_location_idx
    ON torrents.torrent_screenshot_object_locations (object_id)
    WHERE is_preferred;

ALTER TABLE identity.user_avatar_object_locations
    ADD COLUMN id uuid DEFAULT gen_random_uuid(),
    ADD COLUMN state text NOT NULL DEFAULT 'verified',
    ADD COLUMN is_preferred boolean NOT NULL DEFAULT false,
    ADD COLUMN retiring_at timestamptz,
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN last_error_code text,
    ADD COLUMN version bigint NOT NULL DEFAULT 1,
    ADD COLUMN created_at timestamptz DEFAULT now(),
    ADD COLUMN updated_at timestamptz DEFAULT now();

UPDATE identity.user_avatar_object_locations
SET created_at = verified_at,
    updated_at = verified_at;

ALTER TABLE identity.user_avatar_object_locations
    ALTER COLUMN id SET NOT NULL,
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET NOT NULL,
    DROP CONSTRAINT user_avatar_object_locations_pkey,
    ADD CONSTRAINT user_avatar_object_locations_pkey PRIMARY KEY (id),
    ADD CONSTRAINT user_avatar_location_state_check
        CHECK (state IN ('verified', 'retiring', 'deleted')),
    ADD CONSTRAINT user_avatar_location_preferred_check
        CHECK (NOT is_preferred OR state = 'verified'),
    ADD CONSTRAINT user_avatar_location_retiring_check
        CHECK (state NOT IN ('retiring', 'deleted') OR retiring_at IS NOT NULL),
    ADD CONSTRAINT user_avatar_location_deleted_check
        CHECK ((state = 'deleted') = (deleted_at IS NOT NULL)),
    ADD CONSTRAINT user_avatar_location_error_check
        CHECK (last_error_code IS NULL OR last_error_code ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    ADD CONSTRAINT user_avatar_location_version_check CHECK (version > 0),
    ADD CONSTRAINT user_avatar_location_updated_check CHECK (updated_at >= created_at);

WITH preferred AS (
    SELECT DISTINCT ON (object_id) id
    FROM identity.user_avatar_object_locations
    ORDER BY object_id, verified_at DESC, id
)
UPDATE identity.user_avatar_object_locations AS location
SET is_preferred = true
FROM preferred
WHERE location.id = preferred.id;

CREATE UNIQUE INDEX user_avatar_one_preferred_location_idx
    ON identity.user_avatar_object_locations (object_id)
    WHERE is_preferred;

ALTER TABLE media.image_derivative_object_locations
    ADD COLUMN id uuid DEFAULT gen_random_uuid(),
    ADD COLUMN state text NOT NULL DEFAULT 'verified',
    ADD COLUMN is_preferred boolean NOT NULL DEFAULT false,
    ADD COLUMN retiring_at timestamptz,
    ADD COLUMN deleted_at timestamptz,
    ADD COLUMN last_error_code text,
    ADD COLUMN version bigint NOT NULL DEFAULT 1,
    ADD COLUMN created_at timestamptz DEFAULT now(),
    ADD COLUMN updated_at timestamptz DEFAULT now();

UPDATE media.image_derivative_object_locations
SET created_at = verified_at,
    updated_at = verified_at;

ALTER TABLE media.image_derivative_object_locations
    ALTER COLUMN id SET NOT NULL,
    ALTER COLUMN created_at SET NOT NULL,
    ALTER COLUMN updated_at SET NOT NULL,
    DROP CONSTRAINT image_derivative_object_locations_pkey,
    ADD CONSTRAINT image_derivative_object_locations_pkey PRIMARY KEY (id),
    ADD CONSTRAINT image_derivative_location_state_check
        CHECK (state IN ('verified', 'retiring', 'deleted')),
    ADD CONSTRAINT image_derivative_location_preferred_check
        CHECK (NOT is_preferred OR state = 'verified'),
    ADD CONSTRAINT image_derivative_location_retiring_check
        CHECK (state NOT IN ('retiring', 'deleted') OR retiring_at IS NOT NULL),
    ADD CONSTRAINT image_derivative_location_deleted_check
        CHECK ((state = 'deleted') = (deleted_at IS NOT NULL)),
    ADD CONSTRAINT image_derivative_location_error_check
        CHECK (last_error_code IS NULL OR last_error_code ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    ADD CONSTRAINT image_derivative_location_version_check CHECK (version > 0),
    ADD CONSTRAINT image_derivative_location_updated_check CHECK (updated_at >= created_at);

WITH preferred AS (
    SELECT DISTINCT ON (object_id) id
    FROM media.image_derivative_object_locations
    ORDER BY object_id, verified_at DESC, id
)
UPDATE media.image_derivative_object_locations AS location
SET is_preferred = true
FROM preferred
WHERE location.id = preferred.id;

CREATE UNIQUE INDEX image_derivative_one_preferred_location_idx
    ON media.image_derivative_object_locations (object_id)
    WHERE is_preferred;

-- PostgreSQL cannot parameterize a trigger relation. Keep the three tiny
-- typed trigger functions explicit rather than storing an unenforced generic
-- table name or object UUID.
-- +goose StatementBegin
CREATE FUNCTION storage.prefer_first_screenshot_location()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.state = 'verified' AND NOT EXISTS (
        SELECT 1 FROM torrents.torrent_screenshot_object_locations
        WHERE object_id = NEW.object_id AND is_preferred
    ) THEN
        NEW.is_preferred := true;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION storage.prefer_first_avatar_location()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.state = 'verified' AND NOT EXISTS (
        SELECT 1 FROM identity.user_avatar_object_locations
        WHERE object_id = NEW.object_id AND is_preferred
    ) THEN
        NEW.is_preferred := true;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION storage.prefer_first_derivative_location()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.state = 'verified' AND NOT EXISTS (
        SELECT 1 FROM media.image_derivative_object_locations
        WHERE object_id = NEW.object_id AND is_preferred
    ) THEN
        NEW.is_preferred := true;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_screenshot_location_first_preferred
BEFORE INSERT ON torrents.torrent_screenshot_object_locations
FOR EACH ROW EXECUTE FUNCTION storage.prefer_first_screenshot_location();

CREATE TRIGGER user_avatar_location_first_preferred
BEFORE INSERT ON identity.user_avatar_object_locations
FOR EACH ROW EXECUTE FUNCTION storage.prefer_first_avatar_location();

CREATE TRIGGER image_derivative_location_first_preferred
BEFORE INSERT ON media.image_derivative_object_locations
FOR EACH ROW EXECUTE FUNCTION storage.prefer_first_derivative_location();

CREATE TABLE storage.migrations (
    id uuid PRIMARY KEY,
    mode text NOT NULL CHECK (mode IN ('replicate', 'move')),
    object_kinds text[] NOT NULL CHECK (
        cardinality(object_kinds) BETWEEN 1 AND 4
        AND object_kinds <@ ARRAY['torrent', 'torrent_screenshot', 'avatar', 'image_derivative']::text[]
    ),
    source_backend_id text NOT NULL CHECK (source_backend_id ~ '^[a-z0-9][a-z0-9._-]{0,62}$'),
    destination_backend_id text NOT NULL CHECK (destination_backend_id ~ '^[a-z0-9][a-z0-9._-]{0,62}$'),
    status text NOT NULL CHECK (status IN ('copying', 'ready_for_cutover', 'retaining', 'cleaning', 'completed', 'cancelled')),
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
    CHECK (mode = 'move' OR status NOT IN ('ready_for_cutover', 'retaining', 'cleaning'))
);

CREATE TABLE storage.migration_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    migration_id uuid NOT NULL REFERENCES storage.migrations (id) ON DELETE RESTRICT,
    object_kind text NOT NULL CHECK (object_kind IN ('torrent', 'torrent_screenshot', 'avatar', 'image_derivative')),
    torrent_object_id uuid REFERENCES torrents.torrent_objects (id) ON DELETE RESTRICT,
    screenshot_object_id uuid REFERENCES torrents.torrent_screenshot_objects (id) ON DELETE RESTRICT,
    avatar_object_id uuid REFERENCES identity.user_avatar_objects (id) ON DELETE RESTRICT,
    derivative_object_id uuid REFERENCES media.image_derivative_objects (id) ON DELETE RESTRICT,
    torrent_source_location_id uuid REFERENCES torrents.torrent_object_locations (id) ON DELETE RESTRICT,
    screenshot_source_location_id uuid REFERENCES torrents.torrent_screenshot_object_locations (id) ON DELETE RESTRICT,
    avatar_source_location_id uuid REFERENCES identity.user_avatar_object_locations (id) ON DELETE RESTRICT,
    derivative_source_location_id uuid REFERENCES media.image_derivative_object_locations (id) ON DELETE RESTRICT,
    torrent_destination_location_id uuid REFERENCES torrents.torrent_object_locations (id) ON DELETE RESTRICT,
    screenshot_destination_location_id uuid REFERENCES torrents.torrent_screenshot_object_locations (id) ON DELETE RESTRICT,
    avatar_destination_location_id uuid REFERENCES identity.user_avatar_object_locations (id) ON DELETE RESTRICT,
    derivative_destination_location_id uuid REFERENCES media.image_derivative_object_locations (id) ON DELETE RESTRICT,
    content_sha256 bytea NOT NULL CHECK (octet_length(content_sha256) = 32),
    byte_length bigint NOT NULL CHECK (byte_length > 0),
    source_object_key text NOT NULL,
    source_version_id text,
    destination_object_key text NOT NULL,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'copying', 'copy_failed', 'verified', 'deleting_source', 'cleanup_failed', 'source_deleted')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at timestamptz NOT NULL,
    lease_until timestamptz,
    lease_token uuid,
    copied_at timestamptz,
    verified_at timestamptz,
    source_deleted_at timestamptz,
    last_error_code text CHECK (last_error_code IS NULL OR last_error_code ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (num_nonnulls(torrent_object_id, screenshot_object_id, avatar_object_id, derivative_object_id) = 1),
    CHECK (num_nonnulls(torrent_source_location_id, screenshot_source_location_id, avatar_source_location_id, derivative_source_location_id) = 1),
    CHECK (num_nonnulls(torrent_destination_location_id, screenshot_destination_location_id, avatar_destination_location_id, derivative_destination_location_id) <= 1),
    CHECK (
        (object_kind = 'torrent' AND torrent_object_id IS NOT NULL AND torrent_source_location_id IS NOT NULL)
        OR (object_kind = 'torrent_screenshot' AND screenshot_object_id IS NOT NULL AND screenshot_source_location_id IS NOT NULL)
        OR (object_kind = 'avatar' AND avatar_object_id IS NOT NULL AND avatar_source_location_id IS NOT NULL)
        OR (object_kind = 'image_derivative' AND derivative_object_id IS NOT NULL AND derivative_source_location_id IS NOT NULL)
    ),
    CHECK (char_length(source_object_key) BETWEEN 1 AND 1024 AND source_object_key !~ '(^/|\\\\|(^|/)\.\.(/|$)|//)'),
    CHECK (char_length(destination_object_key) BETWEEN 1 AND 1024 AND destination_object_key !~ '(^/|\\\\|(^|/)\.\.(/|$)|//)'),
    CHECK ((lease_until IS NULL) = (lease_token IS NULL)),
    CHECK (state NOT IN ('copying', 'deleting_source') OR lease_token IS NOT NULL),
    CHECK (state NOT IN ('verified', 'deleting_source', 'cleanup_failed', 'source_deleted') OR verified_at IS NOT NULL),
    CHECK ((state = 'source_deleted') = (source_deleted_at IS NOT NULL)),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX storage_one_active_backend_pair_idx
    ON storage.migrations (source_backend_id, destination_backend_id)
    WHERE status NOT IN ('completed', 'cancelled');

CREATE UNIQUE INDEX storage_migration_torrent_object_idx
    ON storage.migration_items (migration_id, torrent_object_id)
    WHERE torrent_object_id IS NOT NULL;
CREATE UNIQUE INDEX storage_migration_screenshot_object_idx
    ON storage.migration_items (migration_id, screenshot_object_id)
    WHERE screenshot_object_id IS NOT NULL;
CREATE UNIQUE INDEX storage_migration_avatar_object_idx
    ON storage.migration_items (migration_id, avatar_object_id)
    WHERE avatar_object_id IS NOT NULL;
CREATE UNIQUE INDEX storage_migration_derivative_object_idx
    ON storage.migration_items (migration_id, derivative_object_id)
    WHERE derivative_object_id IS NOT NULL;

CREATE INDEX storage_migration_copy_claim_idx
    ON storage.migration_items (migration_id, available_at, id)
    WHERE state IN ('pending', 'copy_failed');
CREATE INDEX storage_migration_cleanup_claim_idx
    ON storage.migration_items (migration_id, available_at, id)
    WHERE state IN ('verified', 'cleanup_failed');

-- +goose Down

DROP TABLE storage.migration_items;
DROP TABLE storage.migrations;

DROP TRIGGER image_derivative_location_first_preferred ON media.image_derivative_object_locations;
DROP TRIGGER user_avatar_location_first_preferred ON identity.user_avatar_object_locations;
DROP TRIGGER torrent_screenshot_location_first_preferred ON torrents.torrent_screenshot_object_locations;
DROP FUNCTION storage.prefer_first_derivative_location();
DROP FUNCTION storage.prefer_first_avatar_location();
DROP FUNCTION storage.prefer_first_screenshot_location();

DROP INDEX media.image_derivative_one_preferred_location_idx;
ALTER TABLE media.image_derivative_object_locations
    DROP CONSTRAINT image_derivative_location_updated_check,
    DROP CONSTRAINT image_derivative_location_version_check,
    DROP CONSTRAINT image_derivative_location_error_check,
    DROP CONSTRAINT image_derivative_location_deleted_check,
    DROP CONSTRAINT image_derivative_location_retiring_check,
    DROP CONSTRAINT image_derivative_location_preferred_check,
    DROP CONSTRAINT image_derivative_location_state_check,
    DROP CONSTRAINT image_derivative_object_locations_pkey,
    ADD CONSTRAINT image_derivative_object_locations_pkey PRIMARY KEY (object_id, backend_id),
    DROP COLUMN updated_at,
    DROP COLUMN created_at,
    DROP COLUMN version,
    DROP COLUMN last_error_code,
    DROP COLUMN deleted_at,
    DROP COLUMN retiring_at,
    DROP COLUMN is_preferred,
    DROP COLUMN state,
    DROP COLUMN id;

DROP INDEX identity.user_avatar_one_preferred_location_idx;
ALTER TABLE identity.user_avatar_object_locations
    DROP CONSTRAINT user_avatar_location_updated_check,
    DROP CONSTRAINT user_avatar_location_version_check,
    DROP CONSTRAINT user_avatar_location_error_check,
    DROP CONSTRAINT user_avatar_location_deleted_check,
    DROP CONSTRAINT user_avatar_location_retiring_check,
    DROP CONSTRAINT user_avatar_location_preferred_check,
    DROP CONSTRAINT user_avatar_location_state_check,
    DROP CONSTRAINT user_avatar_object_locations_pkey,
    ADD CONSTRAINT user_avatar_object_locations_pkey PRIMARY KEY (object_id, backend_id),
    DROP COLUMN updated_at,
    DROP COLUMN created_at,
    DROP COLUMN version,
    DROP COLUMN last_error_code,
    DROP COLUMN deleted_at,
    DROP COLUMN retiring_at,
    DROP COLUMN is_preferred,
    DROP COLUMN state,
    DROP COLUMN id;

DROP INDEX torrents.torrent_screenshot_one_preferred_location_idx;
ALTER TABLE torrents.torrent_screenshot_object_locations
    DROP CONSTRAINT torrent_screenshot_location_updated_check,
    DROP CONSTRAINT torrent_screenshot_location_version_check,
    DROP CONSTRAINT torrent_screenshot_location_error_check,
    DROP CONSTRAINT torrent_screenshot_location_deleted_check,
    DROP CONSTRAINT torrent_screenshot_location_retiring_check,
    DROP CONSTRAINT torrent_screenshot_location_preferred_check,
    DROP CONSTRAINT torrent_screenshot_location_state_check,
    DROP COLUMN updated_at,
    DROP COLUMN version,
    DROP COLUMN last_error_code,
    DROP COLUMN deleted_at,
    DROP COLUMN retiring_at,
    DROP COLUMN is_preferred,
    DROP COLUMN state;

DROP SCHEMA storage;
