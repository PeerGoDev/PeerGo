-- +goose Up

-- A member can still log in while the legacy/manual download gate is active,
-- so this self-service surface uses the ordinary Web-session audience. Long-
-- term ratio assessments and per-torrent H&R obligations keep their own read
-- and appeal permissions; approving this source must not erase either one.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    (
        'user.downloadrestriction.read.self',
        '查看自己的下载限制来源与旧站或人工限制申诉状态',
        'low', 'self', 'web-session', true, true
    ),
    (
        'user.downloadrestriction.appeal.create.self',
        '为自己的旧站或人工下载限制提交一次复核申请',
        'medium', 'self', 'web-session', true, true
    );

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'user.downloadrestriction.read.self'),
    ('member', 'user.downloadrestriction.appeal.create.self');

-- Reuse the immutable account-restriction appeal evidence and staff queue
-- instead of creating a second generic appeal table. This new source binds the
-- exact identity.user_access_states version. Session-authenticated requests do
-- not claim that a purpose-limited Vault credential proof occurred.
ALTER TABLE identity.account_access_appeals
    DROP CONSTRAINT account_access_appeals_source_kind_check,
    DROP CONSTRAINT account_access_appeals_check,
    DROP CONSTRAINT account_access_appeals_check1,
    ALTER COLUMN credential_verified_at DROP NOT NULL;

ALTER TABLE identity.account_access_appeals
    ADD CONSTRAINT account_access_appeals_source_kind_check CHECK (
        source_kind IN (
            'temporary_restriction',
            'disabled_account',
            'manual_download_restriction'
        )
    ),
    ADD CONSTRAINT account_access_appeals_credential_evidence_check CHECK (
        (
            source_kind IN ('temporary_restriction', 'disabled_account')
            AND credential_verified_at = created_at
        )
        OR
        (
            source_kind = 'manual_download_restriction'
            AND credential_verified_at IS NULL
        )
    ),
    ADD CONSTRAINT account_access_appeals_source_shape_check CHECK (
        (
            source_kind = 'temporary_restriction'
            AND source_restriction_id IS NOT NULL
            AND source_expires_at IS NOT NULL
            AND source_expires_at > source_starts_at
        )
        OR
        (
            source_kind IN (
                'disabled_account', 'manual_download_restriction'
            )
            AND source_restriction_id IS NULL
        )
    );

CREATE UNIQUE INDEX account_access_appeals_manual_download_version_unique
    ON identity.account_access_appeals (user_id, source_version)
    WHERE source_kind = 'manual_download_restriction';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION identity.validate_account_access_appeal_insert()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    current_status text;
    current_version bigint;
    current_download_restricted boolean;
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
    ELSIF NEW.source_kind = 'manual_download_restriction' THEN
        SELECT download_restricted, version
        INTO STRICT current_download_restricted, current_version
        FROM identity.user_access_states
        WHERE user_id = NEW.user_id;
        IF NOT current_download_restricted OR current_version <> NEW.source_version THEN
            RAISE EXCEPTION 'manual download restriction appeal source is no longer current';
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

-- +goose Down

DROP INDEX IF EXISTS identity.account_access_appeals_manual_download_version_unique;

-- Down migration removes evidence owned exclusively by this schema version
-- before restoring the older two-source shape.
DROP TRIGGER IF EXISTS account_access_appeal_resolutions_immutable
    ON identity.account_access_appeal_resolutions;
DROP TRIGGER IF EXISTS account_access_appeals_immutable
    ON identity.account_access_appeals;

DELETE FROM identity.account_access_appeal_resolutions
WHERE appeal_id IN (
    SELECT id FROM identity.account_access_appeals
    WHERE source_kind = 'manual_download_restriction'
);
DELETE FROM identity.account_access_appeals
WHERE source_kind = 'manual_download_restriction';

CREATE TRIGGER account_access_appeals_immutable
BEFORE UPDATE OR DELETE ON identity.account_access_appeals
FOR EACH ROW EXECUTE FUNCTION identity.reject_account_access_appeal_mutation();

CREATE TRIGGER account_access_appeal_resolutions_immutable
BEFORE UPDATE OR DELETE ON identity.account_access_appeal_resolutions
FOR EACH ROW EXECUTE FUNCTION identity.reject_account_access_appeal_mutation();

ALTER TABLE identity.account_access_appeals
    DROP CONSTRAINT account_access_appeals_source_kind_check,
    DROP CONSTRAINT account_access_appeals_credential_evidence_check,
    DROP CONSTRAINT account_access_appeals_source_shape_check;

ALTER TABLE identity.account_access_appeals
    ADD CONSTRAINT account_access_appeals_source_kind_check CHECK (
        source_kind IN ('temporary_restriction', 'disabled_account')
    ),
    ADD CONSTRAINT account_access_appeals_check CHECK (
        credential_verified_at = created_at
    ),
    ADD CONSTRAINT account_access_appeals_check1 CHECK (
        (
            source_kind = 'temporary_restriction'
            AND source_restriction_id IS NOT NULL
            AND source_expires_at IS NOT NULL
            AND source_expires_at > source_starts_at
        )
        OR
        (
            source_kind = 'disabled_account'
            AND source_restriction_id IS NULL
        )
    ),
    ALTER COLUMN credential_verified_at SET NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION identity.validate_account_access_appeal_insert()
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

DELETE FROM authz.role_permissions
WHERE role_id = 'member'
  AND action IN (
      'user.downloadrestriction.read.self',
      'user.downloadrestriction.appeal.create.self'
  );
DELETE FROM authz.permissions
WHERE action IN (
    'user.downloadrestriction.read.self',
    'user.downloadrestriction.appeal.create.self'
);
