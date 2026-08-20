-- +goose Up

-- Reading one's own upload and its review feedback is intentionally separate
-- from submitting new bytes. A suspended write grant must not erase the
-- uploader's ability to inspect already-persisted review outcomes.
INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'torrent.submission.read.self',
    '查看自己的种子提交状态与审核反馈',
    'low',
    'self',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'torrent.submission.read.self');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'member' AND action = 'torrent.submission.read.self';
DELETE FROM authz.permissions WHERE action = 'torrent.submission.read.self';
