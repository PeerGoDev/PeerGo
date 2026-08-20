-- +goose Up

CREATE TABLE identity.user_avatar_objects (
    id uuid PRIMARY KEY,
    content_sha256 bytea NOT NULL UNIQUE CHECK (octet_length(content_sha256) = 32),
    byte_length bigint NOT NULL CHECK (byte_length BETWEEN 1 AND 1048576),
    content_type text NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png', 'image/webp')),
    extension text NOT NULL CHECK (extension IN ('.jpg', '.png', '.webp')),
    width integer NOT NULL CHECK (width BETWEEN 1 AND 1024),
    height integer NOT NULL CHECK (height BETWEEN 1 AND 1024),
    created_at timestamptz NOT NULL
);

CREATE TABLE identity.user_avatar_object_locations (
    object_id uuid NOT NULL REFERENCES identity.user_avatar_objects (id) ON DELETE CASCADE,
    backend_id text NOT NULL,
    object_key text NOT NULL,
    version_id text,
    observed_byte_length bigint NOT NULL CHECK (observed_byte_length BETWEEN 1 AND 1048576),
    observed_sha256 bytea NOT NULL CHECK (octet_length(observed_sha256) = 32),
    verified_at timestamptz NOT NULL,
    PRIMARY KEY (object_id, backend_id),
    UNIQUE (backend_id, object_key)
);

CREATE TABLE identity.user_avatars (
    user_id uuid PRIMARY KEY REFERENCES identity.users (id) ON DELETE CASCADE,
    object_id uuid NOT NULL REFERENCES identity.user_avatar_objects (id),
    updated_at timestamptz NOT NULL
);

CREATE INDEX user_avatar_locations_object_idx
    ON identity.user_avatar_object_locations (object_id, verified_at DESC);

-- +goose Down

DROP TABLE IF EXISTS identity.user_avatars;
DROP TABLE IF EXISTS identity.user_avatar_object_locations;
DROP TABLE IF EXISTS identity.user_avatar_objects;
