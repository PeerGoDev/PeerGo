-- +goose Up

-- The live peer projection contains no network endpoints or protocol
-- identifiers, but it is intentionally member-only. Keep this permission
-- separate from anonymous torrent reads so the contract and runtime policy
-- cannot silently widen access.
INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES (
    'torrent.peer.read.member',
    '查看已发布种子的隐私最小化实时用户列表',
    'low',
    'none',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'torrent.peer.read.member');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'member' AND action = 'torrent.peer.read.member';

DELETE FROM authz.permissions
WHERE action = 'torrent.peer.read.member';
