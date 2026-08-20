-- +goose Up

-- Administrators need the full address for account support. The value remains
-- inside Privacy Vault and is resolved only from opaque credential references
-- over the service-authenticated internal API; Core never persists a copy.
CREATE TABLE vault.email_addresses (
    credential_ref uuid PRIMARY KEY
        REFERENCES vault.credentials (credential_ref) ON DELETE CASCADE,
    email_address text NOT NULL UNIQUE CHECK (
        char_length(email_address) BETWEEN 3 AND 254
        AND email_address = lower(btrim(email_address))
        AND position('@' IN email_address) > 1
    ),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at)
);

REVOKE ALL ON vault.email_addresses FROM PUBLIC;

-- +goose Down

DROP TABLE vault.email_addresses;
