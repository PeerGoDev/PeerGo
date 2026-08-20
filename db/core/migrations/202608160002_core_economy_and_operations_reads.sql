-- +goose Up

-- Members may read only their own integer magic statement and experience
-- projection. Operational monitoring is a separate staff capability: it does
-- not grant queue mutation, replay, restart or access to Tracker Ledger.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('economy.read.self', '查看自己的魔力值账本、经验和等级进度', 'low', 'self', 'web-session', true, true),
    ('operations.monitor.read', '读取 Core 中的 Tracker 投影与 Worker 队列状态', 'medium', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'economy.read.self'),
    ('site_admin', 'operations.monitor.read');

-- +goose Down

DELETE FROM authz.role_permissions
WHERE (role_id = 'member' AND action = 'economy.read.self')
   OR (role_id = 'site_admin' AND action = 'operations.monitor.read');

DELETE FROM authz.permissions
WHERE action IN ('economy.read.self', 'operations.monitor.read');
