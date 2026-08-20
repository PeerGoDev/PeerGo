-- +goose Up
-- Core keeps only the latest recovery projection. The opaque recovery ID and
-- timestamp let a consumed Vault token be retried idempotently without storing
-- a token digest or credential material in Core.
ALTER TABLE identity.users
    ADD COLUMN password_changed_at timestamptz,
    ADD COLUMN last_password_recovery_id uuid;

UPDATE identity.users
SET password_changed_at = created_at;

ALTER TABLE identity.users
    ALTER COLUMN password_changed_at SET NOT NULL,
    ALTER COLUMN password_changed_at SET DEFAULT now();

CREATE UNIQUE INDEX users_last_password_recovery_id_unique
    ON identity.users (last_password_recovery_id)
    WHERE last_password_recovery_id IS NOT NULL;

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'account.password.recovery.confirm.anonymous',
    '使用一次性凭证恢复账户密码',
    'high',
    'none',
    'anonymous',
    false,
    false
), (
    'account.password.recovery.request.anonymous',
    '请求账户密码恢复邮件',
    'medium',
    'none',
    'anonymous',
    false,
    false
);

-- +goose Down
DELETE FROM authz.permissions
WHERE action IN (
    'account.password.recovery.confirm.anonymous',
    'account.password.recovery.request.anonymous'
);

DROP INDEX identity.users_last_password_recovery_id_unique;

ALTER TABLE identity.users
    DROP COLUMN last_password_recovery_id,
    DROP COLUMN password_changed_at;
