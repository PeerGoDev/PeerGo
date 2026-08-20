-- +goose Up
-- Registration admission is an identity concern. These permissions expose the
-- existing typed singleton to staff without recreating it in generic settings.
INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES
    ('site.registration.manage.read', '读取站点注册准入策略', 'low', 'none', 'staff-session', true, true),
    ('site.registration.update', '更新站点注册准入策略', 'medium', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'site.registration.manage.read'),
    ('site_admin', 'site.registration.update');

-- +goose Down
DELETE FROM authz.role_permissions
WHERE role_id = 'site_admin'
  AND action IN ('site.registration.manage.read', 'site.registration.update');

DELETE FROM authz.permissions
WHERE action IN ('site.registration.manage.read', 'site.registration.update');
