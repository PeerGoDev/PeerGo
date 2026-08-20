-- +goose Up
-- Categories are durable business objects referenced by torrents, not scalar
-- settings. Administration therefore edits a stable row in place, never
-- hard-deletes it, and uses a monotonic version for optimistic concurrency.
ALTER TABLE catalog.categories
    DROP CONSTRAINT categories_name_check,
    DROP CONSTRAINT categories_display_order_check,
    ADD CONSTRAINT categories_id_format_check
        CHECK (id ~ '^[a-z0-9][a-z0-9-]{0,63}$'),
    ADD CONSTRAINT categories_name_check
        CHECK (char_length(btrim(name)) BETWEEN 1 AND 40),
    ADD CONSTRAINT categories_display_order_check
        CHECK (display_order BETWEEN 0 AND 1000000),
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- A display order is a sortable weight rather than an identity. Ties are
-- intentionally legal and resolved by category ID, which avoids unsafe bulk
-- rewrites merely to insert one category between two existing values.
DROP INDEX catalog.categories_display_order_unique;
CREATE INDEX categories_display_order_idx
    ON catalog.categories (display_order, id);

INSERT INTO authz.permissions (
    action,
    description,
    risk_level,
    relationship,
    credential_audience,
    grantable,
    discoverable
) VALUES
    ('category.create', '创建种子分类', 'medium', 'none', 'staff-session', true, true),
    ('category.manage.read', '读取分类管理视图', 'medium', 'none', 'staff-session', true, true),
    ('category.update', '更新或停用种子分类', 'high', 'none', 'staff-session', true, true);

INSERT INTO authz.roles (id, name, description, assignable) VALUES (
    'category_manager',
    '分类管理员',
    '维护分类业务对象；不包含种子审核、通用设置或其他内容管理权限。',
    true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('category_manager', 'category.create'),
    ('category_manager', 'category.manage.read'),
    ('category_manager', 'category.update');

-- +goose Down
DELETE FROM authz.grants WHERE role_id = 'category_manager';
DELETE FROM authz.role_permissions WHERE role_id = 'category_manager';
DELETE FROM authz.roles WHERE id = 'category_manager';
DELETE FROM authz.permissions WHERE action IN (
    'category.create',
    'category.manage.read',
    'category.update'
);

DROP INDEX catalog.categories_display_order_idx;

-- Migration 010 permits equal sort weights. The legacy schema required a
-- unique integer, so a development rollback deterministically compacts the
-- current visual order before recreating that obsolete constraint.
WITH ordered AS (
    SELECT id, (row_number() OVER (ORDER BY display_order, id) * 10)::integer AS new_order
    FROM catalog.categories
)
UPDATE catalog.categories AS category
SET display_order = ordered.new_order
FROM ordered
WHERE category.id = ordered.id;

ALTER TABLE catalog.categories
    DROP CONSTRAINT categories_id_format_check,
    DROP CONSTRAINT categories_name_check,
    DROP CONSTRAINT categories_display_order_check,
    DROP COLUMN updated_at,
    DROP COLUMN version,
    ADD CONSTRAINT categories_name_check
        CHECK (char_length(name) BETWEEN 1 AND 40),
    ADD CONSTRAINT categories_display_order_check
        CHECK (display_order >= 0);

CREATE UNIQUE INDEX categories_display_order_unique
    ON catalog.categories (display_order);
