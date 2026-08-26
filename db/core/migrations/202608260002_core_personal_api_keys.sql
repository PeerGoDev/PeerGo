-- +goose Up

-- The first API-key implementation shipped with the MoviePilot adapter name,
-- but the credential is a user-owned PeerGo credential shared by every
-- external adapter. Rename the single-row table in place so every existing
-- digest, prefix, version and timestamp remains valid without copying secrets.
ALTER TABLE identity.moviepilot_credentials RENAME TO personal_api_keys;

ALTER TABLE identity.personal_api_keys
    ADD COLUMN scopes text[] NOT NULL DEFAULT ARRAY[
        'profile:read',
        'torrent:read',
        'torrent:download',
        'attendance:read',
        'attendance:claim'
    ]::text[],
    ADD CONSTRAINT personal_api_keys_scopes_count_check
        CHECK (
            cardinality(scopes) BETWEEN 1 AND 5
            AND array_position(scopes, NULL) IS NULL
        ),
    ADD CONSTRAINT personal_api_keys_scopes_values_check
        CHECK (scopes <@ ARRAY[
            'profile:read',
            'torrent:read',
            'torrent:download',
            'attendance:read',
            'attendance:claim'
        ]::text[]);

COMMENT ON TABLE identity.personal_api_keys IS
    'One hashed, scope-bounded personal API key per user; no per-request history is stored.';
COMMENT ON COLUMN identity.personal_api_keys.last_used_at IS
    'Operational status coalesced to at most one database write every six hours.';

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
        'integration.apikey.manage.self',
        '创建、轮换或撤销自己的个人 API Key',
        'high',
        'self',
        'web-session',
        true,
        true
    ),
    (
        'integration.apikey.read.self',
        '查看自己的个人 API Key 状态',
        'low',
        'self',
        'web-session',
        true,
        true
    );

INSERT INTO authz.role_permissions (role_id, action)
VALUES
    ('member', 'integration.apikey.manage.self'),
    ('member', 'integration.apikey.read.self')
ON CONFLICT DO NOTHING;

-- Preserve any operator-added role mapping while replacing the action names.
INSERT INTO authz.role_permissions (role_id, action)
SELECT
    role_id,
    CASE action
        WHEN 'integration.moviepilot.manage.self' THEN 'integration.apikey.manage.self'
        ELSE 'integration.apikey.read.self'
    END
FROM authz.role_permissions
WHERE action IN (
    'integration.moviepilot.manage.self',
    'integration.moviepilot.read.self'
)
ON CONFLICT DO NOTHING;

DELETE FROM authz.role_permissions
WHERE action IN (
      'integration.moviepilot.manage.self',
      'integration.moviepilot.read.self'
  );

DELETE FROM authz.permissions
WHERE action IN (
    'integration.moviepilot.manage.self',
    'integration.moviepilot.read.self'
);

-- +goose Down

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
    ('member', 'integration.moviepilot.read.self')
ON CONFLICT DO NOTHING;

INSERT INTO authz.role_permissions (role_id, action)
SELECT
    role_id,
    CASE action
        WHEN 'integration.apikey.manage.self' THEN 'integration.moviepilot.manage.self'
        ELSE 'integration.moviepilot.read.self'
    END
FROM authz.role_permissions
WHERE action IN (
    'integration.apikey.manage.self',
    'integration.apikey.read.self'
)
ON CONFLICT DO NOTHING;

DELETE FROM authz.role_permissions
WHERE action IN (
      'integration.apikey.manage.self',
      'integration.apikey.read.self'
  );

DELETE FROM authz.permissions
WHERE action IN (
    'integration.apikey.manage.self',
    'integration.apikey.read.self'
);

ALTER TABLE identity.personal_api_keys
    DROP CONSTRAINT personal_api_keys_scopes_values_check,
    DROP CONSTRAINT personal_api_keys_scopes_count_check,
    DROP COLUMN scopes;

ALTER TABLE identity.personal_api_keys RENAME TO moviepilot_credentials;
