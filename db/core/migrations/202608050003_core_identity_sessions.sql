-- +goose Up
CREATE SCHEMA IF NOT EXISTS identity;

-- Core keeps only the opaque reference returned by Privacy Vault. Password
-- hashes, email lookup material, TOTP seeds and recovery material never enter
-- this database.
CREATE TABLE identity.users (
    id uuid PRIMARY KEY,
    credential_ref uuid NOT NULL UNIQUE,
    username text NOT NULL CHECK (char_length(username) BETWEEN 1 AND 64),
    display_name text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 80),
    status text NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'disabled', 'pending')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_username_casefold_unique
    ON identity.users (lower(username));

-- Only SHA-256 digests of random browser tokens are persisted. A database
-- disclosure therefore cannot replay a live cookie without recovering its
-- independently generated 256-bit preimage.
CREATE TABLE identity.sessions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE CASCADE,
    audience text NOT NULL CHECK (audience IN ('web')),
    created_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    CHECK (expires_at > created_at),
    CHECK (last_seen_at >= created_at)
);

CREATE INDEX sessions_active_user_idx
    ON identity.sessions (user_id, expires_at DESC)
    WHERE revoked_at IS NULL;

CREATE INDEX sessions_expiry_idx
    ON identity.sessions (expires_at)
    WHERE revoked_at IS NULL;

-- +goose Down
DROP SCHEMA IF EXISTS identity CASCADE;
