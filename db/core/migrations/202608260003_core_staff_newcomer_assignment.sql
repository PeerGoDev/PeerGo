-- +goose Up

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES (
    'newcomer.assessment.assign', '为现有用户分配新人考核',
    'high', 'none', 'staff-session', true, true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('site_admin', 'newcomer.assessment.assign');

CREATE TABLE newcomer.assessment_assignments (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL UNIQUE
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    policy_revision_id uuid NOT NULL
        REFERENCES newcomer.policy_revisions (id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (
        reason = btrim(reason) AND char_length(reason) BETWEEN 10 AND 1000
    ),
    actor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid NOT NULL,
    occurred_at timestamptz NOT NULL
);

CREATE TRIGGER newcomer_assignments_immutable
BEFORE UPDATE OR DELETE ON newcomer.assessment_assignments
FOR EACH ROW EXECUTE FUNCTION newcomer.reject_immutable_mutation();

ALTER TABLE newcomer.assessments
    ALTER COLUMN registration_id DROP NOT NULL,
    ADD COLUMN assignment_id uuid UNIQUE
        REFERENCES newcomer.assessment_assignments (id) ON DELETE RESTRICT,
    ADD CONSTRAINT newcomer_assessment_single_opening_source CHECK (
        (registration_id IS NOT NULL AND assignment_id IS NULL)
        OR (registration_id IS NULL AND assignment_id IS NOT NULL)
    );

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION newcomer.validate_assessment_opening()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    expected_duration bigint;
    source_user uuid;
    source_policy uuid;
    source_started_at timestamptz;
BEGIN
    SELECT revision.duration_seconds
    INTO STRICT expected_duration
    FROM newcomer.policy_revisions AS revision
    WHERE revision.id = NEW.policy_revision_id
      AND revision.enabled;

    IF NEW.registration_id IS NOT NULL THEN
        SELECT registration.user_id, NEW.policy_revision_id, registration.completed_at
        INTO STRICT source_user, source_policy, source_started_at
        FROM identity.registrations AS registration
        WHERE registration.id = NEW.registration_id
          AND registration.state = 'completed';
    ELSE
        SELECT assignment.user_id, assignment.policy_revision_id, assignment.occurred_at
        INTO STRICT source_user, source_policy, source_started_at
        FROM newcomer.assessment_assignments AS assignment
        WHERE assignment.id = NEW.assignment_id;
    END IF;

    IF source_user <> NEW.user_id
       OR source_policy <> NEW.policy_revision_id
       OR source_started_at <> NEW.started_at
       OR NEW.deadline_at <> NEW.started_at
            + make_interval(secs => expected_duration::double precision) THEN
        RAISE EXCEPTION 'newcomer assessment opening does not match source and policy';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION newcomer.protect_assessment_projection()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'newcomer assessments cannot be deleted';
    END IF;
    IF NEW.id <> OLD.id
       OR NEW.registration_id IS DISTINCT FROM OLD.registration_id
       OR NEW.assignment_id IS DISTINCT FROM OLD.assignment_id
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

-- +goose Down

DROP TRIGGER newcomer_assessment_projection_protected ON newcomer.assessments;
DROP FUNCTION newcomer.protect_assessment_projection();
DROP TRIGGER newcomer_assessment_opening_valid ON newcomer.assessments;
DROP FUNCTION newcomer.validate_assessment_opening();

DROP TRIGGER newcomer_transitions_immutable ON newcomer.assessment_transitions;
DROP TRIGGER newcomer_exemptions_immutable ON newcomer.assessment_exemptions;

DELETE FROM newcomer.assessment_transitions
WHERE assessment_id IN (
    SELECT id FROM newcomer.assessments WHERE assignment_id IS NOT NULL
);
DELETE FROM newcomer.assessment_exemptions
WHERE assessment_id IN (
    SELECT id FROM newcomer.assessments WHERE assignment_id IS NOT NULL
);
DELETE FROM newcomer.assessments WHERE assignment_id IS NOT NULL;

CREATE TRIGGER newcomer_transitions_immutable
BEFORE UPDATE OR DELETE ON newcomer.assessment_transitions
FOR EACH ROW EXECUTE FUNCTION newcomer.reject_immutable_mutation();
CREATE TRIGGER newcomer_exemptions_immutable
BEFORE UPDATE OR DELETE ON newcomer.assessment_exemptions
FOR EACH ROW EXECUTE FUNCTION newcomer.reject_immutable_mutation();

ALTER TABLE newcomer.assessments
    DROP CONSTRAINT newcomer_assessment_single_opening_source,
    DROP COLUMN assignment_id,
    ALTER COLUMN registration_id SET NOT NULL;

DROP TRIGGER newcomer_assignments_immutable ON newcomer.assessment_assignments;
DROP TABLE newcomer.assessment_assignments;

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
    IF NEW.id <> OLD.id OR NEW.registration_id <> OLD.registration_id
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
    IF NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at
       OR NEW.current_credited_upload_bytes < OLD.current_credited_upload_bytes
       OR NEW.current_seeding_active_seconds < OLD.current_seeding_active_seconds THEN
        RAISE EXCEPTION 'newcomer assessment progress must advance monotonically';
    END IF;
    IF NOT ((OLD.status = 'active' AND NEW.status IN ('active', 'download_restricted', 'passed', 'exempted'))
        OR (OLD.status = 'download_restricted' AND NEW.status IN ('download_restricted', 'passed', 'exempted'))) THEN
        RAISE EXCEPTION 'invalid newcomer assessment transition';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER newcomer_assessment_projection_protected
BEFORE UPDATE OR DELETE ON newcomer.assessments
FOR EACH ROW EXECUTE FUNCTION newcomer.protect_assessment_projection();

DELETE FROM authz.role_permissions WHERE action = 'newcomer.assessment.assign';
DELETE FROM authz.permissions WHERE action = 'newcomer.assessment.assign';
