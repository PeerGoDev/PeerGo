-- +goose Up

-- H&R administration is intentionally separate from general operations
-- monitoring. Reading worker health must not imply authority to change a rule
-- that can ultimately restrict a member's downloading behavior.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('hnr.policy.read', '读取 H&R 政策时间线与投递状态', 'medium', 'none', 'staff-session', true, true),
    ('hnr.policy.issue', '签发未来生效的全站 H&R 政策', 'high', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'hnr.policy.read'),
    ('site_admin', 'hnr.policy.issue');

CREATE SCHEMA hnr_control;

-- Core stores administrator intent and authorization evidence. Settlement
-- remains the only authority that resolves this policy against completion
-- facts; command_json is the privacy-minimized canonical delivery payload.
CREATE TABLE hnr_control.policy_revisions (
    id uuid PRIMARY KEY,
    rule_id text NOT NULL CHECK (
        char_length(rule_id) BETWEEN 1 AND 128 AND rule_id = btrim(rule_id)
    ),
    rule_version bigint NOT NULL CHECK (rule_version > 0),
    mode text NOT NULL CHECK (mode IN ('disabled', 'exempt', 'enforced')),
    required_seed_seconds bigint NOT NULL CHECK (required_seed_seconds >= 0),
    required_ratio_basis_points bigint NOT NULL CHECK (
        required_ratio_basis_points BETWEEN 0 AND 1000000
    ),
    assessment_window_seconds bigint NOT NULL CHECK (
        assessment_window_seconds BETWEEN 0 AND 315360000
    ),
    grace_period_seconds bigint NOT NULL CHECK (
        grace_period_seconds BETWEEN 0 AND 31536000
    ),
    max_interval_credit_seconds bigint NOT NULL CHECK (
        max_interval_credit_seconds BETWEEN 0 AND 86400
    ),
    effective_at timestamptz NOT NULL,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    actor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid NOT NULL,
    command_json text NOT NULL CHECK (
        octet_length(command_json) BETWEEN 3 AND 8192
        AND jsonb_typeof(command_json::jsonb) = 'object'
    ),
    command_sha256 bytea NOT NULL CHECK (octet_length(command_sha256) = 32),
    created_at timestamptz NOT NULL,
    UNIQUE (rule_id, rule_version),
    UNIQUE (effective_at),
    CHECK (effective_at >= created_at + interval '5 minutes'),
    CHECK (effective_at <= created_at + interval '365 days'),
    CHECK (
        (mode IN ('disabled', 'exempt')
            AND required_seed_seconds = 0
            AND required_ratio_basis_points = 0
            AND assessment_window_seconds = 0
            AND grace_period_seconds = 0
            AND max_interval_credit_seconds = 0)
        OR
        (mode = 'enforced'
            AND (required_seed_seconds > 0 OR required_ratio_basis_points > 0)
            AND assessment_window_seconds >= required_seed_seconds
            AND assessment_window_seconds > 0
            AND max_interval_credit_seconds BETWEEN 60 AND 86400)
    )
);

CREATE INDEX hnr_policy_revisions_timeline_idx
    ON hnr_control.policy_revisions (effective_at DESC, id DESC);

-- Delivery metadata can advance independently while every business field in
-- policy_revisions remains immutable. Exact retries are acknowledged by
-- Settlement using the revision UUID and canonical SHA-256 digest.
CREATE TABLE hnr_control.delivery_outbox (
    revision_id uuid PRIMARY KEY
        REFERENCES hnr_control.policy_revisions (id) ON DELETE RESTRICT,
    available_at timestamptz NOT NULL,
    lease_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    delivered_at timestamptz,
    created_at timestamptz NOT NULL,
    CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
    CHECK (delivered_at IS NULL OR (lease_token IS NULL AND last_error_code IS NULL))
);

CREATE INDEX hnr_delivery_ready_idx
    ON hnr_control.delivery_outbox (available_at, revision_id)
    WHERE delivered_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION hnr_control.reject_policy_revision_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'H&R policy revisions are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER hnr_policy_revisions_immutable
BEFORE UPDATE OR DELETE ON hnr_control.policy_revisions
FOR EACH ROW EXECUTE FUNCTION hnr_control.reject_policy_revision_mutation();

-- +goose Down

DROP TRIGGER hnr_policy_revisions_immutable ON hnr_control.policy_revisions;
DROP FUNCTION hnr_control.reject_policy_revision_mutation();
DROP INDEX hnr_control.hnr_delivery_ready_idx;
DROP TABLE hnr_control.delivery_outbox;
DROP INDEX hnr_control.hnr_policy_revisions_timeline_idx;
DROP TABLE hnr_control.policy_revisions;
DROP SCHEMA hnr_control;

DELETE FROM authz.role_permissions
WHERE role_id = 'site_admin'
  AND action IN ('hnr.policy.read', 'hnr.policy.issue');

DELETE FROM authz.permissions
WHERE action IN ('hnr.policy.read', 'hnr.policy.issue');
