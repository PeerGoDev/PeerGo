-- +goose Up
-- Site display settings are a bounded, typed singleton owned by catalog. They
-- deliberately extend the existing public site profile instead of introducing
-- a generic key/value settings table or a second source of site metadata.
ALTER TABLE catalog.site_profile
    DROP CONSTRAINT site_profile_name_check,
    ADD CONSTRAINT site_profile_name_check
        CHECK (char_length(btrim(name)) BETWEEN 1 AND 80),
    ADD COLUMN description text NOT NULL DEFAULT ''
        CHECK (char_length(description) <= 500),
    ADD COLUMN default_torrent_view text NOT NULL DEFAULT 'list'
        CHECK (default_torrent_view IN ('list', 'poster')),
    ADD COLUMN show_latest_announcement boolean NOT NULL DEFAULT true,
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    ADD COLUMN effective_at timestamptz NOT NULL DEFAULT now();

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES
    ('site.display.manage.read', '读取站点与展示设置', 'low', 'none', 'staff-session', true, true),
    ('site.display.update', '更新低风险站点与展示设置', 'low', 'none', 'staff-session', true, true);

INSERT INTO authz.roles (id, name, description, assignable) VALUES (
    'site_display_manager',
    '站点展示管理员',
    '维护站点名称、说明和公开页展示默认值；不包含身份准入或其他领域设置。',
    true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_display_manager', 'site.display.manage.read'),
    ('site_display_manager', 'site.display.update');

-- +goose Down
DELETE FROM authz.grants WHERE role_id = 'site_display_manager';
DELETE FROM authz.role_permissions WHERE role_id = 'site_display_manager';
DELETE FROM authz.roles WHERE id = 'site_display_manager';
DELETE FROM authz.permissions WHERE action IN (
    'site.display.manage.read',
    'site.display.update'
);

ALTER TABLE catalog.site_profile
    DROP COLUMN effective_at,
    DROP COLUMN version,
    DROP COLUMN show_latest_announcement,
    DROP COLUMN default_torrent_view,
    DROP COLUMN description,
    DROP CONSTRAINT site_profile_name_check,
    ADD CONSTRAINT site_profile_name_check
        CHECK (char_length(name) BETWEEN 1 AND 80);
