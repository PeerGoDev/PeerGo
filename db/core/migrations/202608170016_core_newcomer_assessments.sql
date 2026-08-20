-- +goose Up

-- Newcomer assessment is intentionally narrower than the generic PtYes exam
-- model. It applies only to native registrations completed while an enabled
-- revision is effective, and it can only restrict new downloads. Migrated
-- users have no identity.registrations completion transition and are never
-- enrolled retroactively.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('newcomer.assessment.exempt', '人工豁免一条新人考核', 'high', 'none', 'staff-session', true, true),
    ('newcomer.assessment.read', '读取新人考核名单和进度', 'medium', 'none', 'staff-session', true, true),
    ('newcomer.assessment.read.self', '读取自己的新人考核进度', 'low', 'self', 'web-session', true, true),
    ('newcomer.policy.issue', '签发未来生效的新人考核规则', 'high', 'none', 'staff-session', true, true),
    ('newcomer.policy.read', '读取新人考核规则和运行状态', 'medium', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'newcomer.assessment.read.self'),
    ('site_admin', 'newcomer.assessment.exempt'),
    ('site_admin', 'newcomer.assessment.read'),
    ('site_admin', 'newcomer.policy.issue'),
    ('site_admin', 'newcomer.policy.read');

CREATE SCHEMA newcomer;

-- Revisions are append-only. An assessment keeps the exact revision that was
-- effective at registration completion, so later setting changes cannot move
-- its deadline or rewrite its targets.
CREATE TABLE newcomer.policy_revisions (
    id uuid PRIMARY KEY,
    request_id uuid UNIQUE,
    revision bigint NOT NULL UNIQUE CHECK (revision > 0),
    source_kind text NOT NULL CHECK (source_kind IN ('opening', 'staff')),
    enabled boolean NOT NULL,
    duration_seconds bigint NOT NULL CHECK (
        duration_seconds BETWEEN 604800 AND 7776000
    ),
    minimum_credited_upload_bytes bigint NOT NULL CHECK (
        minimum_credited_upload_bytes BETWEEN 0 AND 9000000000000000000
    ),
    minimum_seeding_active_seconds bigint NOT NULL CHECK (
        minimum_seeding_active_seconds BETWEEN 0 AND 315360000
    ),
    effective_at timestamptz NOT NULL,
    reason text NOT NULL CHECK (
        reason = btrim(reason) AND char_length(reason) BETWEEN 10 AND 1000
    ),
    actor_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid,
    created_at timestamptz NOT NULL,
    CHECK (enabled OR (
        minimum_credited_upload_bytes = 0
        AND minimum_seeding_active_seconds = 0
    )),
    CHECK (NOT enabled OR (
        minimum_credited_upload_bytes > 0
        OR minimum_seeding_active_seconds > 0
    )),
    CHECK (
        (source_kind = 'opening'
            AND request_id IS NULL
            AND actor_id IS NULL
            AND authorization_decision_id IS NULL)
        OR
        (source_kind = 'staff'
            AND request_id IS NOT NULL
            AND actor_id IS NOT NULL
            AND authorization_decision_id IS NOT NULL
            AND effective_at >= created_at + interval '5 minutes'
            AND effective_at <= created_at + interval '365 days')
    )
);

CREATE INDEX newcomer_policy_timeline_idx
    ON newcomer.policy_revisions (effective_at DESC, revision DESC);

INSERT INTO newcomer.policy_revisions (
    id, revision, source_kind, enabled, duration_seconds,
    minimum_credited_upload_bytes, minimum_seeding_active_seconds,
    effective_at, reason, created_at
) VALUES (
    gen_random_uuid(), 1, 'opening', false, 2592000,
    0, 0, '2000-01-01T00:00:00Z', '首次启用前保持关闭，避免迁移用户或现有用户被追溯考核。', now()
);

