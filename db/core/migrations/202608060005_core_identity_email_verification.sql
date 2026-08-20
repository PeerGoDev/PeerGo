-- +goose Up
-- Email verification is a self-service Web-session action. It is catalogued so
-- capability discovery and the reviewed OpenAPI permission never drift apart.
INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'account.email.confirm.anonymous',
    '使用一次性凭证确认邮箱所有权',
    'medium',
    'none',
    'anonymous',
    false,
    false
), (
    'account.email.verify.self',
    '验证自己的登录邮箱',
    'medium',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'account.email.verify.self');

-- +goose Down
DELETE FROM authz.role_permissions
WHERE role_id = 'member' AND action = 'account.email.verify.self';

DELETE FROM authz.permissions
WHERE action IN ('account.email.confirm.anonymous', 'account.email.verify.self');
