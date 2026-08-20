-- +goose Up
-- A provision is intentionally inert until Core has persisted the matching
-- pending user and asks Vault to activate it. This fail-closed gate prevents a
-- cross-database timeout from creating a credential that can already log in.
CREATE TABLE vault.registration_provisions (
    registration_id uuid PRIMARY KEY,
    credential_ref uuid NOT NULL UNIQUE
        REFERENCES vault.credentials (credential_ref)
        ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
    request_hmac bytea NOT NULL CHECK (octet_length(request_hmac) = 32),
    status text NOT NULL DEFAULT 'provisional'
        CHECK (status IN ('provisional', 'active')),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    activated_at timestamptz,
    CHECK (expires_at > created_at),
    CHECK ((status = 'active') = (activated_at IS NOT NULL))
);

CREATE INDEX registration_provisions_expiry_idx
    ON vault.registration_provisions (expires_at)
    WHERE status = 'provisional';

-- +goose Down
DROP TABLE vault.registration_provisions;