CREATE TABLE newcomer.assessments (
    id uuid PRIMARY KEY,
    registration_id uuid NOT NULL UNIQUE
        REFERENCES identity.registrations (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL UNIQUE
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    policy_revision_id uuid NOT NULL
        REFERENCES newcomer.policy_revisions (id) ON DELETE RESTRICT,
    status text NOT NULL CHECK (status IN (
        'active', 'download_restricted', 'passed', 'exempted'
    )),
    started_at timestamptz NOT NULL,
    deadline_at timestamptz NOT NULL,
    opening_credited_uploaded_bytes bigint NOT NULL CHECK (
        opening_credited_uploaded_bytes >= 0
    ),
    current_credited_upload_bytes bigint NOT NULL DEFAULT 0 CHECK (
        current_credited_upload_bytes >= 0
    ),
    current_seeding_active_seconds bigint NOT NULL DEFAULT 0 CHECK (
        current_seeding_active_seconds >= 0
    ),
    restriction_started_at timestamptz,
    resolved_at timestamptz,
    resolution_code text CHECK (
        resolution_code IS NULL OR resolution_code IN (
            'requirements_met', 'staff_exempted'
        )
    ),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at timestamptz NOT NULL,
    UNIQUE (user_id, id),
    CHECK (deadline_at > started_at),
    CHECK (
        (status = 'active'
            AND restriction_started_at IS NULL
            AND resolved_at IS NULL
            AND resolution_code IS NULL)
        OR
        (status = 'download_restricted'
            AND restriction_started_at IS NOT NULL
            AND resolved_at IS NULL
            AND resolution_code IS NULL)
        OR
        (status = 'passed'
            AND resolved_at IS NOT NULL
            AND resolution_code = 'requirements_met')
        OR
        (status = 'exempted'
            AND resolved_at IS NOT NULL
            AND resolution_code = 'staff_exempted')
    )
);

CREATE INDEX newcomer_assessments_status_deadline_idx
    ON newcomer.assessments (status, deadline_at, id);

CREATE TABLE newcomer.assessment_transitions (
    sequence bigserial PRIMARY KEY,
    assessment_id uuid NOT NULL
        REFERENCES newcomer.assessments (id) ON DELETE RESTRICT,
    from_status text CHECK (from_status IS NULL OR from_status IN (
        'active', 'download_restricted', 'passed', 'exempted'
    )),
    to_status text NOT NULL CHECK (to_status IN (
        'active', 'download_restricted', 'passed', 'exempted'
    )),
    credited_upload_bytes bigint NOT NULL CHECK (credited_upload_bytes >= 0),
    seeding_active_seconds bigint NOT NULL CHECK (seeding_active_seconds >= 0),
    reason_code text NOT NULL CHECK (reason_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    occurred_at timestamptz NOT NULL
);

CREATE INDEX newcomer_transitions_assessment_idx
    ON newcomer.assessment_transitions (assessment_id, sequence);

-- The immutable exemption fact keeps the staff reason and authorization
-- evidence out of the mutable projection while providing retry-safe commands.
CREATE TABLE newcomer.assessment_exemptions (
    id uuid PRIMARY KEY,
    assessment_id uuid NOT NULL UNIQUE
        REFERENCES newcomer.assessments (id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (
        reason = btrim(reason) AND char_length(reason) BETWEEN 10 AND 1000
    ),
    actor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL
);

CREATE TABLE newcomer.worker_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    last_started_at timestamptz,
    last_completed_at timestamptz,
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    last_examined bigint NOT NULL DEFAULT 0 CHECK (last_examined >= 0),
    last_transitioned bigint NOT NULL DEFAULT 0 CHECK (last_transitioned >= 0),
    run_count bigint NOT NULL DEFAULT 0 CHECK (run_count >= 0)
);

INSERT INTO newcomer.worker_state (singleton) VALUES (true);

-- +goose StatementBegin
CREATE FUNCTION newcomer.reject_immutable_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'newcomer policy and evidence facts are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER newcomer_policy_revisions_immutable
BEFORE UPDATE OR DELETE ON newcomer.policy_revisions
FOR EACH ROW EXECUTE FUNCTION newcomer.reject_immutable_mutation();

CREATE TRIGGER newcomer_transitions_immutable
BEFORE UPDATE OR DELETE ON newcomer.assessment_transitions
FOR EACH ROW EXECUTE FUNCTION newcomer.reject_immutable_mutation();

CREATE TRIGGER newcomer_exemptions_immutable
BEFORE UPDATE OR DELETE ON newcomer.assessment_exemptions
FOR EACH ROW EXECUTE FUNCTION newcomer.reject_immutable_mutation();

-- +goose StatementBegin
CREATE FUNCTION newcomer.validate_assessment_opening()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    expected_duration bigint;
    registration_user uuid;
    registration_completed_at timestamptz;
BEGIN
    SELECT revision.duration_seconds
    INTO STRICT expected_duration
    FROM newcomer.policy_revisions AS revision
    WHERE revision.id = NEW.policy_revision_id
      AND revision.enabled;

    SELECT registration.user_id, registration.completed_at
    INTO STRICT registration_user, registration_completed_at
    FROM identity.registrations AS registration
    WHERE registration.id = NEW.registration_id
      AND registration.state = 'completed';

    IF registration_user <> NEW.user_id
       OR registration_completed_at <> NEW.started_at
       OR NEW.deadline_at <> NEW.started_at
            + make_interval(secs => expected_duration::double precision) THEN
        RAISE EXCEPTION 'newcomer assessment opening does not match registration and policy';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER newcomer_assessment_opening_valid
BEFORE INSERT ON newcomer.assessments
FOR EACH ROW EXECUTE FUNCTION newcomer.validate_assessment_opening();

-- +goose StatementBegin
CREATE FUNCTION newcomer.protect_assessment_projection()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'newcomer assessments cannot be deleted';
    END IF;
    IF NEW.id <> OLD.id
       OR NEW.registration_id <> OLD.registration_id
       OR NEW.user_id <> OLD.user_id
       OR NEW.policy_revision_id <> OLD.policy_revision_id
       OR NEW.started_at <> OLD.started_at
       OR NEW.deadline_at <> OLD.deadline_at
       OR NEW.opening_credited_uploaded_bytes <> OLD.opening_credited_uploaded_bytes THEN
        RAISE EXCEPTION 'newcomer assessment opening is immutable';
    END IF;
    IF OLD.status IN ('passed', 'exempted') THEN
        RAISE EXCEPTION 'resolved newcomer assessment is immutable';
    END IF;
    IF NEW.version <> OLD.version + 1
       OR NEW.updated_at < OLD.updated_at
       OR NEW.current_credited_upload_bytes < OLD.current_credited_upload_bytes
       OR NEW.current_seeding_active_seconds < OLD.current_seeding_active_seconds THEN
        RAISE EXCEPTION 'newcomer assessment progress must advance monotonically';
    END IF;
    IF NOT (
        (OLD.status = 'active' AND NEW.status IN (
            'active', 'download_restricted', 'passed', 'exempted'
        ))
        OR
        (OLD.status = 'download_restricted' AND NEW.status IN (
            'download_restricted', 'passed', 'exempted'
        ))
    ) THEN
        RAISE EXCEPTION 'invalid newcomer assessment transition';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER newcomer_assessment_projection_protected
BEFORE UPDATE OR DELETE ON newcomer.assessments
FOR EACH ROW EXECUTE FUNCTION newcomer.protect_assessment_projection();

-- Enrollment lives on the native registration completion boundary. The
-- trigger inserts no historical rows and therefore cannot capture migrated
-- users or registrations that were completed before this migration existed.
-- +goose StatementBegin
CREATE FUNCTION newcomer.enroll_completed_registration()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    policy newcomer.policy_revisions%ROWTYPE;
    assessment_id uuid;
    opening_upload bigint;
BEGIN
    IF OLD.state = 'completed' OR NEW.state <> 'completed' THEN
        RETURN NEW;
    END IF;

    SELECT revision.*
    INTO policy
    FROM newcomer.policy_revisions AS revision
    WHERE revision.effective_at <= NEW.completed_at
    ORDER BY revision.effective_at DESC, revision.revision DESC
    LIMIT 1;

    IF policy.id IS NULL OR NOT policy.enabled THEN
        RETURN NEW;
    END IF;

    SELECT COALESCE(totals.credited_uploaded, 0)
    INTO opening_upload
    FROM identity.users AS users
    LEFT JOIN traffic.user_totals AS totals ON totals.user_id = users.id
    WHERE users.id = NEW.user_id;

    assessment_id := gen_random_uuid();
    INSERT INTO newcomer.assessments (
        id, registration_id, user_id, policy_revision_id, status,
        started_at, deadline_at, opening_credited_uploaded_bytes,
        current_credited_upload_bytes, current_seeding_active_seconds,
        version, updated_at
    ) VALUES (
        assessment_id, NEW.id, NEW.user_id, policy.id, 'active',
        NEW.completed_at,
        NEW.completed_at + make_interval(secs => policy.duration_seconds::double precision),
        opening_upload, 0, 0, 1, NEW.completed_at
    );

    INSERT INTO newcomer.assessment_transitions (
        assessment_id, from_status, to_status, credited_upload_bytes,
        seeding_active_seconds, reason_code, occurred_at
    ) VALUES (
        assessment_id, NULL, 'active', 0, 0, 'registration_completed', NEW.completed_at
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER newcomer_registration_completed
AFTER UPDATE OF state ON identity.registrations
FOR EACH ROW EXECUTE FUNCTION newcomer.enroll_completed_registration();

-- Every download path and the Tracker subject snapshot use this one predicate.
-- A passed or exempted assessment stops contributing immediately without
-- clearing manual, ratio-watch or H&R restrictions owned by other domains.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION identity.is_download_restricted(subject_id uuid)
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
        )
        OR EXISTS (
            SELECT 1
            FROM newcomer.assessments AS assessment
            WHERE assessment.user_id = subject_id
              AND assessment.status = 'download_restricted'
              AND assessment.resolved_at IS NULL
        )
        OR EXISTS (
            SELECT 1
            FROM traffic.user_hnr_obligations AS obligation
            WHERE obligation.user_id = subject_id
              AND obligation.state = 'tracking'
              AND obligation.grace_ends_at <= CURRENT_TIMESTAMP
              AND NOT EXISTS (
                  SELECT 1
                  FROM traffic.hnr_appeal_exemptions AS exemption
                  WHERE exemption.obligation_id = obligation.obligation_id
              )
        );
$$;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION identity.is_download_restricted(subject_id uuid)
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
        )
        OR EXISTS (
            SELECT 1
            FROM traffic.user_hnr_obligations AS obligation
            WHERE obligation.user_id = subject_id
              AND obligation.state = 'tracking'
              AND obligation.grace_ends_at <= CURRENT_TIMESTAMP
              AND NOT EXISTS (
                  SELECT 1
                  FROM traffic.hnr_appeal_exemptions AS exemption
                  WHERE exemption.obligation_id = obligation.obligation_id
              )
        );
$$;
-- +goose StatementEnd

DROP TRIGGER newcomer_registration_completed ON identity.registrations;
DROP FUNCTION newcomer.enroll_completed_registration();
DROP TRIGGER newcomer_assessment_projection_protected ON newcomer.assessments;
DROP FUNCTION newcomer.protect_assessment_projection();
DROP TRIGGER newcomer_assessment_opening_valid ON newcomer.assessments;
DROP FUNCTION newcomer.validate_assessment_opening();
DROP TRIGGER newcomer_exemptions_immutable ON newcomer.assessment_exemptions;
DROP TRIGGER newcomer_transitions_immutable ON newcomer.assessment_transitions;
DROP TRIGGER newcomer_policy_revisions_immutable ON newcomer.policy_revisions;
DROP FUNCTION newcomer.reject_immutable_mutation();
DROP TABLE newcomer.worker_state;
DROP TABLE newcomer.assessment_exemptions;
DROP INDEX newcomer.newcomer_transitions_assessment_idx;
DROP TABLE newcomer.assessment_transitions;
DROP INDEX newcomer.newcomer_assessments_status_deadline_idx;
DROP TABLE newcomer.assessments;
DROP INDEX newcomer.newcomer_policy_timeline_idx;
DROP TABLE newcomer.policy_revisions;
DROP SCHEMA newcomer;

DELETE FROM authz.role_permissions
WHERE action IN (
    'newcomer.assessment.exempt', 'newcomer.assessment.read',
    'newcomer.assessment.read.self', 'newcomer.policy.issue',
    'newcomer.policy.read'
);

DELETE FROM authz.permissions
WHERE action IN (
    'newcomer.assessment.exempt', 'newcomer.assessment.read',
    'newcomer.assessment.read.self', 'newcomer.policy.issue',
    'newcomer.policy.read'
);
