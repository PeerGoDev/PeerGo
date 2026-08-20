-- +goose Up

-- Account-access appeals use credential re-verification instead of a Web
-- session because both a legacy ban and a temporary account restriction make
-- ordinary login unavailable. The public command still receives a typed
-- permission name for contract/catalog review, but authority comes from the
-- purpose-limited Vault decision and never creates a browser session.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    (
        'user.account.appeal.create.restricted',
        '使用受限账户凭据查询本人限制并提交一次复核申请',
        'medium', 'self', 'anonymous', false, true
    ),
    (
        'user.account.appeal.read',
        '读取账户访问限制复核队列',
        'medium', 'none', 'staff-session', true, true
    ),
    (
        'user.account.appeal.decide',
        '批准或驳回账户访问限制复核申请',
        'high', 'none', 'staff-session', true, true
    );

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'user.account.appeal.read'),
    ('site_admin', 'user.account.appeal.decide'),
    ('user_access_operator', 'user.account.appeal.read'),
    ('user_access_operator', 'user.account.appeal.decide');

-- Each request snapshots exactly one owning source. A temporary restriction
-- binds its immutable UUID/version; a disabled account binds the user's
-- administration_version. It never uses a loose target_type + target_id pair.
CREATE TABLE identity.account_access_appeals (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    source_kind text NOT NULL CHECK (source_kind IN (
        'temporary_restriction', 'disabled_account'
    )),
    source_restriction_id uuid REFERENCES identity.account_restrictions (id)
        ON DELETE RESTRICT,
    source_version bigint NOT NULL CHECK (source_version > 0),
    source_reason_code text NOT NULL CHECK (
        source_reason_code ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    source_reason_summary text NOT NULL CHECK (
        char_length(btrim(source_reason_summary)) BETWEEN 2 AND 500
        AND source_reason_summary = btrim(source_reason_summary)
    ),
    source_starts_at timestamptz NOT NULL,
    source_expires_at timestamptz,
    statement text NOT NULL CHECK (
        char_length(btrim(statement)) BETWEEN 20 AND 1000
        AND statement = btrim(statement)
    ),
    credential_verified_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    CHECK (credential_verified_at = created_at),
    CHECK (
        (source_kind = 'temporary_restriction'
            AND source_restriction_id IS NOT NULL
            AND source_expires_at IS NOT NULL
            AND source_expires_at > source_starts_at)
        OR
        (source_kind = 'disabled_account'
            AND source_restriction_id IS NULL)
    )
);

CREATE UNIQUE INDEX account_access_appeals_restriction_unique
    ON identity.account_access_appeals (source_restriction_id)
    WHERE source_kind = 'temporary_restriction';

CREATE UNIQUE INDEX account_access_appeals_disabled_version_unique
    ON identity.account_access_appeals (user_id, source_version)
    WHERE source_kind = 'disabled_account';

CREATE INDEX account_access_appeals_recent_idx
    ON identity.account_access_appeals (created_at DESC, id DESC);

CREATE TABLE identity.account_access_appeal_resolutions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    appeal_id uuid NOT NULL UNIQUE
        REFERENCES identity.account_access_appeals (id) ON DELETE RESTRICT,
    outcome text NOT NULL CHECK (outcome IN ('approved', 'rejected')),
    response text NOT NULL CHECK (
        char_length(btrim(response)) BETWEEN 10 AND 1000
        AND response = btrim(response)
    ),
    actor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid NOT NULL,
    source_version bigint NOT NULL CHECK (source_version > 0),
    created_at timestamptz NOT NULL
);

-- +goose StatementBegin
CREATE FUNCTION identity.validate_account_access_appeal_insert()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    current_status text;
    current_version bigint;
    restriction_record identity.account_restrictions%ROWTYPE;
BEGIN
    SELECT status, administration_version
    INTO STRICT current_status, current_version
    FROM identity.users
    WHERE id = NEW.user_id;

    IF NEW.source_kind = 'disabled_account' THEN
        IF current_status <> 'disabled' OR current_version <> NEW.source_version THEN
            RAISE EXCEPTION 'disabled-account appeal source is no longer current';
        END IF;
    ELSE
        SELECT * INTO STRICT restriction_record
        FROM identity.account_restrictions
        WHERE id = NEW.source_restriction_id
          AND user_id = NEW.user_id;
        IF restriction_record.version <> NEW.source_version
           OR restriction_record.revoked_at IS NOT NULL
           OR restriction_record.starts_at > NEW.created_at
           OR restriction_record.expires_at <= NEW.created_at THEN
            RAISE EXCEPTION 'temporary restriction appeal source is no longer current';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER account_access_appeal_insert_valid
BEFORE INSERT ON identity.account_access_appeals
FOR EACH ROW EXECUTE FUNCTION identity.validate_account_access_appeal_insert();

-- +goose StatementBegin
CREATE FUNCTION identity.validate_account_access_appeal_resolution_insert()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_user_id uuid;
    source_created_at timestamptz;
BEGIN
    SELECT user_id, created_at
    INTO STRICT source_user_id, source_created_at
    FROM identity.account_access_appeals
    WHERE id = NEW.appeal_id;

    IF NEW.actor_id = source_user_id OR NEW.created_at < source_created_at THEN
        RAISE EXCEPTION 'invalid account-access appeal resolution actor or time';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER account_access_appeal_resolution_insert_valid
BEFORE INSERT ON identity.account_access_appeal_resolutions
FOR EACH ROW EXECUTE FUNCTION identity.validate_account_access_appeal_resolution_insert();

-- +goose StatementBegin
CREATE FUNCTION identity.reject_account_access_appeal_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'account-access appeal evidence is immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER account_access_appeals_immutable
BEFORE UPDATE OR DELETE ON identity.account_access_appeals
FOR EACH ROW EXECUTE FUNCTION identity.reject_account_access_appeal_mutation();

CREATE TRIGGER account_access_appeal_resolutions_immutable
BEFORE UPDATE OR DELETE ON identity.account_access_appeal_resolutions
FOR EACH ROW EXECUTE FUNCTION identity.reject_account_access_appeal_mutation();

-- +goose Down

DROP TRIGGER IF EXISTS account_access_appeal_resolutions_immutable
    ON identity.account_access_appeal_resolutions;
DROP TRIGGER IF EXISTS account_access_appeals_immutable
    ON identity.account_access_appeals;
DROP FUNCTION IF EXISTS identity.reject_account_access_appeal_mutation();
DROP TRIGGER IF EXISTS account_access_appeal_resolution_insert_valid
    ON identity.account_access_appeal_resolutions;
DROP FUNCTION IF EXISTS identity.validate_account_access_appeal_resolution_insert();
DROP TRIGGER IF EXISTS account_access_appeal_insert_valid
    ON identity.account_access_appeals;
DROP FUNCTION IF EXISTS identity.validate_account_access_appeal_insert();
DROP TABLE IF EXISTS identity.account_access_appeal_resolutions;
DROP TABLE IF EXISTS identity.account_access_appeals;

DELETE FROM authz.role_permissions
WHERE action IN ('user.account.appeal.read', 'user.account.appeal.decide');
DELETE FROM authz.permissions WHERE action IN (
    'user.account.appeal.create.restricted',
    'user.account.appeal.read',
    'user.account.appeal.decide'
);
