-- +goose Up
-- The simple site administrator is managed from the local operator CLI. Keep
-- the first-release UI focused on site operation and do not expose the older
-- multi-party grant-governance workflow through this aggregate role.
DELETE FROM authz.role_permissions
WHERE role_id = 'site_admin'
  AND action LIKE 'authz.grant.%';

-- +goose Down
INSERT INTO authz.role_permissions (role_id, action)
SELECT 'site_admin', permission.action
FROM authz.permissions AS permission
WHERE permission.action LIKE 'authz.grant.%'
ON CONFLICT DO NOTHING;
