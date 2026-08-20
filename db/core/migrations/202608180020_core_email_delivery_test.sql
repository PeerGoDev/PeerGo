-- +goose Up

-- Sending a real message is intentionally separate from read-only operations
-- visibility. The site administrator receives the capability by default, while
-- custom staff roles must be granted it explicitly.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES (
    'operations.email.test', '向指定地址发送一次邮件投递测试',
    'high', 'none', 'staff-session', true, true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('site_admin', 'operations.email.test');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'site_admin' AND action = 'operations.email.test';
DELETE FROM authz.permissions WHERE action = 'operations.email.test';
