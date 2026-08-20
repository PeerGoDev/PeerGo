-- +goose Up
CREATE SCHEMA IF NOT EXISTS vault;

-- Privacy Vault owns P0 credential material. Core receives only credential_ref
-- after a successful verification and cannot query this table directly.
CREATE TABLE vault.credentials (
    credential_ref uuid PRIMARY KEY,
    password_hash text NOT NULL CHECK (char_length(password_hash) BETWEEN 40 AND 512),
    password_updated_at timestamptz NOT NULL DEFAULT now(),
    disabled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Login requires equality lookup but never the original identifier. The HMAC
-- is keyed outside PostgreSQL, so a database-only leak cannot cheaply enumerate
-- common usernames or email addresses.
CREATE TABLE vault.direct_identifiers (
    credential_ref uuid NOT NULL
        REFERENCES vault.credentials (credential_ref) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('username', 'email')),
    lookup_hmac bytea NOT NULL UNIQUE CHECK (octet_length(lookup_hmac) = 32),
    masked_value text NOT NULL CHECK (char_length(masked_value) BETWEEN 1 AND 254),
    verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (credential_ref, kind)
);

-- +goose Down
DROP SCHEMA IF EXISTS vault CASCADE;
