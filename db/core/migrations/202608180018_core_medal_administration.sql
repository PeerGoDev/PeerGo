-- +goose Up

-- Migration evidence is deliberately immutable, but medal definitions are
-- live business data. This runtime singleton copies the cutover limits out of
-- migration.* so native PeerGo installations and later administration code do
-- not depend on the presence of a Rousi receipt.
CREATE TABLE economy.medal_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    enabled boolean NOT NULL,
    maximum_wear_count bigint NOT NULL CHECK (maximum_wear_count BETWEEN 0 AND 100),
    maximum_upload_bonus_bps bigint NOT NULL CHECK (maximum_upload_bonus_bps BETWEEN 0 AND 100000),
    maximum_download_discount_bps bigint NOT NULL CHECK (maximum_download_discount_bps BETWEEN 0 AND 100000),
    maximum_magic_bonus_bps bigint NOT NULL CHECK (maximum_magic_bonus_bps BETWEEN 0 AND 100000),
    maximum_invite_bonus bigint NOT NULL CHECK (maximum_invite_bonus BETWEEN 0 AND 1000000),
    condition_check_day bigint NOT NULL CHECK (condition_check_day BETWEEN 1 AND 28),
    condition_warning_days bigint NOT NULL CHECK (condition_warning_days BETWEEN 0 AND 365),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL
);

INSERT INTO economy.medal_settings (
    enabled, maximum_wear_count, maximum_upload_bonus_bps,
    maximum_download_discount_bps, maximum_magic_bonus_bps,
    maximum_invite_bonus, condition_check_day, condition_warning_days,
    updated_at
)
SELECT
    enabled, maximum_wear_count, maximum_upload_bonus_bps,
    maximum_download_discount_bps, maximum_magic_bonus_bps,
    maximum_invite_bonus, condition_check_day, condition_warning_days,
    imported_at
FROM migration.medal_system_openings
WHERE source_system = 'ptyes'
UNION ALL
SELECT true, 5, 10000, 10000, 10000, 10, 1, 7, clock_timestamp()
WHERE NOT EXISTS (
    SELECT 1 FROM migration.medal_system_openings WHERE source_system = 'ptyes'
);

-- Definitions are updated in place for efficient reads, while every resulting
-- version is also frozen here. The authorization decision links the human
-- reason to Core's separately recorded permission evidence.
CREATE TABLE economy.medal_definition_revisions (
    medal_id bigint NOT NULL
        REFERENCES economy.medal_definitions (id) ON DELETE RESTRICT,
    version bigint NOT NULL CHECK (version > 0),
    transition text NOT NULL CHECK (transition IN ('imported', 'created', 'updated')),
    snapshot_json jsonb NOT NULL CHECK (jsonb_typeof(snapshot_json) = 'object'),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 500),
    changed_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (medal_id, version),
    CHECK (
        (transition = 'imported' AND version = 1 AND changed_by IS NULL
            AND authorization_decision_id IS NULL)
        OR
        (transition IN ('created', 'updated') AND changed_by IS NOT NULL
            AND authorization_decision_id IS NOT NULL)
    )
);

CREATE TRIGGER medal_definition_revisions_immutable
BEFORE UPDATE OR DELETE ON economy.medal_definition_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO economy.medal_definition_revisions (
    medal_id, version, transition, snapshot_json, reason, created_at
)
SELECT
    definition.id,
    definition.version,
    'imported',
    to_jsonb(definition) - 'created_at' - 'updated_at',
    'Rousi 勋章定义迁移基线；原图片路径仅保留在迁移证据中。',
    definition.created_at
FROM economy.medal_definitions AS definition
ORDER BY definition.id;

INSERT INTO authz.permissions (
    action, description, risk_level, relationship, credential_audience,
    grantable, discoverable
) VALUES
    ('economy.medal.create', '创建勋章定义', 'high', 'none', 'staff-session', true, true),
    ('economy.medal.manage.read', '读取勋章定义和持有数量', 'medium', 'none', 'staff-session', true, true),
    ('economy.medal.update', '更新勋章图片、发放方式和权益参数', 'high', 'none', 'staff-session', true, true);

INSERT INTO authz.roles (id, name, description, assignable) VALUES (
    'medal_manager',
    '勋章管理员',
    '维护勋章定义、图片地址与权益参数；不包含向用户发放或收回勋章。',
    true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('medal_manager', 'economy.medal.create'),
    ('medal_manager', 'economy.medal.manage.read'),
    ('medal_manager', 'economy.medal.update'),
    ('site_admin', 'economy.medal.create'),
    ('site_admin', 'economy.medal.manage.read'),
    ('site_admin', 'economy.medal.update');

REVOKE ALL ON economy.medal_settings FROM PUBLIC;
REVOKE ALL ON economy.medal_definition_revisions FROM PUBLIC;

-- +goose Down

DELETE FROM authz.grants WHERE role_id = 'medal_manager';
DELETE FROM authz.role_permissions
WHERE role_id = 'medal_manager'
   OR (role_id = 'site_admin' AND action IN (
       'economy.medal.create',
       'economy.medal.manage.read',
       'economy.medal.update'
   ));
DELETE FROM authz.roles WHERE id = 'medal_manager';
DELETE FROM authz.permissions WHERE action IN (
    'economy.medal.create',
    'economy.medal.manage.read',
    'economy.medal.update'
);

DROP TRIGGER medal_definition_revisions_immutable
    ON economy.medal_definition_revisions;
DROP TABLE economy.medal_definition_revisions;
DROP TABLE economy.medal_settings;
