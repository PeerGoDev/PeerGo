-- +goose Up

-- Upload permission means "submit for review", never publish or allow Tracker
-- traffic. It is a self-scoped ordinary Web-session capability.
INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'torrent.submit',
    '提交自己的种子进入审核',
    'medium',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'torrent.submit');

-- The reservation is the recoverable bridge across PostgreSQL and object
-- storage. It claims one info hash before any bytes are written and preserves
-- enough immutable evidence to finish, safely retry, or later compensate.
CREATE TABLE torrents.torrent_uploads (
    id uuid PRIMARY KEY,
    uploader_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    request_fingerprint bytea NOT NULL
        CHECK (octet_length(request_fingerprint) = 32),
    public_id uuid NOT NULL UNIQUE,
    object_id uuid NOT NULL UNIQUE,
    category_id text NOT NULL
        REFERENCES catalog.categories (id) ON DELETE RESTRICT,
    info_hash_v1 bytea NOT NULL
        CHECK (octet_length(info_hash_v1) = 20),
    content_sha256 bytea NOT NULL
        CHECK (octet_length(content_sha256) = 32),
    byte_length bigint NOT NULL CHECK (byte_length > 0),
    backend_id text NOT NULL CHECK (
        backend_id ~ '^[a-z0-9][a-z0-9._-]{0,62}$'
    ),
    object_key text NOT NULL CHECK (
        char_length(object_key) BETWEEN 1 AND 1024
        AND object_key !~ '(^/|\\\\|(^|/)\.\.(/|$)|//)'
    ),
    state text NOT NULL DEFAULT 'reserved' CHECK (
        state IN ('reserved', 'object_verified', 'completed', 'cleaning', 'abandoned')
    ),
    object_created boolean,
    storage_version_id text CHECK (
        storage_version_id IS NULL OR char_length(storage_version_id) <= 1024
    ),
    object_verified_at timestamptz,
    torrent_id bigint UNIQUE
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    completed_at timestamptz,
    cleanup_available_at timestamptz NOT NULL,
    cleanup_lease_until timestamptz,
    cleanup_lease_token uuid,
    cleanup_attempts integer NOT NULL DEFAULT 0 CHECK (cleanup_attempts >= 0),
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    abandoned_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at),
    CHECK ((cleanup_lease_until IS NULL) = (cleanup_lease_token IS NULL)),
    CHECK ((state = 'cleaning') = (cleanup_lease_token IS NOT NULL)),
    CHECK (
        (object_verified_at IS NULL AND object_created IS NULL)
        OR (object_verified_at IS NOT NULL AND object_created IS NOT NULL)
    ),
    CHECK (
        state NOT IN ('object_verified', 'completed')
        OR object_verified_at IS NOT NULL
    ),
    CHECK (
        (state = 'completed' AND torrent_id IS NOT NULL AND completed_at IS NOT NULL)
        OR (state <> 'completed' AND torrent_id IS NULL AND completed_at IS NULL)
    ),
    CHECK ((state = 'abandoned') = (abandoned_at IS NOT NULL)),
    CHECK (object_verified_at IS NULL OR object_verified_at >= created_at),
    CHECK (completed_at IS NULL OR completed_at >= object_verified_at),
    CHECK (abandoned_at IS NULL OR abandoned_at >= created_at)
);

-- A second idempotency key cannot race the first reservation for the same
-- swarm or exact object. Abandonment releases the claim without deleting the
-- immutable forensic row.
CREATE UNIQUE INDEX torrent_upload_active_info_hash_idx
    ON torrents.torrent_uploads (info_hash_v1)
    WHERE state <> 'abandoned';

CREATE UNIQUE INDEX torrent_upload_active_content_idx
    ON torrents.torrent_uploads (content_sha256)
    WHERE state <> 'abandoned';

CREATE INDEX torrent_upload_cleanup_claim_idx
    ON torrents.torrent_uploads (backend_id, cleanup_available_at, created_at, id)
    WHERE state IN ('reserved', 'object_verified', 'cleaning');

-- +goose StatementBegin
CREATE FUNCTION torrents.protect_torrent_upload()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'torrent upload evidence must not be deleted';
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.uploader_id IS DISTINCT FROM NEW.uploader_id
        OR OLD.request_fingerprint IS DISTINCT FROM NEW.request_fingerprint
        OR OLD.public_id IS DISTINCT FROM NEW.public_id
        OR OLD.object_id IS DISTINCT FROM NEW.object_id
        OR OLD.category_id IS DISTINCT FROM NEW.category_id
        OR OLD.info_hash_v1 IS DISTINCT FROM NEW.info_hash_v1
        OR OLD.content_sha256 IS DISTINCT FROM NEW.content_sha256
        OR OLD.byte_length IS DISTINCT FROM NEW.byte_length
        OR OLD.backend_id IS DISTINCT FROM NEW.backend_id
        OR OLD.object_key IS DISTINCT FROM NEW.object_key
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'torrent upload request identity is immutable';
    END IF;

    IF NEW.version <> OLD.version + 1 THEN
        RAISE EXCEPTION 'torrent upload version must increment exactly once';
    END IF;

    IF OLD.state IN ('completed', 'abandoned') THEN
        RAISE EXCEPTION 'terminal torrent upload evidence is immutable';
    END IF;

    IF OLD.object_verified_at IS NOT NULL AND (
        OLD.object_verified_at IS DISTINCT FROM NEW.object_verified_at
        OR OLD.object_created IS DISTINCT FROM NEW.object_created
        OR OLD.storage_version_id IS DISTINCT FROM NEW.storage_version_id
    ) THEN
        RAISE EXCEPTION 'verified torrent upload object observation is immutable';
    END IF;

    IF OLD.state IS DISTINCT FROM NEW.state AND NOT (
        (OLD.state = 'reserved' AND NEW.state IN ('object_verified', 'cleaning'))
        OR (OLD.state = 'object_verified' AND NEW.state IN ('completed', 'cleaning'))
        OR (OLD.state = 'cleaning' AND NEW.state IN ('reserved', 'object_verified', 'abandoned'))
    ) THEN
        RAISE EXCEPTION 'torrent upload transition from % to % is invalid', OLD.state, NEW.state;
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_upload_lifecycle
BEFORE UPDATE OR DELETE ON torrents.torrent_uploads
FOR EACH ROW EXECUTE FUNCTION torrents.protect_torrent_upload();

-- +goose Down
DROP TRIGGER IF EXISTS torrent_upload_lifecycle ON torrents.torrent_uploads;
DROP FUNCTION IF EXISTS torrents.protect_torrent_upload();
DROP TABLE IF EXISTS torrents.torrent_uploads;

DELETE FROM authz.role_permissions
WHERE role_id = 'member' AND action = 'torrent.submit';
DELETE FROM authz.permissions WHERE action = 'torrent.submit';
