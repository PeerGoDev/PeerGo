-- +goose Up

-- VIP download charging is an accounting input. Its canonical Settlement
-- command is committed beside the immutable VIP transition and retried through
-- an outbox; the browser-facing request never writes Settlement directly.
CREATE TABLE identity.settlement_vip_benefit_outbox (
    transition_id uuid PRIMARY KEY
        REFERENCES identity.user_vip_transitions (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    state_version bigint NOT NULL CHECK (state_version > 0),
    effective_at timestamptz NOT NULL,
    command_json text NOT NULL CHECK (
        octet_length(command_json) BETWEEN 2 AND 2048
        AND jsonb_typeof(command_json::jsonb) = 'object'
    ),
    command_sha256 bytea NOT NULL CHECK (octet_length(command_sha256) = 32),
    available_at timestamptz NOT NULL,
    lease_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    delivered_at timestamptz,
    created_at timestamptz NOT NULL,
    UNIQUE (user_id, state_version),
    CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
    CHECK (delivered_at IS NULL OR (lease_token IS NULL AND last_error_code IS NULL))
);

CREATE INDEX settlement_vip_benefit_outbox_ready_idx
    ON identity.settlement_vip_benefit_outbox
        (available_at, state_version, transition_id)
    WHERE delivered_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION identity.protect_settlement_vip_benefit_outbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'VIP benefit outbox rows cannot be deleted';
    END IF;
    IF OLD.transition_id IS DISTINCT FROM NEW.transition_id
        OR OLD.user_id IS DISTINCT FROM NEW.user_id
        OR OLD.state_version IS DISTINCT FROM NEW.state_version
        OR OLD.effective_at IS DISTINCT FROM NEW.effective_at
        OR OLD.command_json IS DISTINCT FROM NEW.command_json
        OR OLD.command_sha256 IS DISTINCT FROM NEW.command_sha256
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'VIP benefit outbox payload is immutable';
    END IF;
    IF OLD.delivered_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'delivered VIP benefit command is terminal';
    END IF;
    IF NEW.attempts < OLD.attempts THEN
        RAISE EXCEPTION 'VIP benefit attempts cannot regress';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER settlement_vip_benefit_outbox_protected
BEFORE UPDATE OR DELETE ON identity.settlement_vip_benefit_outbox
FOR EACH ROW EXECUTE FUNCTION identity.protect_settlement_vip_benefit_outbox();

REVOKE ALL ON identity.settlement_vip_benefit_outbox FROM PUBLIC;

-- PtYes exempts active VIP members from the newcomer exam, not from H&R.
-- Preserve that distinction and retain the source VIP transition in immutable
-- assessment history when an already-enrolled member receives VIP.
ALTER TABLE newcomer.assessments
    DROP CONSTRAINT assessments_resolution_code_check;
ALTER TABLE newcomer.assessments
    ADD CONSTRAINT assessments_resolution_code_check CHECK (
        resolution_code IS NULL OR resolution_code IN (
            'requirements_met', 'staff_exempted', 'vip_exempted'
        )
    );
ALTER TABLE newcomer.assessments
    DROP CONSTRAINT assessments_check1;
ALTER TABLE newcomer.assessments
    ADD CONSTRAINT assessments_state_resolution_check CHECK (
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
            AND resolution_code IN ('staff_exempted', 'vip_exempted'))
    );

ALTER TABLE newcomer.assessment_transitions
    ADD COLUMN source_vip_transition_id uuid
        REFERENCES identity.user_vip_transitions (id) ON DELETE RESTRICT;
ALTER TABLE newcomer.assessment_transitions
    ADD CONSTRAINT assessment_transition_vip_source_check CHECK (
        (reason_code = 'vip_exempted') = (source_vip_transition_id IS NOT NULL)
    );

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION newcomer.enroll_completed_registration()
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

    -- A VIP active when registration completes is exempt from assignment.
    -- Later expiry does not retroactively create an assessment, matching the
    -- old site's one-time registration boundary.
    IF EXISTS (
        SELECT 1
        FROM identity.user_access_states AS access
        WHERE access.user_id = NEW.user_id
          AND access.vip_enabled
          AND (access.vip_until IS NULL OR access.vip_until > NEW.completed_at)
    ) THEN
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

-- +goose Down

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION newcomer.enroll_completed_registration()
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

ALTER TABLE newcomer.assessment_transitions
    DROP CONSTRAINT assessment_transition_vip_source_check;
ALTER TABLE newcomer.assessment_transitions
    DROP COLUMN source_vip_transition_id;

ALTER TABLE newcomer.assessments
    DROP CONSTRAINT assessments_state_resolution_check;
ALTER TABLE newcomer.assessments
    ADD CONSTRAINT assessments_check1 CHECK (
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
    );
ALTER TABLE newcomer.assessments
    DROP CONSTRAINT assessments_resolution_code_check;
ALTER TABLE newcomer.assessments
    ADD CONSTRAINT assessments_resolution_code_check CHECK (
        resolution_code IS NULL OR resolution_code IN (
            'requirements_met', 'staff_exempted'
        )
    );

DROP TRIGGER settlement_vip_benefit_outbox_protected
    ON identity.settlement_vip_benefit_outbox;
DROP FUNCTION identity.protect_settlement_vip_benefit_outbox();
DROP TABLE identity.settlement_vip_benefit_outbox;
