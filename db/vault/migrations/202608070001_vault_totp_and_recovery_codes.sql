-- +goose Up
-- A pending enrollment is separate from the active factor so restarting an
-- enrollment can never overwrite a working second factor. Secret material is
-- encrypted by Privacy Vault before it reaches PostgreSQL; the key and raw
-- Base32 secret are deliberately absent from this schema.
CREATE TABLE vault.totp_enrollments (
    id uuid PRIMARY KEY,
    credential_ref uuid NOT NULL
        REFERENCES vault.credentials (credential_ref) ON DELETE CASCADE,
    secret_ciphertext bytea NOT NULL CHECK (octet_length(secret_ciphertext) BETWEEN 32 AND 512),
    secret_nonce bytea NOT NULL CHECK (octet_length(secret_nonce) = 12),
    key_epoch text NOT NULL CHECK (char_length(key_epoch) BETWEEN 1 AND 80),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    confirmed_at timestamptz,
    superseded_at timestamptz,
    recovery_bundle_ciphertext bytea,
    recovery_bundle_nonce bytea,
    recovery_bundle_expires_at timestamptz,
    CHECK (expires_at > created_at),
    CHECK (confirmed_at IS NULL OR confirmed_at >= created_at),
    CHECK (superseded_at IS NULL OR superseded_at >= created_at),
    CHECK (NOT (confirmed_at IS NOT NULL AND superseded_at IS NOT NULL)),
    CHECK (
        (recovery_bundle_ciphertext IS NULL AND recovery_bundle_nonce IS NULL AND recovery_bundle_expires_at IS NULL)
        OR
        (confirmed_at IS NOT NULL
            AND octet_length(recovery_bundle_ciphertext) BETWEEN 64 AND 4096
            AND octet_length(recovery_bundle_nonce) = 12
            AND recovery_bundle_expires_at > confirmed_at)
    )
);

CREATE UNIQUE INDEX totp_enrollments_one_pending_idx
    ON vault.totp_enrollments (credential_ref)
    WHERE confirmed_at IS NULL AND superseded_at IS NULL;

CREATE INDEX totp_enrollments_expiry_idx
    ON vault.totp_enrollments (expires_at)
    WHERE confirmed_at IS NULL AND superseded_at IS NULL;

-- One row per credential keeps the active factor lookup bounded on the login
-- path. Re-enrollment updates this row only after a new code has been proven.
CREATE TABLE vault.totp_factors (
    credential_ref uuid PRIMARY KEY
        REFERENCES vault.credentials (credential_ref) ON DELETE CASCADE,
    enrollment_id uuid NOT NULL UNIQUE
        REFERENCES vault.totp_enrollments (id) ON DELETE RESTRICT,
    secret_ciphertext bytea NOT NULL CHECK (octet_length(secret_ciphertext) BETWEEN 32 AND 512),
    secret_nonce bytea NOT NULL CHECK (octet_length(secret_nonce) = 12),
    key_epoch text NOT NULL CHECK (char_length(key_epoch) BETWEEN 1 AND 80),
    enabled_at timestamptz NOT NULL,
    disabled_at timestamptz,
    last_used_step bigint NOT NULL DEFAULT -1 CHECK (last_used_step >= -1),
    updated_at timestamptz NOT NULL,
    CHECK (disabled_at IS NULL OR disabled_at >= enabled_at),
    CHECK (updated_at >= enabled_at)
);

-- Recovery codes are high-entropy values, but Vault still stores only a keyed
-- digest. A generation UUID makes rotation an atomic set replacement while
-- retaining the fact that older codes were revoked rather than deleting it.
CREATE TABLE vault.totp_recovery_codes (
    credential_ref uuid NOT NULL
        REFERENCES vault.credentials (credential_ref) ON DELETE CASCADE,
    generation_id uuid NOT NULL,
    ordinal smallint NOT NULL CHECK (ordinal BETWEEN 1 AND 32),
    code_hmac bytea NOT NULL UNIQUE CHECK (octet_length(code_hmac) = 32),
    created_at timestamptz NOT NULL,
    used_at timestamptz,
    revoked_at timestamptz,
    PRIMARY KEY (credential_ref, generation_id, ordinal),
    CHECK (used_at IS NULL OR used_at >= created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CHECK (NOT (used_at IS NOT NULL AND revoked_at IS NOT NULL))
);

CREATE INDEX totp_recovery_codes_active_idx
    ON vault.totp_recovery_codes (credential_ref, code_hmac)
    WHERE used_at IS NULL AND revoked_at IS NULL;

-- Rotation and disable use a browser-generated idempotency key. Vault keeps a
-- bounded result record so Core can safely finish its own transaction after a
-- process crash or network timeout without accepting replayed TOTP evidence.
-- Only rotation has a display bundle, encrypted exactly like enrollment codes
-- and available for the same short retry window.
CREATE TABLE vault.totp_changes (
    id uuid PRIMARY KEY,
    credential_ref uuid NOT NULL
        REFERENCES vault.credentials (credential_ref) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('recovery_codes_rotated', 'disabled')),
    changed_at timestamptz NOT NULL,
    recovery_bundle_ciphertext bytea,
    recovery_bundle_nonce bytea,
    recovery_bundle_key_epoch text,
    recovery_bundle_expires_at timestamptz,
    CHECK (
        (kind = 'disabled'
            AND recovery_bundle_ciphertext IS NULL
            AND recovery_bundle_nonce IS NULL
            AND recovery_bundle_key_epoch IS NULL
            AND recovery_bundle_expires_at IS NULL)
        OR
        (kind = 'recovery_codes_rotated'
            AND octet_length(recovery_bundle_ciphertext) BETWEEN 64 AND 4096
            AND octet_length(recovery_bundle_nonce) = 12
            AND char_length(recovery_bundle_key_epoch) BETWEEN 1 AND 80
            AND recovery_bundle_expires_at > changed_at)
    )
);

CREATE INDEX totp_changes_credential_time_idx
    ON vault.totp_changes (credential_ref, changed_at DESC);

-- +goose Down
DROP TABLE vault.totp_changes;
DROP TABLE vault.totp_recovery_codes;
DROP TABLE vault.totp_factors;
DROP TABLE vault.totp_enrollments;
