-- +goose Up

-- Workgroups are business roles with fixed, code-owned semantics. They are not
-- another free-form authorization system: a group kind maps to one narrowly
-- defined entitlement consumed by the torrent or settlement domain.
CREATE SCHEMA workgroups;

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('workgroup.application.create.self', '申请加入允许自助申请的工作组', 'medium', 'self', 'web-session', true, true),
    ('workgroup.application.decide', '审批工作组加入申请', 'high', 'none', 'staff-session', true, true),
    ('workgroup.membership.manage', '授予、暂停、恢复或结束工作组成员资格', 'high', 'none', 'staff-session', true, true),
    ('workgroup.read.self', '查看自己的工作组资格与申请状态', 'low', 'self', 'web-session', true, true),
    ('workgroup.manage.read', '查看工作组申请、成员与不可变变更记录', 'medium', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'workgroup.application.create.self'),
    ('member', 'workgroup.read.self'),
    ('site_admin', 'workgroup.application.decide'),
    ('site_admin', 'workgroup.membership.manage'),
    ('site_admin', 'workgroup.manage.read');

INSERT INTO authz.roles (id, name, description, assignable) VALUES (
    'workgroup_manager',
    '工作组管理员',
    '管理转种组、种审组和保种组的申请与成员资格；不自动获得种子终审、用户限制或站点设置权限。',
    true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('workgroup_manager', 'workgroup.application.decide'),
    ('workgroup_manager', 'workgroup.membership.manage'),
    ('workgroup_manager', 'workgroup.manage.read');

CREATE TABLE workgroups.definitions (
    kind text PRIMARY KEY CHECK (kind IN ('reseed', 'review', 'retention')),
    display_name text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 32),
    description text NOT NULL CHECK (char_length(description) BETWEEN 1 AND 500),
    join_mode text NOT NULL CHECK (join_mode IN ('staff_only', 'application')),
    enabled boolean NOT NULL DEFAULT true,
    sort_order smallint NOT NULL UNIQUE CHECK (sort_order BETWEEN 1 AND 99),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

INSERT INTO workgroups.definitions (
    kind, display_name, description, join_mode, sort_order, created_at, updated_at
) VALUES
    ('reseed', '转种组', '可信转种成员可在通过解析、安全、重复与分类校验后跳过人工审核；所有直发仍保留证据。', 'staff_only', 10, now(), now()),
    ('review', '种审组', '通过申请与考核的成员参与种子多人审核；投票权不等于管理员最终裁决权。', 'application', 20, now(), now()),
    ('retention', '保种组', '承担长期保种任务；有效成员期内的下载保留原始流量但结算下载计费为零。', 'staff_only', 30, now(), now());

-- Reviewer admission policy is revisioned and immutable. An application stores
-- both the revision and the evaluated snapshot so later policy changes cannot
-- rewrite why an old request was accepted for review.
CREATE TABLE workgroups.review_application_policy_revisions (
    revision bigint PRIMARY KEY CHECK (revision > 0),
    effective_from timestamptz NOT NULL UNIQUE,
    minimum_level smallint NOT NULL CHECK (minimum_level BETWEEN 1 AND 99),
    minimum_credited_uploaded bigint NOT NULL CHECK (minimum_credited_uploaded >= 0),
    minimum_account_age_days integer NOT NULL CHECK (minimum_account_age_days >= 0),
    require_verified_email boolean NOT NULL,
    require_unrestricted_download boolean NOT NULL,
    created_at timestamptz NOT NULL
);

INSERT INTO workgroups.review_application_policy_revisions (
    revision, effective_from, minimum_level, minimum_credited_uploaded,
    minimum_account_age_days, require_verified_email,
    require_unrestricted_download, created_at
) VALUES (1, '-infinity', 3, 53687091200, 30, true, true, now());

CREATE TABLE workgroups.applications (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE,
    group_kind text NOT NULL REFERENCES workgroups.definitions (kind) ON DELETE RESTRICT,
    applicant_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    statement text NOT NULL CHECK (char_length(btrim(statement)) BETWEEN 20 AND 1000),
    status text NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
    policy_revision bigint REFERENCES workgroups.review_application_policy_revisions (revision) ON DELETE RESTRICT,
    eligibility_snapshot jsonb NOT NULL CHECK (jsonb_typeof(eligibility_snapshot) = 'object'),
    submission_authorization_decision_id uuid NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    submitted_at timestamptz NOT NULL,
    decided_at timestamptz,
    updated_at timestamptz NOT NULL,
    CHECK ((status = 'pending') = (decided_at IS NULL)),
    CHECK ((group_kind = 'review') = (policy_revision IS NOT NULL))
);

CREATE UNIQUE INDEX workgroup_applications_one_pending_idx
    ON workgroups.applications (applicant_id, group_kind)
    WHERE status = 'pending';
CREATE INDEX workgroup_applications_staff_queue_idx
    ON workgroups.applications (status, submitted_at, id);

CREATE TABLE workgroups.application_transitions (
    id uuid PRIMARY KEY,
    application_id uuid NOT NULL REFERENCES workgroups.applications (id) ON DELETE RESTRICT,
    transition text NOT NULL CHECK (transition IN ('submitted', 'approved', 'rejected')),
    from_status text CHECK (from_status IN ('pending', 'approved', 'rejected')),
    to_status text NOT NULL CHECK (to_status IN ('pending', 'approved', 'rejected')),
    actor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 1000),
    authorization_decision_id uuid NOT NULL,
    state_version bigint NOT NULL CHECK (state_version > 0),
    occurred_at timestamptz NOT NULL,
    UNIQUE (application_id, state_version),
    CHECK (
        (transition = 'submitted' AND from_status IS NULL AND to_status = 'pending' AND state_version = 1)
        OR (transition = 'approved' AND from_status = 'pending' AND to_status = 'approved' AND state_version = 2)
        OR (transition = 'rejected' AND from_status = 'pending' AND to_status = 'rejected' AND state_version = 2)
    )
);

