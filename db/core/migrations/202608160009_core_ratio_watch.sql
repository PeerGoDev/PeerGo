-- +goose Up

-- Long-term total-ratio assessment is deliberately independent from per-
-- torrent H&R. Reading settings, issuing a future rule and clearing one
-- assessment are separate authorities so an operator can be granted only the
-- minimum capability required for their duty.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('ratio.assessment.manage', '人工解除一条长期分享率考核', 'high', 'none', 'staff-session', true, true),
    ('ratio.policy.issue', '签发未来生效的全站长期分享率规则', 'high', 'none', 'staff-session', true, true),
    ('ratio.policy.read', '读取长期分享率规则、考核和运行状态', 'medium', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'ratio.assessment.manage'),
    ('site_admin', 'ratio.policy.issue'),
    ('site_admin', 'ratio.policy.read');

CREATE SCHEMA ratio_watch;

-- A revision is future-only and immutable. Existing assessments retain the
-- exact revision that admitted them, so a later setting change cannot rewrite
-- a member's deadline or threshold halfway through an observation window.
CREATE TABLE ratio_watch.policy_revisions (
    id uuid PRIMARY KEY,
    rule_id text NOT NULL CHECK (
        char_length(rule_id) BETWEEN 1 AND 128 AND rule_id = btrim(rule_id)
    ),
    rule_version bigint NOT NULL CHECK (rule_version > 0),
    enabled boolean NOT NULL,
    download_threshold_bytes bigint NOT NULL CHECK (
        download_threshold_bytes BETWEEN 0 AND 9000000000000000000
    ),
    minimum_ratio_basis_points bigint NOT NULL CHECK (
        minimum_ratio_basis_points BETWEEN 0 AND 1000000
    ),
    watch_period_seconds bigint NOT NULL CHECK (
        watch_period_seconds BETWEEN 0 AND 31536000
    ),
    restriction_ratio_basis_points bigint NOT NULL CHECK (
        restriction_ratio_basis_points BETWEEN 0 AND 1000000
    ),
    vip_exempt boolean NOT NULL DEFAULT true CHECK (vip_exempt),
    effective_at timestamptz NOT NULL,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    actor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid NOT NULL,
    command_sha256 bytea NOT NULL CHECK (octet_length(command_sha256) = 32),
    created_at timestamptz NOT NULL,
    UNIQUE (rule_id, rule_version),
    UNIQUE (effective_at),
    CHECK (effective_at >= created_at + interval '5 minutes'),
    CHECK (effective_at <= created_at + interval '365 days'),
    CHECK (
        (NOT enabled
            AND download_threshold_bytes = 0
            AND minimum_ratio_basis_points = 0
            AND watch_period_seconds = 0
            AND restriction_ratio_basis_points = 0)
        OR
        (enabled
            AND download_threshold_bytes >= 1073741824
            AND minimum_ratio_basis_points BETWEEN 1 AND 1000000
            AND watch_period_seconds BETWEEN 86400 AND 31536000
            AND restriction_ratio_basis_points BETWEEN 1 AND minimum_ratio_basis_points)
    )
);

CREATE INDEX ratio_watch_policy_timeline_idx
    ON ratio_watch.policy_revisions (effective_at DESC, id DESC);

