-- +goose Up

-- Tracker passkeys are P0 credentials. Privacy Vault keeps the reversible
-- value encrypted; Core receives only a purpose-limited plaintext response
-- while generating one download copy and a keyed lookup projection for the
-- Tracker control plane. The 32-byte HMAC is not a password-equivalent hash:
-- it is keyed outside PostgreSQL so a database-only leak cannot enumerate the
-- 128-bit passkey space or validate guesses.
CREATE TABLE vault.tracker_passkeys (
    credential_ref uuid PRIMARY KEY
        REFERENCES vault.credentials (credential_ref) ON DELETE RESTRICT,
    ciphertext bytea NOT NULL CHECK (octet_length(ciphertext) BETWEEN 17 AND 256),
    nonce bytea NOT NULL CHECK (octet_length(nonce) BETWEEN 12 AND 32),
    encryption_key_epoch text NOT NULL
        CHECK (char_length(encryption_key_epoch) BETWEEN 1 AND 80),
    lookup_hmac bytea NOT NULL UNIQUE CHECK (octet_length(lookup_hmac) = 32),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    rotated_at timestamptz,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at),
    CHECK ((version = 1) = (rotated_at IS NULL)),
    CHECK (rotated_at IS NULL OR (rotated_at >= created_at AND updated_at >= rotated_at))
);

-- The first vertical slice only inserts or reads a stable passkey. This
-- trigger still fixes the future rotation contract now: identity and creation
-- evidence cannot change, every secret replacement advances exactly one
-- version, and a credential cannot be deleted to erase history accidentally.
-- +goose StatementBegin
CREATE FUNCTION vault.protect_tracker_passkey()
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

CREATE TRIGGER tracker_passkeys_protected
BEFORE UPDATE OR DELETE ON vault.tracker_passkeys
FOR EACH ROW EXECUTE FUNCTION vault.protect_tracker_passkey();

-- +goose Down
DROP TRIGGER IF EXISTS tracker_passkeys_protected ON vault.tracker_passkeys;
DROP FUNCTION IF EXISTS vault.protect_tracker_passkey();
DROP TABLE IF EXISTS vault.tracker_passkeys;
