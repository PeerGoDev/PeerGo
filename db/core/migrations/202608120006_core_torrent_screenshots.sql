-- +goose Up

-- Screenshot bytes are immutable objects, not editable URLs. Keeping object
-- identity separate from physical placement lets local filesystem and
-- S3-compatible deployments use the same upload contract without persisting
-- provider URLs or credentials in torrent metadata.
CREATE TABLE torrents.torrent_screenshot_objects (
    id uuid PRIMARY KEY,
    content_sha256 bytea NOT NULL UNIQUE
        CHECK (octet_length(content_sha256) = 32),
    byte_length bigint NOT NULL CHECK (byte_length BETWEEN 1 AND 5242880),
    content_type text NOT NULL CHECK (
        content_type IN ('image/jpeg', 'image/png', 'image/webp')
    ),
    width integer NOT NULL CHECK (width BETWEEN 1 AND 32768),
    height integer NOT NULL CHECK (height BETWEEN 1 AND 32768),
    created_at timestamptz NOT NULL,
    CHECK (width::bigint * height::bigint <= 25000000)
);

CREATE TABLE torrents.torrent_screenshot_object_locations (
    id uuid PRIMARY KEY,
    object_id uuid NOT NULL
        REFERENCES torrents.torrent_screenshot_objects (id) ON DELETE RESTRICT,
    backend_id text NOT NULL CHECK (
        backend_id ~ '^[a-z0-9][a-z0-9._-]{0,62}$'
    ),
    object_key text NOT NULL CHECK (
        char_length(object_key) BETWEEN 1 AND 1024
        AND object_key !~ '(^/|\\\\|(^|/)\.\.(/|$)|//)'
    ),
    version_id text CHECK (version_id IS NULL OR char_length(version_id) <= 1024),
    observed_byte_length bigint NOT NULL CHECK (observed_byte_length > 0),
    observed_sha256 bytea NOT NULL CHECK (octet_length(observed_sha256) = 32),
    verified_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (object_id, backend_id),
    UNIQUE (backend_id, object_key)
);

CREATE TABLE torrents.torrent_screenshots (
    torrent_id bigint NOT NULL
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    object_id uuid NOT NULL
        REFERENCES torrents.torrent_screenshot_objects (id) ON DELETE RESTRICT,
    position smallint NOT NULL CHECK (position BETWEEN 0 AND 5),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (torrent_id, position),
    UNIQUE (torrent_id, object_id)
);

CREATE INDEX torrent_screenshots_object_idx
    ON torrents.torrent_screenshots (object_id, torrent_id);

-- Media evidence is append-only for the first upload slice. Later moderation
-- changes will use explicit replacement events rather than rewriting objects.
CREATE TRIGGER torrent_screenshot_objects_immutable
BEFORE UPDATE OR DELETE ON torrents.torrent_screenshot_objects
FOR EACH ROW EXECUTE FUNCTION torrents.reject_immutable_evidence_mutation();

CREATE TRIGGER torrent_screenshots_immutable
BEFORE UPDATE OR DELETE ON torrents.torrent_screenshots
FOR EACH ROW EXECUTE FUNCTION torrents.reject_immutable_evidence_mutation();

-- +goose Down

DROP TRIGGER torrent_screenshots_immutable ON torrents.torrent_screenshots;
DROP TRIGGER torrent_screenshot_objects_immutable ON torrents.torrent_screenshot_objects;
DROP INDEX torrents.torrent_screenshots_object_idx;
DROP TABLE torrents.torrent_screenshots;
DROP TABLE torrents.torrent_screenshot_object_locations;
DROP TABLE torrents.torrent_screenshot_objects;
