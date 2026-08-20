-- +goose Up

-- Normal registration and password recovery always write Argon2id. The only
-- additional algorithm accepted here is the exact PtYes snapshot profile that
-- was audited before cutover: bcrypt $2a$, cost 10. Keeping the algorithm on
-- the row prevents a malformed prefix from silently selecting a verifier.
ALTER TABLE vault.credentials
    ADD COLUMN password_algorithm text NOT NULL DEFAULT 'argon2id'
        CHECK (password_algorithm IN ('argon2id', 'bcrypt_ptyes_cost10')),
    ADD COLUMN password_rehashed_at timestamptz,
    ADD CONSTRAINT credentials_password_hash_algorithm_valid CHECK (
        (password_algorithm = 'argon2id'
            AND password_hash ~ '^\$argon2id\$v=19\$')
        OR
        (password_algorithm = 'bcrypt_ptyes_cost10'
            AND password_hash ~ '^\$2a\$10\$[./A-Za-z0-9]{53}$')
    ),
    ADD CONSTRAINT credentials_password_rehash_time_valid CHECK (
        password_rehashed_at IS NULL
        OR (password_rehashed_at >= created_at AND password_rehashed_at <= updated_at)
    );

-- Most PtYes passkeys already satisfy PeerGo's canonical lowercase-hex
-- profile. Four audited rows contain a non-hex ASCII alphanumeric value; they
-- remain usable under this isolated profile rather than being rotated during
-- migration. No other historical passkey syntax is admitted.
ALTER TABLE vault.tracker_passkeys
    ADD COLUMN format_profile text NOT NULL DEFAULT 'canonical_hex_v1'
        CHECK (format_profile IN ('canonical_hex_v1', 'ptyes_alnum32_v1'));

-- Rotation always generates a canonical PeerGo passkey. A canonical record
-- cannot be relabelled legacy; a legacy record may leave the compatibility
-- profile only while its encrypted material and lookup HMAC rotate together.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION vault.protect_tracker_passkey()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Tracker passkey records cannot be deleted';
    END IF;

    IF OLD.credential_ref IS DISTINCT FROM NEW.credential_ref
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'Tracker passkey identity is immutable';
    END IF;

    IF NEW.version <> OLD.version + 1
        OR NEW.rotated_at IS NULL
        OR NEW.rotated_at < OLD.updated_at
        OR NEW.updated_at <> NEW.rotated_at THEN
        RAISE EXCEPTION 'Tracker passkey rotation must advance exactly once';
    END IF;

    IF OLD.ciphertext IS NOT DISTINCT FROM NEW.ciphertext
        OR OLD.nonce IS NOT DISTINCT FROM NEW.nonce
        OR OLD.lookup_hmac IS NOT DISTINCT FROM NEW.lookup_hmac THEN
        RAISE EXCEPTION 'Tracker passkey rotation must replace credential material';
    END IF;

    IF (OLD.format_profile = 'canonical_hex_v1'
            AND NEW.format_profile <> 'canonical_hex_v1')
        OR (OLD.format_profile = 'ptyes_alnum32_v1'
            AND NEW.format_profile <> 'canonical_hex_v1') THEN
        RAISE EXCEPTION 'Tracker passkey rotation must end in the canonical profile';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down

-- Restore the pre-compatibility trigger before removing format_profile.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION vault.protect_tracker_passkey()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Tracker passkey records cannot be deleted';
    END IF;

    IF OLD.credential_ref IS DISTINCT FROM NEW.credential_ref
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'Tracker passkey identity is immutable';
    END IF;

    IF NEW.version <> OLD.version + 1
        OR NEW.rotated_at IS NULL
        OR NEW.rotated_at < OLD.updated_at
        OR NEW.updated_at <> NEW.rotated_at THEN
        RAISE EXCEPTION 'Tracker passkey rotation must advance exactly once';
    END IF;

    IF OLD.ciphertext IS NOT DISTINCT FROM NEW.ciphertext
        OR OLD.nonce IS NOT DISTINCT FROM NEW.nonce
        OR OLD.lookup_hmac IS NOT DISTINCT FROM NEW.lookup_hmac THEN
        RAISE EXCEPTION 'Tracker passkey rotation must replace credential material';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER TABLE vault.tracker_passkeys
    DROP COLUMN format_profile;

ALTER TABLE vault.credentials
    DROP CONSTRAINT credentials_password_rehash_time_valid,
    DROP CONSTRAINT credentials_password_hash_algorithm_valid,
    DROP COLUMN password_rehashed_at,
    DROP COLUMN password_algorithm;
