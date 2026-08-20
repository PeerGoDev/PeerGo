-- +goose Up

-- Invitation controls remain part of the identity-owned registration policy.
-- Keeping one versioned row means staff cannot accidentally enable invite-only
-- registration while editing a second, contradictory invitation setting.
ALTER TABLE identity.registration_policy
    ADD COLUMN member_invites_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN invite_valid_days smallint NOT NULL DEFAULT 7
        CHECK (invite_valid_days BETWEEN 1 AND 90),
    ADD COLUMN max_invites_per_member smallint NOT NULL DEFAULT 5
        CHECK (max_invites_per_member BETWEEN 0 AND 100),
    ADD COLUMN minimum_invite_account_age_days smallint NOT NULL DEFAULT 30
        CHECK (minimum_invite_account_age_days BETWEEN 0 AND 3650),
    ADD COLUMN minimum_invite_level smallint NOT NULL DEFAULT 1
        CHECK (minimum_invite_level BETWEEN 1 AND 99);

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('invitation.issue.self', '签发自己的单次注册邀请码', 'medium', 'self', 'web-session', true, true),
    ('invitation.read.self', '查看自己的邀请名额与邀请记录', 'low', 'self', 'web-session', true, true),
    ('invitation.revoke.self', '撤销自己尚未被领取的邀请码', 'medium', 'self', 'web-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'invitation.issue.self'),
    ('member', 'invitation.read.self'),
    ('member', 'invitation.revoke.self');

-- Existing development invitations are deliberately not assigned to a user.
-- Member-issued rows bind authorization evidence and never persist the raw
-- bearer token, which is returned only by the successful issue response.
ALTER TABLE identity.registration_invitations
    ADD COLUMN issuer_user_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    ADD COLUMN source_kind text NOT NULL DEFAULT 'development'
        CHECK (source_kind IN ('development', 'member', 'operator')),
    ADD COLUMN issued_authorization_decision_id uuid,
    ADD COLUMN revoked_at timestamptz,
    ADD COLUMN revoked_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    ADD COLUMN revoked_authorization_decision_id uuid,
    ADD CONSTRAINT registration_invitations_source_valid CHECK (
        (source_kind = 'member'
            AND issuer_user_id IS NOT NULL
            AND issued_authorization_decision_id IS NOT NULL)
        OR
        (source_kind IN ('development', 'operator'))
    ),
    ADD CONSTRAINT registration_invitations_revocation_valid CHECK (
        (revoked_at IS NULL
            AND revoked_by IS NULL
            AND revoked_authorization_decision_id IS NULL)
        OR
        (revoked_at IS NOT NULL
            AND revoked_by IS NOT NULL
            AND revoked_authorization_decision_id IS NOT NULL
            AND claimed_by IS NULL
            AND claimed_at IS NULL
            AND consumed_at IS NULL)
    );

CREATE INDEX registration_invitations_issuer_history_idx
    ON identity.registration_invitations (issuer_user_id, created_at DESC, id DESC)
    WHERE source_kind = 'member';

CREATE INDEX registration_invitations_issuer_quota_idx
    ON identity.registration_invitations (issuer_user_id, expires_at, consumed_at)
    WHERE source_kind = 'member' AND revoked_at IS NULL;

-- +goose Down

DROP INDEX identity.registration_invitations_issuer_quota_idx;
DROP INDEX identity.registration_invitations_issuer_history_idx;

ALTER TABLE identity.registration_invitations
    DROP CONSTRAINT registration_invitations_revocation_valid,
    DROP CONSTRAINT registration_invitations_source_valid,
    DROP COLUMN revoked_authorization_decision_id,
    DROP COLUMN revoked_by,
    DROP COLUMN revoked_at,
    DROP COLUMN issued_authorization_decision_id,
    DROP COLUMN source_kind,
    DROP COLUMN issuer_user_id;

DELETE FROM authz.role_permissions
WHERE action IN ('invitation.issue.self', 'invitation.read.self', 'invitation.revoke.self');

DELETE FROM authz.permissions
WHERE action IN ('invitation.issue.self', 'invitation.read.self', 'invitation.revoke.self');

ALTER TABLE identity.registration_policy
    DROP COLUMN minimum_invite_level,
    DROP COLUMN minimum_invite_account_age_days,
    DROP COLUMN max_invites_per_member,
    DROP COLUMN invite_valid_days,
    DROP COLUMN member_invites_enabled;
