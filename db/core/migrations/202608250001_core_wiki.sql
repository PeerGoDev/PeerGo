-- +goose Up

-- Wiki pages are durable editorial content, not an activity stream. The
-- current projection stays compact while a bounded revision window provides
-- recovery without retaining view, search, click, or autosave telemetry.
CREATE TABLE community.wiki_pages (
    id uuid PRIMARY KEY,
    slug text NOT NULL UNIQUE
        CHECK (slug ~ '^[a-z0-9][a-z0-9-]{0,95}$'),
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 160),
    summary text NOT NULL DEFAULT '' CHECK (char_length(summary) <= 500),
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 100000),
    visibility text NOT NULL DEFAULT 'members'
        CHECK (visibility IN ('public', 'members')),
    sort_order integer NOT NULL DEFAULT 0
        CHECK (sort_order BETWEEN -100000 AND 100000),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    revision_number bigint NOT NULL DEFAULT 1 CHECK (revision_number > 0),
    created_by uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    updated_by uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    legacy_source_system text CHECK (
        legacy_source_system IS NULL OR legacy_source_system = 'ptyes'
    ),
    legacy_wiki_id bigint CHECK (legacy_wiki_id IS NULL OR legacy_wiki_id > 0),
    legacy_source_sha256 bytea CHECK (
        legacy_source_sha256 IS NULL OR octet_length(legacy_source_sha256) = 32
    ),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    archived_at timestamptz,
    UNIQUE (legacy_source_system, legacy_wiki_id),
    CHECK (updated_at >= created_at),
    CHECK (
        (legacy_source_system IS NULL AND legacy_wiki_id IS NULL AND legacy_source_sha256 IS NULL)
        OR
        (legacy_source_system IS NOT NULL AND legacy_wiki_id IS NOT NULL AND legacy_source_sha256 IS NOT NULL)
    ),
    CHECK (archived_at IS NULL OR archived_at >= created_at)
);

CREATE INDEX wiki_pages_active_order_idx
    ON community.wiki_pages (sort_order DESC, updated_at DESC, id)
    WHERE archived_at IS NULL;

CREATE TABLE community.wiki_page_editors (
    page_id uuid NOT NULL
        REFERENCES community.wiki_pages (id) ON DELETE CASCADE,
    user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    assigned_by uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    assigned_at timestamptz NOT NULL,
    PRIMARY KEY (page_id, user_id)
);

CREATE INDEX wiki_page_editors_user_idx
    ON community.wiki_page_editors (user_id, page_id);

CREATE TABLE community.wiki_revisions (
    page_id uuid NOT NULL
        REFERENCES community.wiki_pages (id) ON DELETE CASCADE,
    revision_number bigint NOT NULL CHECK (revision_number > 0),
    slug text NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9-]{0,95}$'),
    title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 160),
    summary text NOT NULL CHECK (char_length(summary) <= 500),
    body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 100000),
    visibility text NOT NULL CHECK (visibility IN ('public', 'members')),
    sort_order integer NOT NULL CHECK (sort_order BETWEEN -100000 AND 100000),
    archived boolean NOT NULL DEFAULT false,
    editor_user_ids uuid[] NOT NULL DEFAULT ARRAY[]::uuid[]
        CHECK (cardinality(editor_user_ids) <= 20),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 500),
    origin text NOT NULL CHECK (origin IN ('migration', 'member', 'staff', 'restore')),
    editor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (page_id, revision_number)
);

CREATE INDEX wiki_revisions_page_recent_idx
    ON community.wiki_revisions (page_id, revision_number DESC);

COMMENT ON TABLE community.wiki_pages IS
    'Current Wiki projection; no page-view, click, search, or autosave activity is persisted.';
COMMENT ON TABLE community.wiki_revisions IS
    'Bounded content recovery window; application transactions retain at most 50 revisions per page.';

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('wiki.page.create', '创建并配置 Wiki 页面', 'medium', 'none', 'staff-session', true, true),
    ('wiki.page.manage.read', '读取 Wiki 管理与版本视图', 'medium', 'none', 'staff-session', true, true),
    ('wiki.page.read', '查看公开 Wiki 页面', 'low', 'none', 'anonymous', false, false),
    ('wiki.page.read.member', '查看仅站内成员可见的 Wiki 页面', 'low', 'none', 'web-session', true, true),
    ('wiki.page.restore', '从历史修订恢复 Wiki 页面', 'high', 'none', 'staff-session', true, true),
    ('wiki.page.update', '修改 Wiki 页面配置、正文与协作者', 'high', 'none', 'staff-session', true, true),
    ('wiki.page.update.assigned', '修改自己创建或被指派协作的 Wiki 正文', 'medium', 'none', 'web-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'wiki.page.read.member'),
    ('member', 'wiki.page.update.assigned'),
    ('site_admin', 'wiki.page.create'),
    ('site_admin', 'wiki.page.manage.read'),
    ('site_admin', 'wiki.page.restore'),
    ('site_admin', 'wiki.page.update');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE action IN (
    'wiki.page.create',
    'wiki.page.manage.read',
    'wiki.page.read',
    'wiki.page.read.member',
    'wiki.page.restore',
    'wiki.page.update',
    'wiki.page.update.assigned'
);

DELETE FROM authz.permissions
WHERE action IN (
    'wiki.page.create',
    'wiki.page.manage.read',
    'wiki.page.read',
    'wiki.page.read.member',
    'wiki.page.restore',
    'wiki.page.update',
    'wiki.page.update.assigned'
);

DROP TABLE community.wiki_revisions;
DROP TABLE community.wiki_page_editors;
DROP TABLE community.wiki_pages;
