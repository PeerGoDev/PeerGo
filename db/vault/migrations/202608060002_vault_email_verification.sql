-- +goose Up
-- One live challenge per credential makes resends replace older links instead
-- of accumulating bearer tokens. Vault stores only the token digest and keyed
-- email lookup; the raw address and token exist only in delivery memory.
CREATE TABLE vault.email_verification_challenges (
    id uuid PRIMARY KEY,
    credential_ref uuid NOT NULL UNIQUE
        REFERENCES vault.credentials (credential_ref) ON DELETE CASCADE,
    token_sha256 bytea NOT NULL UNIQUE CHECK (octet_length(token_sha256) = 32),
    email_lookup_hmac bytea NOT NULL CHECK (octet_length(email_lookup_hmac) = 32),
    delivery_status text NOT NULL
        CHECK (delivery_status IN ('pending', 'sent', 'failed')),
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    next_request_at timestamptz NOT NULL,
    delivered_at timestamptz,
    verified_at timestamptz,
    CHECK (expires_at > issued_at),
    CHECK (next_request_at > issued_at),
    CHECK (delivered_at IS NULL OR delivered_at >= issued_at),
    CHECK (verified_at IS NULL OR verified_at >= issued_at),
    CHECK ((delivery_status = 'sent') = (delivered_at IS NOT NULL))
);

CREATE INDEX email_verification_challenges_expiry_idx
    ON vault.email_verification_challenges (expires_at)
    WHERE verified_at IS NULL;

-- +goose Down
DROP TABLE vault.email_verification_challenges;
