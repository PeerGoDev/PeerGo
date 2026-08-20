-- +goose Up
-- Login throttling is keyed by the same secret HMAC used for identifier lookup.
-- Unknown identifiers receive buckets too, so the public response cannot reveal
-- whether a credential exists. Stale rows are indexed for bounded worker cleanup.
CREATE TABLE vault.login_failure_buckets (
    identifier_lookup_hmac bytea PRIMARY KEY
        CHECK (octet_length(identifier_lookup_hmac) = 32),
    failed_attempts integer NOT NULL CHECK (failed_attempts BETWEEN 1 AND 1000),
    window_started_at timestamptz NOT NULL,
    last_failed_at timestamptz NOT NULL,
    blocked_until timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (last_failed_at >= window_started_at),
    CHECK (blocked_until >= last_failed_at),
    CHECK (updated_at >= last_failed_at)
);

CREATE INDEX login_failure_buckets_cleanup_idx
    ON vault.login_failure_buckets (updated_at);

-- Rate state exists only for a verified credential. Requests for an unknown
-- address still receive the same public timing hint, but never create a token.
CREATE TABLE vault.password_recovery_rate_limits (
    credential_ref uuid PRIMARY KEY
        REFERENCES vault.credentials (credential_ref) ON DELETE CASCADE,
    next_issue_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

-- Recovery challenges are append-only enough to preserve consumed-token
-- idempotency across a Vault/Core timeout. New requests supersede only an
-- unconsumed challenge; raw bearer tokens and email addresses are never stored.
CREATE TABLE vault.password_recovery_challenges (
    id uuid PRIMARY KEY,
    credential_ref uuid NOT NULL
        REFERENCES vault.credentials (credential_ref) ON DELETE CASCADE,
    token_sha256 bytea NOT NULL UNIQUE CHECK (octet_length(token_sha256) = 32),
    email_lookup_hmac bytea NOT NULL CHECK (octet_length(email_lookup_hmac) = 32),
    delivery_status text NOT NULL
        CHECK (delivery_status IN ('pending', 'sent', 'failed')),
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    delivered_at timestamptz,
    superseded_at timestamptz,
    consumed_at timestamptz,
    password_changed_at timestamptz,
    CHECK (expires_at > issued_at),
    CHECK (delivered_at IS NULL OR delivered_at >= issued_at),
    CHECK (superseded_at IS NULL OR superseded_at >= issued_at),
    CHECK (consumed_at IS NULL OR consumed_at >= issued_at),
    CHECK (password_changed_at IS NULL OR password_changed_at >= issued_at),
    CHECK ((delivery_status = 'sent') = (delivered_at IS NOT NULL)),
    CHECK ((consumed_at IS NULL) = (password_changed_at IS NULL)),
    CHECK (NOT (superseded_at IS NOT NULL AND consumed_at IS NOT NULL))
);

CREATE UNIQUE INDEX password_recovery_one_live_challenge_idx
    ON vault.password_recovery_challenges (credential_ref)
    WHERE superseded_at IS NULL AND consumed_at IS NULL;

CREATE INDEX password_recovery_challenges_expiry_idx
    ON vault.password_recovery_challenges (expires_at)
    WHERE superseded_at IS NULL AND consumed_at IS NULL;

-- +goose Down
DROP TABLE vault.password_recovery_challenges;
DROP TABLE vault.password_recovery_rate_limits;
DROP TABLE vault.login_failure_buckets;
