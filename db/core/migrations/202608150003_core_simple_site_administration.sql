-- +goose Up
-- PeerGo's first release exposes one practical administrator role. The
-- existing grant/mandate tables remain the internal authorization mechanism,
-- but operators no longer have to assemble several staff roles by hand.
INSERT INTO authz.roles (id, name, description, assignable) VALUES (
    'site_admin',
    '站点管理员',
    '通过普通站点账号登录并管理当前已实现的全部后台功能。',
    true
);

-- Administrative actions keep their typed permission definitions and are
-- still checked inside each owning use case. The one Web-audience action is
-- the explicit gate that allows an authenticated account to enter the admin
-- surface; WebAuthn enrollment remains optional and is not bundled here.
INSERT INTO authz.role_permissions (role_id, action)
SELECT 'site_admin', permission.action
FROM authz.permissions AS permission
WHERE (
        permission.credential_audience = 'staff-session'
        AND permission.action NOT LIKE 'authz.grant.%'
    )
   OR permission.action = 'staff.session.create.self';

-- +goose Down
DELETE FROM authz.grants WHERE role_id = 'site_admin';
DELETE FROM governance.mandates
WHERE source_type = 'bootstrap' AND source_reference = 'site-admin-cli';
DELETE FROM authz.role_permissions WHERE role_id = 'site_admin';
DELETE FROM authz.roles WHERE id = 'site_admin';