CREATE INDEX workgroup_application_transitions_history_idx
    ON workgroups.application_transitions (application_id, occurred_at DESC, id DESC);

CREATE TABLE workgroups.memberships (
    id uuid PRIMARY KEY,
    group_kind text NOT NULL REFERENCES workgroups.definitions (kind) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    status text NOT NULL CHECK (status IN ('active', 'suspended', 'ended')),
    source text NOT NULL CHECK (source IN ('application', 'staff')),
    source_application_id uuid REFERENCES workgroups.applications (id) ON DELETE RESTRICT,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    started_at timestamptz NOT NULL,
    ended_at timestamptz,
    updated_at timestamptz NOT NULL,
    UNIQUE (group_kind, user_id),
    CHECK ((source = 'application') = (source_application_id IS NOT NULL)),
    CHECK ((status = 'ended') = (ended_at IS NOT NULL))
);

CREATE INDEX workgroup_memberships_staff_list_idx
    ON workgroups.memberships (group_kind, status, started_at DESC, id DESC);

-- Membership transitions are the authoritative point-in-time entitlement
-- timeline. Settlement can resolve the latest transition at an announce time;
-- it must never infer historical entitlement from today's membership row.
CREATE TABLE workgroups.membership_transitions (
    id uuid PRIMARY KEY,
    membership_id uuid NOT NULL REFERENCES workgroups.memberships (id) ON DELETE RESTRICT,
    group_kind text NOT NULL REFERENCES workgroups.definitions (kind) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    transition text NOT NULL CHECK (transition IN ('joined', 'suspended', 'reactivated', 'ended')),
    from_status text CHECK (from_status IN ('active', 'suspended', 'ended')),
    to_status text NOT NULL CHECK (to_status IN ('active', 'suspended', 'ended')),
    actor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    source text NOT NULL CHECK (source IN ('application', 'staff')),
    source_application_id uuid REFERENCES workgroups.applications (id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 1000),
    authorization_decision_id uuid NOT NULL,
    state_version bigint NOT NULL CHECK (state_version > 0),
    occurred_at timestamptz NOT NULL,
    UNIQUE (membership_id, state_version),
    CHECK (
        (transition = 'joined' AND from_status IS NULL AND to_status = 'active' AND state_version = 1)
        OR (transition = 'suspended' AND from_status = 'active' AND to_status = 'suspended')
        OR (transition = 'reactivated' AND from_status IN ('suspended', 'ended') AND to_status = 'active')
        OR (transition = 'ended' AND from_status IN ('active', 'suspended') AND to_status = 'ended')
    ),
    CHECK ((source = 'application') = (source_application_id IS NOT NULL))
);

CREATE INDEX workgroup_membership_timeline_idx
    ON workgroups.membership_transitions (user_id, group_kind, occurred_at DESC, state_version DESC);

-- +goose StatementBegin
CREATE FUNCTION workgroups.reject_history_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'workgroup history is immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER workgroup_application_transitions_immutable
BEFORE UPDATE OR DELETE ON workgroups.application_transitions
FOR EACH ROW EXECUTE FUNCTION workgroups.reject_history_mutation();

CREATE TRIGGER workgroup_membership_transitions_immutable
BEFORE UPDATE OR DELETE ON workgroups.membership_transitions
FOR EACH ROW EXECUTE FUNCTION workgroups.reject_history_mutation();

CREATE TRIGGER workgroup_review_policy_revisions_immutable
BEFORE UPDATE OR DELETE ON workgroups.review_application_policy_revisions
FOR EACH ROW EXECUTE FUNCTION workgroups.reject_history_mutation();

REVOKE ALL ON SCHEMA workgroups FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA workgroups FROM PUBLIC;

-- +goose Down

DROP SCHEMA workgroups CASCADE;

DELETE FROM authz.grants WHERE role_id = 'workgroup_manager';
DELETE FROM authz.role_permissions WHERE role_id = 'workgroup_manager';
DELETE FROM authz.roles WHERE id = 'workgroup_manager';
DELETE FROM authz.role_permissions
WHERE action IN (
    'workgroup.application.create.self',
    'workgroup.application.decide',
    'workgroup.membership.manage',
    'workgroup.read.self',
    'workgroup.manage.read'
);
DELETE FROM authz.permissions
WHERE action IN (
    'workgroup.application.create.self',
    'workgroup.application.decide',
    'workgroup.membership.manage',
    'workgroup.read.self',
    'workgroup.manage.read'
);
