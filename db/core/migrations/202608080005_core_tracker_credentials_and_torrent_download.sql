-- +goose Up

-- Downloading a private metainfo copy is a self-scoped Web capability. It is
-- separate from public catalog reads and from submitting a torrent for review.
INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'torrent.download',
    '下载绑定本人 Tracker 凭据的已发布种子副本',
    'medium',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'torrent.download');

ALTER TABLE identity.users
    ADD CONSTRAINT users_id_credential_ref_unique UNIQUE (id, credential_ref);

-- Core never stores the passkey itself. This recoverable projection binds the
-- Vault credential version to a user and retains only the keyed lookup needed
-- by a later signed Tracker credential snapshot. A Vault success followed by a
-- Core timeout is safe: the next download receives the same version and
-- idempotently repairs this row.
CREATE TABLE identity.tracker_passkey_hmac (
    user_id uuid PRIMARY KEY,
    credential_ref uuid NOT NULL UNIQUE,
    lookup_hmac bytea NOT NULL UNIQUE CHECK (octet_length(lookup_hmac) = 32),
    vault_version bigint NOT NULL CHECK (vault_version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at),
    FOREIGN KEY (user_id, credential_ref)
        REFERENCES identity.users (id, credential_ref) ON DELETE RESTRICT
);

-- +goose StatementBegin
CREATE FUNCTION identity.protect_tracker_passkey_projection()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Tracker passkey projections cannot be deleted';
    END IF;

    IF OLD.user_id IS DISTINCT FROM NEW.user_id
        OR OLD.credential_ref IS DISTINCT FROM NEW.credential_ref
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'Tracker passkey projection identity is immutable';
    END IF;

    IF NEW.vault_version < OLD.vault_version THEN
        RAISE EXCEPTION 'Tracker passkey projection cannot move backwards';
    END IF;

    IF NEW.vault_version = OLD.vault_version
        AND OLD.lookup_hmac IS DISTINCT FROM NEW.lookup_hmac THEN
        RAISE EXCEPTION 'same Tracker passkey version cannot fork';
    END IF;

    IF NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'Tracker passkey projection time cannot move backwards';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER tracker_passkey_projection_protected
BEFORE UPDATE OR DELETE ON identity.tracker_passkey_hmac
FOR EACH ROW EXECUTE FUNCTION identity.protect_tracker_passkey_projection();

-- +goose Down
DROP TRIGGER IF EXISTS tracker_passkey_projection_protected
    ON identity.tracker_passkey_hmac;
DROP FUNCTION IF EXISTS identity.protect_tracker_passkey_projection();
DROP TABLE IF EXISTS identity.tracker_passkey_hmac;
ALTER TABLE identity.users
    DROP CONSTRAINT IF EXISTS users_id_credential_ref_unique;

DELETE FROM authz.role_permissions
WHERE role_id = 'member' AND action = 'torrent.download';
DELETE FROM authz.permissions WHERE action = 'torrent.download';
