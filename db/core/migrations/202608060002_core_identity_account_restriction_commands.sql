-- +goose Up
-- Account administration version is deliberately narrower than a generic user
-- profile version. It advances only when an account-access restriction changes,
-- giving the staff command surface a stable optimistic-concurrency boundary.
ALTER TABLE identity.users
    ADD COLUMN administration_version bigint NOT NULL DEFAULT 1
        CHECK (administration_version > 0);

ALTER TABLE identity.account_restrictions
    ADD COLUMN revoked_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    ADD COLUMN revocation_reason_code text
        CHECK (revocation_reason_code IS NULL OR revocation_reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    ADD COLUMN revocation_reason text
        CHECK (revocation_reason IS NULL OR char_length(btrim(revocation_reason)) BETWEEN 10 AND 500),
    -- Existing imported rows predate the bounded command. NOT VALID preserves
    -- those historical facts while PostgreSQL still enforces the limit for
    -- every command-created or subsequently updated row.
    ADD CONSTRAINT account_restrictions_duration_bounded
        CHECK (expires_at <= starts_at + interval '7 days') NOT VALID,
    ADD CONSTRAINT account_restrictions_revocation_metadata_complete
        CHECK (
            (revoked_at IS NULL AND revoked_by IS NULL AND revocation_reason_code IS NULL AND revocation_reason IS NULL)
            OR
            (revoked_at IS NOT NULL AND revoked_by IS NOT NULL AND revocation_reason_code IS NOT NULL AND revocation_reason IS NOT NULL)
        ) NOT VALID;

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES
    ('user.account.restrict', '临时限制账户访问', 'high', 'none', 'staff-session', true, true),
    ('user.account.restriction.revoke', '显式撤销账户访问限制', 'high', 'none', 'staff-session', true, true);

INSERT INTO authz.roles (id, name, description, assignable) VALUES (
    'user_access_operator',
    '账户访问处置员',
    '读取脱敏账户，并创建或撤销最长七天的账户访问限制；不包含永久封禁、隐私字段、凭据或账本权限。',
    true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('user_access_operator', 'user.account.read'),
    ('user_access_operator', 'user.account.restrict'),
    ('user_access_operator', 'user.account.restriction.revoke');

-- +goose Down
DELETE FROM authz.grants WHERE role_id = 'user_access_operator';
DELETE FROM authz.role_permissions WHERE role_id = 'user_access_operator';
DELETE FROM authz.roles WHERE id = 'user_access_operator';
DELETE FROM authz.permissions WHERE action IN (
    'user.account.restrict',
    'user.account.restriction.revoke'
);

ALTER TABLE identity.account_restrictions
    DROP CONSTRAINT account_restrictions_revocation_metadata_complete,
    DROP CONSTRAINT account_restrictions_duration_bounded,
    DROP COLUMN revocation_reason,
    DROP COLUMN revocation_reason_code,
    DROP COLUMN revoked_by;

ALTER TABLE identity.users
    DROP COLUMN administration_version;
