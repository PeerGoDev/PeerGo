-- +goose Up

CREATE SCHEMA IF NOT EXISTS media;

-- Source images remain owned by their existing domains. Derivative objects are
-- shared because the bytes, integrity checks and physical storage lifecycle are
-- identical for torrent screenshots and user avatars.
CREATE TABLE media.image_derivative_objects (
    id uuid PRIMARY KEY,
    content_sha256 bytea NOT NULL UNIQUE CHECK (octet_length(content_sha256) = 32),
    byte_length bigint NOT NULL CHECK (byte_length BETWEEN 1 AND 16777216),
    content_type text NOT NULL CHECK (content_type = 'image/webp'),
    extension text NOT NULL CHECK (extension = '.webp'),
    width integer NOT NULL CHECK (width BETWEEN 1 AND 32768),
    height integer NOT NULL CHECK (height BETWEEN 1 AND 32768),
    created_at timestamptz NOT NULL,
    CHECK (width::bigint * height::bigint <= 100000000)
);

CREATE TABLE media.image_derivative_object_locations (
    object_id uuid NOT NULL
        REFERENCES media.image_derivative_objects (id) ON DELETE RESTRICT,
    backend_id text NOT NULL CHECK (
        backend_id ~ '^[a-z0-9][a-z0-9._-]{0,62}$'
    ),
    object_key text NOT NULL CHECK (
        char_length(object_key) BETWEEN 1 AND 1024
        AND object_key !~ '(^/|\\\\|(^|/)\.\.(/|$)|//)'
    ),
    version_id text CHECK (
        version_id IS NULL OR char_length(version_id) <= 1024
    ),
    observed_byte_length bigint NOT NULL CHECK (
        observed_byte_length BETWEEN 1 AND 16777216
    ),
    observed_sha256 bytea NOT NULL CHECK (octet_length(observed_sha256) = 32),
    verified_at timestamptz NOT NULL,
    PRIMARY KEY (object_id, backend_id),
    UNIQUE (backend_id, object_key)
);

