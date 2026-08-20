-- +goose Up

-- Reading one's final Core projection is a distinct self capability. It does
-- not imply access to Tracker evidence, other users, adjustments or economy
-- balances; adding it to member only extends existing finite member grants.
INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'traffic.read.self',
    '查看自己的最终流量汇总与结算记录',
    'low',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'traffic.read.self');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'member' AND action = 'traffic.read.self';
DELETE FROM authz.permissions WHERE action = 'traffic.read.self';
