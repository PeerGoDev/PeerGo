-- +goose Up

-- Public nickname changes are a bounded self-service action. Keeping the
-- permission separate from session.read.self prevents a read grant from
-- silently becoming write authority.
INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'account.profile.update.self',
    '修改自己的公开资料',
    'medium',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'account.profile.update.self');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'member'
  AND action = 'account.profile.update.self';

DELETE FROM authz.permissions
WHERE action = 'account.profile.update.self';
