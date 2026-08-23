-- +goose Up

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'social.post.create.restricted.self',
    '向仅限管理团队的动态圈板块发布动态',
    'medium',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('community_moderator', 'social.post.create.restricted.self'),
    ('site_admin', 'social.post.create.restricted.self');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id IN ('community_moderator', 'site_admin')
  AND action = 'social.post.create.restricted.self';

DELETE FROM authz.permissions
WHERE action = 'social.post.create.restricted.self';
