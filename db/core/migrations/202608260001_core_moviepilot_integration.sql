-- +goose Up

-- MoviePilot uses one long-lived, user-controlled integration credential.
-- Core persists only the SHA-256 digest of a 256-bit random API key; rotation
-- replaces the single row and revocation deletes it, so this compatibility
-- surface cannot grow an unbounded credential or request-history table.
CREATE TABLE identity.moviepilot_credentials (
    user_id uuid PRIMARY KEY REFERENCES identity.users (id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    token_prefix text NOT NULL CHECK (
        char_length(token_prefix) = 12
        AND token_prefix ~ '^pgk_[A-Za-z0-9_-]{8}$'
    ),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    last_used_at timestamptz,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at),
    CHECK (last_used_at IS NULL OR last_used_at >= created_at)
);

-- Last-use activity is coalesced to one write per six hours. It is operational
-- status for the account owner, not an API request log. No last-use index is
-- created because every status lookup uses the user_id primary key.

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES
    (
        'integration.moviepilot.manage.self',
        '创建、轮换或撤销自己的 MoviePilot API Key',
        'high',
        'self',
        'web-session',
        true,
        true
    ),
    (
        'integration.moviepilot.read.self',
        '查看自己的 MoviePilot API Key 状态',
        'low',
        'self',
        'web-session',
        true,
        true
    );

INSERT INTO authz.role_permissions (role_id, action)
VALUES
    ('member', 'integration.moviepilot.manage.self'),
    ('member', 'integration.moviepilot.read.self');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'member'
  AND action IN (
      'integration.moviepilot.manage.self',
      'integration.moviepilot.read.self'
  );

DELETE FROM authz.permissions
WHERE action IN (
    'integration.moviepilot.manage.self',
    'integration.moviepilot.read.self'
);

DROP TABLE identity.moviepilot_credentials;
