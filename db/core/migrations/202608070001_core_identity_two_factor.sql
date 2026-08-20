-- +goose Up
-- Two-factor management is a distinct self-service capability. It must not be
-- smuggled through the broader session revoke permission even though enabling
-- or disabling a factor also invalidates other sessions.
INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'account.totp.manage.self',
    '管理自己的 TOTP 与恢复码',
    'high',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'account.totp.manage.self');

-- Vault owns the actual factor. Core persists only an opaque change ID and
-- redacted outcome so a Vault-first cross-database command can finish its
-- audit/session transaction idempotently after a network timeout.
CREATE TABLE identity.two_factor_changes (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    kind text NOT NULL
        CHECK (kind IN ('enabled', 'recovery_codes_rotated', 'disabled')),
    occurred_at timestamptz NOT NULL,
    revoked_web_sessions bigint NOT NULL DEFAULT 0 CHECK (revoked_web_sessions >= 0),
    revoked_staff_sessions bigint NOT NULL DEFAULT 0 CHECK (revoked_staff_sessions >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, user_id, kind)
);

CREATE INDEX two_factor_changes_user_time_idx
    ON identity.two_factor_changes (user_id, occurred_at DESC);

-- +goose Down
DROP TABLE identity.two_factor_changes;
DELETE FROM authz.role_permissions
WHERE role_id = 'member' AND action = 'account.totp.manage.self';
DELETE FROM authz.permissions
WHERE action = 'account.totp.manage.self';