CREATE TABLE ratio_watch.assessments (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    policy_revision_id uuid NOT NULL
        REFERENCES ratio_watch.policy_revisions (id) ON DELETE RESTRICT,
    status text NOT NULL CHECK (status IN (
        'watching', 'warning', 'download_restricted',
        'satisfied', 'manually_cleared', 'vip_exempted', 'ineligible'
    )),
    started_at timestamptz NOT NULL,
    deadline_at timestamptz NOT NULL,
    opening_credited_uploaded bigint NOT NULL CHECK (opening_credited_uploaded >= 0),
    opening_charged_downloaded bigint NOT NULL CHECK (opening_charged_downloaded > 0),
    opening_ratio_basis_points bigint NOT NULL CHECK (
        opening_ratio_basis_points BETWEEN 0 AND 1000000
    ),
    current_credited_uploaded bigint NOT NULL CHECK (current_credited_uploaded >= 0),
    current_charged_downloaded bigint NOT NULL CHECK (current_charged_downloaded > 0),
    current_ratio_basis_points bigint NOT NULL CHECK (
        current_ratio_basis_points BETWEEN 0 AND 1000000
    ),
    restriction_started_at timestamptz,
    resolved_at timestamptz,
    resolution_code text CHECK (
        resolution_code IS NULL OR resolution_code IN (
            'ratio_recovered', 'staff_cleared', 'vip_became_active', 'account_ineligible'
        )
    ),
    resolution_reason text CHECK (
        resolution_reason IS NULL OR char_length(btrim(resolution_reason)) BETWEEN 10 AND 1000
    ),
    resolved_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    resolution_authorization_decision_id uuid,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL,
    UNIQUE (user_id, id),
    CHECK (deadline_at > started_at),
    CHECK (
        (status IN ('watching', 'warning')
            AND restriction_started_at IS NULL
            AND resolved_at IS NULL
            AND resolution_code IS NULL
            AND resolution_reason IS NULL
            AND resolved_by IS NULL
            AND resolution_authorization_decision_id IS NULL)
        OR
        (status = 'download_restricted'
            AND restriction_started_at IS NOT NULL
            AND resolved_at IS NULL
            AND resolution_code IS NULL
            AND resolution_reason IS NULL
            AND resolved_by IS NULL
            AND resolution_authorization_decision_id IS NULL)
        OR
        (status IN ('satisfied', 'vip_exempted', 'ineligible')
            AND resolved_at IS NOT NULL
            AND resolution_code IS NOT NULL
            AND resolution_reason IS NULL
            AND resolved_by IS NULL
            AND resolution_authorization_decision_id IS NULL)
        OR
        (status = 'manually_cleared'
            AND resolved_at IS NOT NULL
            AND resolution_code = 'staff_cleared'
            AND resolution_reason IS NOT NULL
            AND resolved_by IS NOT NULL
            AND resolution_authorization_decision_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX ratio_watch_one_active_assessment_per_user_idx
    ON ratio_watch.assessments (user_id)
    WHERE status IN ('watching', 'warning', 'download_restricted');

CREATE INDEX ratio_watch_assessments_status_deadline_idx
    ON ratio_watch.assessments (status, deadline_at, id);

CREATE INDEX ratio_watch_assessments_user_history_idx
    ON ratio_watch.assessments (user_id, started_at DESC, id DESC);

-- A member manually cleared from one policy revision is not immediately put
-- back into the same rule on the next worker tick. A new revision may assess
-- them again, which keeps staff exceptions explicit and naturally bounded.
CREATE UNIQUE INDEX ratio_watch_manual_clear_per_revision_idx
    ON ratio_watch.assessments (user_id, policy_revision_id)
    WHERE status = 'manually_cleared';

-- Every state change is append-only and contains structured counters. No
-- worker or report needs to parse a human sentence to recover the opening
-- ratio, unlike the legacy implementation.
CREATE TABLE ratio_watch.assessment_transitions (
    sequence bigserial PRIMARY KEY,
    assessment_id uuid NOT NULL
        REFERENCES ratio_watch.assessments (id) ON DELETE RESTRICT,
    from_status text CHECK (from_status IS NULL OR from_status IN (
        'watching', 'warning', 'download_restricted',
        'satisfied', 'manually_cleared', 'vip_exempted', 'ineligible'
    )),
    to_status text NOT NULL CHECK (to_status IN (
        'watching', 'warning', 'download_restricted',
        'satisfied', 'manually_cleared', 'vip_exempted', 'ineligible'
    )),
    credited_uploaded bigint NOT NULL CHECK (credited_uploaded >= 0),
    charged_downloaded bigint NOT NULL CHECK (charged_downloaded > 0),
    ratio_basis_points bigint NOT NULL CHECK (ratio_basis_points BETWEEN 0 AND 1000000),
    reason_code text NOT NULL CHECK (reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    reason text CHECK (
        reason IS NULL OR char_length(btrim(reason)) BETWEEN 10 AND 1000
    ),
    actor_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid,
    occurred_at timestamptz NOT NULL,
    CHECK ((actor_id IS NULL) = (authorization_decision_id IS NULL)),
    CHECK ((reason IS NOT NULL) = (actor_id IS NOT NULL))
);

CREATE INDEX ratio_watch_transitions_assessment_idx
    ON ratio_watch.assessment_transitions (assessment_id, sequence);

CREATE TABLE ratio_watch.worker_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    last_started_at timestamptz,
    last_completed_at timestamptz,
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    last_examined bigint NOT NULL DEFAULT 0 CHECK (last_examined >= 0),
    last_created bigint NOT NULL DEFAULT 0 CHECK (last_created >= 0),
    last_transitioned bigint NOT NULL DEFAULT 0 CHECK (last_transitioned >= 0),
    run_count bigint NOT NULL DEFAULT 0 CHECK (run_count >= 0)
);

INSERT INTO ratio_watch.worker_state (singleton) VALUES (true);

-- +goose StatementBegin
CREATE FUNCTION ratio_watch.validate_assessment_opening()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    policy_watch_seconds bigint;
BEGIN
    SELECT revision.watch_period_seconds
    INTO STRICT policy_watch_seconds
    FROM ratio_watch.policy_revisions AS revision
    WHERE revision.id = NEW.policy_revision_id
      AND revision.enabled;

    IF NEW.deadline_at <> NEW.started_at
        + make_interval(secs => policy_watch_seconds::double precision) THEN
        RAISE EXCEPTION 'ratio assessment deadline does not match its policy revision';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER ratio_watch_assessment_opening_valid
BEFORE INSERT ON ratio_watch.assessments
FOR EACH ROW EXECUTE FUNCTION ratio_watch.validate_assessment_opening();

-- +goose StatementBegin
CREATE FUNCTION ratio_watch.reject_policy_revision_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'ratio policy revisions are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER ratio_watch_policy_revisions_immutable
BEFORE UPDATE OR DELETE ON ratio_watch.policy_revisions
FOR EACH ROW EXECUTE FUNCTION ratio_watch.reject_policy_revision_mutation();

-- +goose StatementBegin
CREATE FUNCTION ratio_watch.protect_assessment_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'ratio assessments cannot be deleted';
    END IF;
    IF NEW.id <> OLD.id
       OR NEW.user_id <> OLD.user_id
       OR NEW.policy_revision_id <> OLD.policy_revision_id
       OR NEW.started_at <> OLD.started_at
       OR NEW.deadline_at <> OLD.deadline_at
       OR NEW.opening_credited_uploaded <> OLD.opening_credited_uploaded
       OR NEW.opening_charged_downloaded <> OLD.opening_charged_downloaded
       OR NEW.opening_ratio_basis_points <> OLD.opening_ratio_basis_points THEN
        RAISE EXCEPTION 'ratio assessment opening evidence is immutable';
    END IF;
    IF OLD.status IN ('satisfied', 'manually_cleared', 'vip_exempted', 'ineligible') THEN
        RAISE EXCEPTION 'resolved ratio assessment is immutable';
    END IF;
    IF NEW.version <> OLD.version + 1
       OR NEW.updated_at < OLD.updated_at
       OR NEW.current_credited_uploaded < OLD.current_credited_uploaded
       OR NEW.current_charged_downloaded < OLD.current_charged_downloaded THEN
        RAISE EXCEPTION 'ratio assessment projection must advance monotonically';
    END IF;
    IF NOT (
        (OLD.status = 'watching' AND NEW.status IN (
            'watching', 'warning', 'download_restricted',
            'satisfied', 'manually_cleared', 'vip_exempted', 'ineligible'
        ))
        OR (OLD.status = 'warning' AND NEW.status IN (
            'warning', 'download_restricted',
            'satisfied', 'manually_cleared', 'vip_exempted', 'ineligible'
        ))
        OR (OLD.status = 'download_restricted' AND NEW.status IN (
            'download_restricted', 'satisfied',
            'manually_cleared', 'vip_exempted', 'ineligible'
        ))
    ) THEN
        RAISE EXCEPTION 'invalid ratio assessment state transition';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER ratio_watch_assessment_transition_guard
BEFORE UPDATE OR DELETE ON ratio_watch.assessments
FOR EACH ROW EXECUTE FUNCTION ratio_watch.protect_assessment_transition();

-- +goose StatementBegin
CREATE FUNCTION ratio_watch.reject_transition_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'ratio assessment transitions are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER ratio_watch_transitions_immutable
BEFORE UPDATE OR DELETE ON ratio_watch.assessment_transitions
FOR EACH ROW EXECUTE FUNCTION ratio_watch.reject_transition_mutation();

-- This is the single read expression for all download gates. The legacy/manual
-- opening flag and the policy-owned assessment remain separate sources so an
-- automatic recovery can never erase a restriction imported from PtYes.
-- +goose StatementBegin
CREATE FUNCTION identity.is_download_restricted(subject_id uuid)
RETURNS boolean
LANGUAGE sql
STABLE
AS $$
    SELECT
        COALESCE((
            SELECT access.download_restricted
            FROM identity.user_access_states AS access
            WHERE access.user_id = subject_id
        ), false)
        OR EXISTS (
            SELECT 1
            FROM ratio_watch.assessments AS assessment
            WHERE assessment.user_id = subject_id
              AND assessment.status = 'download_restricted'
              AND assessment.resolved_at IS NULL
        );
$$;
-- +goose StatementEnd

-- +goose Down

DROP FUNCTION identity.is_download_restricted(uuid);
DROP TRIGGER ratio_watch_assessment_opening_valid ON ratio_watch.assessments;
DROP FUNCTION ratio_watch.validate_assessment_opening();
DROP TRIGGER ratio_watch_transitions_immutable ON ratio_watch.assessment_transitions;
DROP FUNCTION ratio_watch.reject_transition_mutation();
DROP TRIGGER ratio_watch_assessment_transition_guard ON ratio_watch.assessments;
DROP FUNCTION ratio_watch.protect_assessment_transition();
DROP TRIGGER ratio_watch_policy_revisions_immutable ON ratio_watch.policy_revisions;
DROP FUNCTION ratio_watch.reject_policy_revision_mutation();
DROP TABLE ratio_watch.worker_state;
DROP INDEX ratio_watch.ratio_watch_transitions_assessment_idx;
DROP TABLE ratio_watch.assessment_transitions;
DROP INDEX ratio_watch.ratio_watch_manual_clear_per_revision_idx;
DROP INDEX ratio_watch.ratio_watch_assessments_user_history_idx;
DROP INDEX ratio_watch.ratio_watch_assessments_status_deadline_idx;
DROP INDEX ratio_watch.ratio_watch_one_active_assessment_per_user_idx;
DROP TABLE ratio_watch.assessments;
DROP INDEX ratio_watch.ratio_watch_policy_timeline_idx;
DROP TABLE ratio_watch.policy_revisions;
DROP SCHEMA ratio_watch;

DELETE FROM authz.role_permissions
WHERE role_id = 'site_admin'
  AND action IN ('ratio.assessment.manage', 'ratio.policy.issue', 'ratio.policy.read');

DELETE FROM authz.permissions
WHERE action IN ('ratio.assessment.manage', 'ratio.policy.issue', 'ratio.policy.read');
