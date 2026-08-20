-- +goose Up

-- Public member profiles are a member-directory capability, distinct from the
-- privileged user.account.read projection. It permits only the bounded public
-- fields exposed by Core and does not imply traffic, credential or moderation
-- access for another account.
INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'user.profile.read.member',
    '查看站内成员的公开资料',
    'low',
    'none',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'user.profile.read.member');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'member'
  AND action = 'user.profile.read.member';

DELETE FROM authz.permissions
WHERE action = 'user.profile.read.member';
