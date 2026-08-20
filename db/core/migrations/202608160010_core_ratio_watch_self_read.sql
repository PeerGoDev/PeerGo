-- +goose Up

-- A member may inspect only their own long-term ratio assessment. This is
-- intentionally separate from staff policy reads: the user projection omits
-- policy UUIDs, operator reasons, staff identities and transition evidence.
INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'ratio.assessment.read.self',
    '查看自己的长期分享率考核与恢复进度',
    'low',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'ratio.assessment.read.self');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'member' AND action = 'ratio.assessment.read.self';
DELETE FROM authz.permissions WHERE action = 'ratio.assessment.read.self';
