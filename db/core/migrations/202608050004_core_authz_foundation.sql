-- +goose Up
CREATE SCHEMA IF NOT EXISTS governance;
CREATE SCHEMA IF NOT EXISTS authz;

-- Permissions are a reviewed catalog, not arbitrary strings accepted from an
-- admin form. The Core process verifies these rows against its typed Go
-- catalog at startup and refuses to serve when either side has drifted.
CREATE TABLE authz.permissions (
    action text PRIMARY KEY
        CHECK (action ~ '^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)+$'),
    description text NOT NULL CHECK (char_length(description) BETWEEN 1 AND 200),
    risk_level text NOT NULL CHECK (risk_level IN ('low', 'medium', 'high', 'critical')),
    relationship text NOT NULL CHECK (relationship IN ('none', 'self')),
    credential_audience text NOT NULL
        CHECK (credential_audience IN ('anonymous', 'web-session', 'staff-session', 'service')),
    grantable boolean NOT NULL,
    discoverable boolean NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE authz.roles (
    id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9_]{1,63}$'),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    description text NOT NULL CHECK (char_length(description) BETWEEN 1 AND 240),
    assignable boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE authz.role_permissions (
    role_id text NOT NULL REFERENCES authz.roles (id) ON DELETE CASCADE,
    action text NOT NULL REFERENCES authz.permissions (action) ON DELETE RESTRICT,
    PRIMARY KEY (role_id, action)
);

-- A mandate records where a role's authority came from and when that authority
-- ends. Even bootstrap grants use a finite mandate so no human-facing role can
-- silently become permanent.
CREATE TABLE governance.mandates (
    id uuid PRIMARY KEY,
    subject_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    source_type text NOT NULL
        CHECK (source_type IN ('bootstrap', 'appointment', 'election', 'emergency')),
    source_reference text NOT NULL CHECK (char_length(source_reference) BETWEEN 1 AND 160),
    scope_type text NOT NULL CHECK (scope_type IN ('site', 'category')),
    scope_id text NOT NULL
        CHECK (char_length(scope_id) BETWEEN 1 AND 128 AND position('*' IN scope_id) = 0),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('active', 'suspended', 'expired', 'revoked')),
    approved_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, subject_id),
    CHECK (ends_at > starts_at),
    CHECK (approved_by IS NULL OR approved_by <> subject_id)
);

CREATE TABLE authz.grants (
    id uuid PRIMARY KEY,
    subject_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    role_id text NOT NULL REFERENCES authz.roles (id) ON DELETE RESTRICT,
    mandate_id uuid NOT NULL,
    scope_type text NOT NULL CHECK (scope_type IN ('site', 'category')),
    scope_id text NOT NULL
        CHECK (char_length(scope_id) BETWEEN 1 AND 128 AND position('*' IN scope_id) = 0),
    valid_from timestamptz NOT NULL,
    valid_until timestamptz NOT NULL,
    constraints jsonb NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(constraints) = 'object'),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (mandate_id, subject_id)
        REFERENCES governance.mandates (id, subject_id) ON DELETE RESTRICT,
    UNIQUE (subject_id, role_id, mandate_id, scope_type, scope_id),
    CHECK (valid_until > valid_from)
);

CREATE INDEX grants_subject_active_idx
    ON authz.grants (subject_id, valid_until DESC)
    WHERE revoked_at IS NULL;

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES
    ('announcement.read', '读取公开公告', 'low', 'none', 'anonymous', false, false),
    ('authz.capability.read.self', '查看自己的当前有效权限', 'low', 'self', 'web-session', true, true),
    ('category.read', '读取公开种子分类', 'low', 'none', 'anonymous', false, false),
    ('session.create.self', '创建自己的 Web 会话', 'medium', 'none', 'anonymous', false, false),
    ('session.read.self', '读取自己的当前 Web 会话', 'low', 'self', 'web-session', true, true),
    ('session.revoke.self', '撤销自己的当前 Web 会话', 'medium', 'self', 'web-session', true, true),
    ('site.read', '读取公开站点信息', 'low', 'none', 'anonymous', false, false),
    ('torrent.read', '读取公开种子摘要', 'low', 'none', 'anonymous', false, false);

INSERT INTO authz.roles (id, name, description, assignable) VALUES (
    'member',
    '普通成员',
    '仅包含当前已实现的本人会话与权限发现能力。',
    true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'authz.capability.read.self'),
    ('member', 'session.read.self'),
    ('member', 'session.revoke.self');

-- +goose Down
DROP SCHEMA IF EXISTS authz CASCADE;
DROP SCHEMA IF EXISTS governance CASCADE;
