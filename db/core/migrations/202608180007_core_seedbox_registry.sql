-- +goose Up

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('tracker.seedbox.read.self', '查看自己的盒子申报和审核结果', 'low', 'self', 'web-session', true, true),
    ('tracker.seedbox.report.create.self', '申报自己的盒子主机地址等待审核', 'medium', 'self', 'web-session', true, true),
    ('tracker.seedbox.registry.read', '查看盒子申报、用户绑定地址和审核记录', 'medium', 'none', 'staff-session', true, true),
    ('tracker.seedbox.report.decide', '批准或驳回盒子申报并签发用户绑定规则', 'high', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'tracker.seedbox.read.self'),
    ('member', 'tracker.seedbox.report.create.self'),
    ('site_admin', 'tracker.seedbox.registry.read'),
    ('site_admin', 'tracker.seedbox.report.decide');

CREATE TABLE tracker_control.seedbox_reports (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    network cidr NOT NULL,
    provider text NOT NULL CHECK (char_length(btrim(provider)) BETWEEN 2 AND 100),
    bandwidth_mbps bigint NOT NULL CHECK (bandwidth_mbps BETWEEN 1 AND 10000000),
    statement text NOT NULL CHECK (char_length(btrim(statement)) BETWEEN 10 AND 1000),
    status text NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
    version bigint NOT NULL CHECK (version > 0),
    authorization_decision_id uuid NOT NULL,
    submitted_at timestamptz NOT NULL,
    decided_at timestamptz,
    decided_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    decision_reason text CHECK (decision_reason IS NULL OR char_length(btrim(decision_reason)) BETWEEN 10 AND 1000),
    policy_sequence bigint REFERENCES tracker_control.runtime_policy_revisions (sequence) ON DELETE RESTRICT,
    CHECK (
        (status = 'pending' AND decided_at IS NULL AND decided_by IS NULL AND decision_reason IS NULL AND policy_sequence IS NULL)
        OR (status = 'approved' AND decided_at IS NOT NULL AND decided_by IS NOT NULL AND decision_reason IS NOT NULL AND policy_sequence IS NOT NULL)
        OR (status = 'rejected' AND decided_at IS NOT NULL AND decided_by IS NOT NULL AND decision_reason IS NOT NULL AND policy_sequence IS NULL)
    )
);

CREATE UNIQUE INDEX seedbox_report_one_pending_per_user_idx
    ON tracker_control.seedbox_reports (user_id)
    WHERE status = 'pending';

CREATE UNIQUE INDEX seedbox_report_one_approved_binding_idx
    ON tracker_control.seedbox_reports (user_id, network)
    WHERE status = 'approved';

CREATE INDEX seedbox_report_staff_queue_idx
    ON tracker_control.seedbox_reports (status, submitted_at DESC, id DESC);

CREATE TABLE tracker_control.seedbox_report_decisions (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE,
    report_id uuid NOT NULL UNIQUE REFERENCES tracker_control.seedbox_reports (id) ON DELETE RESTRICT,
    decision text NOT NULL CHECK (decision IN ('approve', 'reject')),
    expected_version bigint NOT NULL CHECK (expected_version > 0),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    actor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid NOT NULL,
    policy_sequence bigint REFERENCES tracker_control.runtime_policy_revisions (sequence) ON DELETE RESTRICT,
    decided_at timestamptz NOT NULL
);

CREATE TRIGGER seedbox_report_decision_immutable
BEFORE UPDATE OR DELETE ON tracker_control.seedbox_report_decisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

REVOKE ALL ON tracker_control.seedbox_reports FROM PUBLIC;
REVOKE ALL ON tracker_control.seedbox_report_decisions FROM PUBLIC;

-- +goose Down

DROP TRIGGER seedbox_report_decision_immutable ON tracker_control.seedbox_report_decisions;
DROP TABLE tracker_control.seedbox_report_decisions;
DROP INDEX tracker_control.seedbox_report_staff_queue_idx;
DROP INDEX tracker_control.seedbox_report_one_approved_binding_idx;
DROP INDEX tracker_control.seedbox_report_one_pending_per_user_idx;
DROP TABLE tracker_control.seedbox_reports;
DELETE FROM authz.role_permissions WHERE action IN (
    'tracker.seedbox.read.self', 'tracker.seedbox.report.create.self',
    'tracker.seedbox.registry.read', 'tracker.seedbox.report.decide'
);
DELETE FROM authz.permissions WHERE action IN (
    'tracker.seedbox.read.self', 'tracker.seedbox.report.create.self',
    'tracker.seedbox.registry.read', 'tracker.seedbox.report.decide'
);
