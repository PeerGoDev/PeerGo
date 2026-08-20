-- +goose Up
-- Account restrictions belong to the identity lifecycle because an active
-- restriction is enforced by login and session lookup. Download, community,
-- Tracker and economy restrictions remain with their future owning modules.
CREATE TABLE identity.account_restrictions (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    kind text NOT NULL CHECK (kind IN ('account_access')),
    reason_code text NOT NULL
        CHECK (reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    reason_summary text NOT NULL
        CHECK (char_length(btrim(reason_summary)) BETWEEN 10 AND 240),
    starts_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > starts_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE INDEX account_restrictions_current_user_idx
    ON identity.account_restrictions (user_id, starts_at, expires_at, id)
    WHERE revoked_at IS NULL;

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'user.account.read',
    '读取脱敏账户与当前有效限制',
    'medium',
    'none',
    'staff-session',
    true,
    true
);

INSERT INTO authz.roles (id, name, description, assignable) VALUES (
    'user_reader',
    '用户账户只读员',
    '读取脱敏账户状态与当前账户访问限制；不包含 PII、凭据、账本或限制写入。',
    true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('user_reader', 'user.account.read');

-- +goose Down
DELETE FROM authz.grants WHERE role_id = 'user_reader';
DELETE FROM authz.role_permissions WHERE role_id = 'user_reader';
DELETE FROM authz.roles WHERE id = 'user_reader';
DELETE FROM authz.permissions WHERE action = 'user.account.read';

DROP TABLE identity.account_restrictions;