-- One row is both the requested immutable derivative identity and its bounded
-- processing state. Exactly one typed source foreign key is present, avoiding
-- a polymorphic UUID reference that PostgreSQL could not enforce.
CREATE TABLE media.image_derivatives (
    id uuid PRIMARY KEY,
    torrent_screenshot_object_id uuid
        REFERENCES torrents.torrent_screenshot_objects (id) ON DELETE RESTRICT,
    avatar_object_id uuid
        REFERENCES identity.user_avatar_objects (id) ON DELETE RESTRICT,
    variant text NOT NULL CHECK (variant IN ('thumbnail', 'display', 'large')),
    policy_version text NOT NULL CHECK (policy_version = 'webp-v1'),
    state text NOT NULL CHECK (
        state IN ('pending', 'processing', 'retry_wait', 'ready', 'dead')
    ),
    attempt_count smallint NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 10),
    available_at timestamptz NOT NULL,
    lease_token uuid,
    lease_until timestamptz,
    object_id uuid REFERENCES media.image_derivative_objects (id) ON DELETE RESTRICT,
    last_error_code text CHECK (
        last_error_code IS NULL OR char_length(last_error_code) BETWEEN 1 AND 64
    ),
    last_error_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    completed_at timestamptz,
    CHECK (num_nonnulls(torrent_screenshot_object_id, avatar_object_id) = 1),
    CHECK (
        (state = 'processing' AND lease_token IS NOT NULL AND lease_until IS NOT NULL AND object_id IS NULL AND completed_at IS NULL)
        OR
        (state IN ('pending', 'retry_wait') AND lease_token IS NULL AND lease_until IS NULL AND object_id IS NULL AND completed_at IS NULL)
        OR
        (state = 'ready' AND lease_token IS NULL AND lease_until IS NULL AND object_id IS NOT NULL AND completed_at IS NOT NULL)
        OR
        (state = 'dead' AND lease_token IS NULL AND lease_until IS NULL AND object_id IS NULL AND completed_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX image_derivatives_screenshot_identity_idx
    ON media.image_derivatives (
        torrent_screenshot_object_id, variant, policy_version
    )
    WHERE torrent_screenshot_object_id IS NOT NULL;

CREATE UNIQUE INDEX image_derivatives_avatar_identity_idx
    ON media.image_derivatives (avatar_object_id, variant, policy_version)
    WHERE avatar_object_id IS NOT NULL;

CREATE INDEX image_derivatives_claim_idx
    ON media.image_derivatives (available_at, created_at, id)
    WHERE state IN ('pending', 'retry_wait', 'processing');

CREATE INDEX image_derivatives_ready_screenshot_idx
    ON media.image_derivatives (
        torrent_screenshot_object_id, variant, policy_version, object_id
    )
    WHERE state = 'ready' AND torrent_screenshot_object_id IS NOT NULL;

CREATE INDEX image_derivatives_ready_avatar_idx
    ON media.image_derivatives (
        avatar_object_id, variant, policy_version, object_id
    )
    WHERE state = 'ready' AND avatar_object_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION media.enqueue_torrent_screenshot_derivatives()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO media.image_derivatives (
        id, torrent_screenshot_object_id, avatar_object_id,
        variant, policy_version, state, attempt_count, available_at,
        created_at, updated_at
    )
    SELECT
        gen_random_uuid(), NEW.id, NULL,
        requested.variant, 'webp-v1', 'pending', 0, NEW.created_at,
        NEW.created_at, NEW.created_at
    FROM (VALUES ('thumbnail'), ('display'), ('large')) AS requested(variant)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION media.enqueue_avatar_derivatives()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO media.image_derivatives (
        id, torrent_screenshot_object_id, avatar_object_id,
        variant, policy_version, state, attempt_count, available_at,
        created_at, updated_at
    )
    SELECT
        gen_random_uuid(), NULL, NEW.id,
        requested.variant, 'webp-v1', 'pending', 0, NEW.created_at,
        NEW.created_at, NEW.created_at
    FROM (VALUES ('thumbnail'), ('display'), ('large')) AS requested(variant)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_screenshot_derivative_enqueue
AFTER INSERT ON torrents.torrent_screenshot_objects
FOR EACH ROW EXECUTE FUNCTION media.enqueue_torrent_screenshot_derivatives();

CREATE TRIGGER avatar_derivative_enqueue
AFTER INSERT ON identity.user_avatar_objects
FOR EACH ROW EXECUTE FUNCTION media.enqueue_avatar_derivatives();

-- Existing objects, including a previously restored Rousi snapshot, receive
-- the same deterministic policy rows. Reapplying a newer three-archive restore
-- therefore needs no fourth media package or separate backfill command.
INSERT INTO media.image_derivatives (
    id, torrent_screenshot_object_id, avatar_object_id,
    variant, policy_version, state, attempt_count, available_at,
    created_at, updated_at
)
SELECT
    gen_random_uuid(), source.id, NULL,
    requested.variant, 'webp-v1', 'pending', 0, source.created_at,
    source.created_at, source.created_at
FROM torrents.torrent_screenshot_objects AS source
CROSS JOIN (VALUES ('thumbnail'), ('display'), ('large')) AS requested(variant)
ON CONFLICT DO NOTHING;

INSERT INTO media.image_derivatives (
    id, torrent_screenshot_object_id, avatar_object_id,
    variant, policy_version, state, attempt_count, available_at,
    created_at, updated_at
)
SELECT
    gen_random_uuid(), NULL, source.id,
    requested.variant, 'webp-v1', 'pending', 0, source.created_at,
    source.created_at, source.created_at
FROM identity.user_avatar_objects AS source
CROSS JOIN (VALUES ('thumbnail'), ('display'), ('large')) AS requested(variant)
ON CONFLICT DO NOTHING;

-- +goose Down

DROP TRIGGER avatar_derivative_enqueue ON identity.user_avatar_objects;
DROP TRIGGER torrent_screenshot_derivative_enqueue ON torrents.torrent_screenshot_objects;
DROP FUNCTION media.enqueue_avatar_derivatives();
DROP FUNCTION media.enqueue_torrent_screenshot_derivatives();
DROP INDEX media.image_derivatives_ready_avatar_idx;
DROP INDEX media.image_derivatives_ready_screenshot_idx;
DROP INDEX media.image_derivatives_claim_idx;
DROP INDEX media.image_derivatives_avatar_identity_idx;
DROP INDEX media.image_derivatives_screenshot_identity_idx;
DROP TABLE media.image_derivatives;
DROP TABLE media.image_derivative_object_locations;
DROP TABLE media.image_derivative_objects;
DROP SCHEMA media;
