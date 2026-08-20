-- +goose Up

-- A member may submit one appeal for one immutable assessment. The request is
-- append-only and keeps its idempotency key as the public identifier, so a
-- browser retry cannot create a second case or silently replace the original
-- statement.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES (
    'ratio.appeal.create.self',
    '提交自己当前长期分享率考核的一次申诉',
    'medium', 'self', 'web-session', true, true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'ratio.appeal.create.self');

CREATE TABLE ratio_watch.appeals (
    id uuid PRIMARY KEY,
    assessment_id uuid NOT NULL UNIQUE,
    user_id uuid NOT NULL,
    statement text NOT NULL CHECK (
        char_length(btrim(statement)) BETWEEN 20 AND 1000
        AND statement = btrim(statement)
    ),
    authorization_decision_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (user_id, id),
    FOREIGN KEY (user_id, assessment_id)
        REFERENCES ratio_watch.assessments (user_id, id) ON DELETE RESTRICT
);

CREATE INDEX ratio_watch_appeals_user_recent_idx
    ON ratio_watch.appeals (user_id, created_at DESC, id DESC);

-- Resolution is a separate append-only fact. Rejection leaves the assessment
-- untouched; approval is written before the existing staff-clear transition
-- in the same serializable transaction. If the worker resolves the assessment
-- first, the transition trigger below closes the pending appeal automatically.
CREATE TABLE ratio_watch.appeal_resolutions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    appeal_id uuid NOT NULL UNIQUE
        REFERENCES ratio_watch.appeals (id) ON DELETE RESTRICT,
    outcome text NOT NULL CHECK (outcome IN (
        'approved', 'rejected', 'assessment_resolved'
    )),
    response text CHECK (
        response IS NULL OR (
            char_length(btrim(response)) BETWEEN 10 AND 1000
            AND response = btrim(response)
        )
    ),
    actor_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid,
    created_at timestamptz NOT NULL,
    CHECK (
        (outcome IN ('approved', 'rejected')
            AND response IS NOT NULL
            AND actor_id IS NOT NULL
            AND authorization_decision_id IS NOT NULL)
        OR
        (outcome = 'assessment_resolved'
            AND response IS NULL
            AND actor_id IS NULL
            AND authorization_decision_id IS NULL)
    )
);

-- +goose StatementBegin
CREATE FUNCTION ratio_watch.validate_appeal_insert()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_status text;
    source_started_at timestamptz;
BEGIN
    SELECT assessment.status, assessment.started_at
    INTO STRICT source_status, source_started_at
    FROM ratio_watch.assessments AS assessment
    WHERE assessment.id = NEW.assessment_id
      AND assessment.user_id = NEW.user_id;

    IF source_status NOT IN ('watching', 'warning', 'download_restricted')
       OR NEW.created_at < source_started_at THEN
        RAISE EXCEPTION 'appeal must bind the member current active assessment';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER ratio_watch_appeal_insert_valid
BEFORE INSERT ON ratio_watch.appeals
FOR EACH ROW EXECUTE FUNCTION ratio_watch.validate_appeal_insert();

-- +goose StatementBegin
CREATE FUNCTION ratio_watch.validate_appeal_resolution_insert()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_user_id uuid;
    source_created_at timestamptz;
    source_status text;
BEGIN
    SELECT appeal.user_id, appeal.created_at, assessment.status
    INTO STRICT source_user_id, source_created_at, source_status
    FROM ratio_watch.appeals AS appeal
    JOIN ratio_watch.assessments AS assessment
      ON assessment.id = appeal.assessment_id
    WHERE appeal.id = NEW.appeal_id;

    IF NEW.created_at < source_created_at THEN
        RAISE EXCEPTION 'appeal resolution predates its request';
    END IF;

    IF NEW.outcome IN ('approved', 'rejected') THEN
        IF source_status NOT IN ('watching', 'warning', 'download_restricted')
           OR NEW.actor_id = source_user_id THEN
            RAISE EXCEPTION 'staff appeal decision does not target an active foreign assessment';
        END IF;
    ELSIF source_status IN ('watching', 'warning', 'download_restricted') THEN
        RAISE EXCEPTION 'active assessment cannot automatically resolve its appeal';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER ratio_watch_appeal_resolution_insert_valid
BEFORE INSERT ON ratio_watch.appeal_resolutions
FOR EACH ROW EXECUTE FUNCTION ratio_watch.validate_appeal_resolution_insert();

-- +goose StatementBegin
CREATE FUNCTION ratio_watch.reject_appeal_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'ratio watch appeal evidence is immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER ratio_watch_appeals_immutable
BEFORE UPDATE OR DELETE ON ratio_watch.appeals
FOR EACH ROW EXECUTE FUNCTION ratio_watch.reject_appeal_mutation();

CREATE TRIGGER ratio_watch_appeal_resolutions_immutable
BEFORE UPDATE OR DELETE ON ratio_watch.appeal_resolutions
FOR EACH ROW EXECUTE FUNCTION ratio_watch.reject_appeal_mutation();

-- +goose StatementBegin
CREATE FUNCTION ratio_watch.resolve_appeal_from_assessment_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.to_status IN (
        'satisfied', 'manually_cleared', 'vip_exempted', 'ineligible'
    ) THEN
        INSERT INTO ratio_watch.appeal_resolutions (
            appeal_id, outcome, created_at
        )
        SELECT appeal.id, 'assessment_resolved', NEW.occurred_at
        FROM ratio_watch.appeals AS appeal
        WHERE appeal.assessment_id = NEW.assessment_id
        ON CONFLICT (appeal_id) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER ratio_watch_appeal_resolved_from_transition
AFTER INSERT ON ratio_watch.assessment_transitions
FOR EACH ROW EXECUTE FUNCTION ratio_watch.resolve_appeal_from_assessment_transition();

-- Rejected appeals need their own typed notification source because no
-- assessment transition occurs. Approved appeals reuse the manually-cleared
-- assessment transition notification and therefore do not create duplicates.
CREATE TABLE community.ratio_watch_appeal_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    appeal_id uuid NOT NULL,
    resolution_id uuid NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    read_at timestamptz,
    archived_at timestamptz,
    FOREIGN KEY (recipient_user_id, appeal_id)
        REFERENCES ratio_watch.appeals (user_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (resolution_id)
        REFERENCES ratio_watch.appeal_resolutions (id) ON DELETE RESTRICT,
    CHECK (read_at IS NULL OR read_at >= created_at),
    CHECK (archived_at IS NULL OR archived_at >= created_at)
);

CREATE INDEX ratio_watch_appeal_notifications_recipient_recent_idx
    ON community.ratio_watch_appeal_notifications (
        recipient_user_id, created_at DESC, id DESC
    ) WHERE archived_at IS NULL;

CREATE INDEX ratio_watch_appeal_notifications_recipient_unread_idx
    ON community.ratio_watch_appeal_notifications (
        recipient_user_id, created_at DESC, id DESC
    ) WHERE read_at IS NULL AND archived_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION community.protect_ratio_watch_appeal_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_user_id uuid;
    source_appeal_id uuid;
    source_created_at timestamptz;
    source_outcome text;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'ratio watch appeal notifications cannot be deleted';
    END IF;

    IF TG_OP = 'INSERT' THEN
        SELECT appeal.user_id, appeal.id, resolution.created_at,
               resolution.outcome
        INTO STRICT source_user_id, source_appeal_id,
                    source_created_at, source_outcome
        FROM ratio_watch.appeal_resolutions AS resolution
        JOIN ratio_watch.appeals AS appeal ON appeal.id = resolution.appeal_id
        WHERE resolution.id = NEW.resolution_id;

        IF source_outcome <> 'rejected'
           OR source_user_id <> NEW.recipient_user_id
           OR source_appeal_id <> NEW.appeal_id
           OR source_created_at <> NEW.created_at THEN
            RAISE EXCEPTION 'invalid ratio watch appeal notification source';
        END IF;
        RETURN NEW;
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.recipient_user_id IS DISTINCT FROM NEW.recipient_user_id
       OR OLD.appeal_id IS DISTINCT FROM NEW.appeal_id
       OR OLD.resolution_id IS DISTINCT FROM NEW.resolution_id
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'ratio watch appeal notification source is immutable';
    END IF;
    IF OLD.read_at IS NOT NULL AND OLD.read_at IS DISTINCT FROM NEW.read_at THEN
        RAISE EXCEPTION 'notification read state is monotonic';
    END IF;
    IF OLD.archived_at IS NOT NULL
       AND OLD.archived_at IS DISTINCT FROM NEW.archived_at THEN
        RAISE EXCEPTION 'notification archive state is monotonic';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER ratio_watch_appeal_notifications_protected
BEFORE INSERT OR UPDATE OR DELETE
ON community.ratio_watch_appeal_notifications
FOR EACH ROW EXECUTE FUNCTION community.protect_ratio_watch_appeal_notification();

-- +goose StatementBegin
CREATE FUNCTION community.project_ratio_watch_appeal_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.outcome = 'rejected' THEN
        INSERT INTO community.ratio_watch_appeal_notifications (
            recipient_user_id, appeal_id, resolution_id, created_at
        )
        SELECT appeal.user_id, appeal.id, NEW.id, NEW.created_at
        FROM ratio_watch.appeals AS appeal
        WHERE appeal.id = NEW.appeal_id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER ratio_watch_appeal_notification_projected
AFTER INSERT ON ratio_watch.appeal_resolutions
FOR EACH ROW EXECUTE FUNCTION community.project_ratio_watch_appeal_notification();

-- Staff clears are now member-facing outcomes. The source remains the same
-- immutable assessment transition, so direct clear and approved appeal share
-- one inbox contract without inventing a second success notification.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION community.protect_ratio_watch_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_user_id uuid;
    source_occurred_at timestamptz;
    source_status text;
    source_reason_code text;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'ratio watch notifications cannot be deleted';
    END IF;

    IF TG_OP = 'INSERT' THEN
        SELECT assessment.user_id, transition.occurred_at,
               transition.to_status, transition.reason_code
        INTO STRICT source_user_id, source_occurred_at,
                    source_status, source_reason_code
        FROM ratio_watch.assessment_transitions AS transition
        JOIN ratio_watch.assessments AS assessment
          ON assessment.id = transition.assessment_id
        WHERE transition.assessment_id = NEW.assessment_id
          AND transition.sequence = NEW.transition_sequence;

        IF source_user_id <> NEW.recipient_user_id
           OR source_occurred_at <> NEW.created_at
           OR NOT (
               (source_status = 'watching' AND source_reason_code = 'entered_watch')
               OR (source_status = 'warning' AND source_reason_code = 'deadline_warning')
               OR (source_status = 'download_restricted'
                   AND source_reason_code IN ('deadline_restricted', 'warning_restricted'))
               OR (source_status = 'satisfied' AND source_reason_code = 'ratio_recovered')
               OR (source_status = 'manually_cleared' AND source_reason_code = 'staff_cleared')
           ) THEN
            RAISE EXCEPTION 'invalid ratio watch notification source';
        END IF;
        RETURN NEW;
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.recipient_user_id IS DISTINCT FROM NEW.recipient_user_id
       OR OLD.assessment_id IS DISTINCT FROM NEW.assessment_id
       OR OLD.transition_sequence IS DISTINCT FROM NEW.transition_sequence
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'ratio watch notification source is immutable';
    END IF;
    IF OLD.read_at IS NOT NULL AND OLD.read_at IS DISTINCT FROM NEW.read_at THEN
        RAISE EXCEPTION 'notification read state is monotonic';
    END IF;
    IF OLD.archived_at IS NOT NULL
       AND OLD.archived_at IS DISTINCT FROM NEW.archived_at THEN
        RAISE EXCEPTION 'notification archive state is monotonic';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION community.project_ratio_watch_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.to_status = 'watching' AND NEW.reason_code = 'entered_watch')
       OR (NEW.to_status = 'warning' AND NEW.reason_code = 'deadline_warning')
       OR (NEW.to_status = 'download_restricted'
           AND NEW.reason_code IN ('deadline_restricted', 'warning_restricted'))
       OR (NEW.to_status = 'satisfied' AND NEW.reason_code = 'ratio_recovered')
       OR (NEW.to_status = 'manually_cleared' AND NEW.reason_code = 'staff_cleared') THEN
        INSERT INTO community.ratio_watch_notifications (
            recipient_user_id, assessment_id, transition_sequence, created_at
        )
        SELECT assessment.user_id, NEW.assessment_id, NEW.sequence, NEW.occurred_at
        FROM ratio_watch.assessments AS assessment
        WHERE assessment.id = NEW.assessment_id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

INSERT INTO community.ratio_watch_notifications (
    recipient_user_id, assessment_id, transition_sequence, created_at
)
SELECT assessment.user_id, transition.assessment_id,
       transition.sequence, transition.occurred_at
FROM ratio_watch.assessment_transitions AS transition
JOIN ratio_watch.assessments AS assessment
  ON assessment.id = transition.assessment_id
WHERE transition.to_status = 'manually_cleared'
  AND transition.reason_code = 'staff_cleared'
ON CONFLICT (transition_sequence) DO NOTHING;

-- +goose Down

DROP TRIGGER IF EXISTS ratio_watch_notifications_protected
    ON community.ratio_watch_notifications;

DELETE FROM community.ratio_watch_notifications AS notification
USING ratio_watch.assessment_transitions AS transition
WHERE notification.assessment_id = transition.assessment_id
  AND notification.transition_sequence = transition.sequence
  AND transition.to_status = 'manually_cleared'
  AND transition.reason_code = 'staff_cleared';

CREATE OR REPLACE FUNCTION community.project_ratio_watch_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF (NEW.to_status = 'watching' AND NEW.reason_code = 'entered_watch')
       OR (NEW.to_status = 'warning' AND NEW.reason_code = 'deadline_warning')
       OR (NEW.to_status = 'download_restricted'
           AND NEW.reason_code IN ('deadline_restricted', 'warning_restricted'))
       OR (NEW.to_status = 'satisfied' AND NEW.reason_code = 'ratio_recovered') THEN
        INSERT INTO community.ratio_watch_notifications (
            recipient_user_id, assessment_id, transition_sequence, created_at
        )
        SELECT assessment.user_id, NEW.assessment_id, NEW.sequence, NEW.occurred_at
        FROM ratio_watch.assessments AS assessment
        WHERE assessment.id = NEW.assessment_id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION community.protect_ratio_watch_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_user_id uuid;
    source_occurred_at timestamptz;
    source_status text;
    source_reason_code text;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'ratio watch notifications cannot be deleted';
    END IF;
    IF TG_OP = 'INSERT' THEN
        SELECT assessment.user_id, transition.occurred_at,
               transition.to_status, transition.reason_code
        INTO STRICT source_user_id, source_occurred_at,
                    source_status, source_reason_code
        FROM ratio_watch.assessment_transitions AS transition
        JOIN ratio_watch.assessments AS assessment
          ON assessment.id = transition.assessment_id
        WHERE transition.assessment_id = NEW.assessment_id
          AND transition.sequence = NEW.transition_sequence;
        IF source_user_id <> NEW.recipient_user_id
           OR source_occurred_at <> NEW.created_at
           OR NOT (
               (source_status = 'watching' AND source_reason_code = 'entered_watch')
               OR (source_status = 'warning' AND source_reason_code = 'deadline_warning')
               OR (source_status = 'download_restricted'
                   AND source_reason_code IN ('deadline_restricted', 'warning_restricted'))
               OR (source_status = 'satisfied' AND source_reason_code = 'ratio_recovered')
           ) THEN
            RAISE EXCEPTION 'invalid ratio watch notification source';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.recipient_user_id IS DISTINCT FROM NEW.recipient_user_id
       OR OLD.assessment_id IS DISTINCT FROM NEW.assessment_id
       OR OLD.transition_sequence IS DISTINCT FROM NEW.transition_sequence
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'ratio watch notification source is immutable';
    END IF;
    IF OLD.read_at IS NOT NULL AND OLD.read_at IS DISTINCT FROM NEW.read_at THEN
        RAISE EXCEPTION 'notification read state is monotonic';
    END IF;
    IF OLD.archived_at IS NOT NULL
       AND OLD.archived_at IS DISTINCT FROM NEW.archived_at THEN
        RAISE EXCEPTION 'notification archive state is monotonic';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER ratio_watch_notifications_protected
BEFORE INSERT OR UPDATE OR DELETE ON community.ratio_watch_notifications
FOR EACH ROW EXECUTE FUNCTION community.protect_ratio_watch_notification();

DROP TRIGGER IF EXISTS ratio_watch_appeal_notification_projected
    ON ratio_watch.appeal_resolutions;
DROP FUNCTION IF EXISTS community.project_ratio_watch_appeal_notification();
DROP TRIGGER IF EXISTS ratio_watch_appeal_notifications_protected
    ON community.ratio_watch_appeal_notifications;
DROP FUNCTION IF EXISTS community.protect_ratio_watch_appeal_notification();
DROP TABLE IF EXISTS community.ratio_watch_appeal_notifications;

DROP TRIGGER IF EXISTS ratio_watch_appeal_resolved_from_transition
    ON ratio_watch.assessment_transitions;
DROP FUNCTION IF EXISTS ratio_watch.resolve_appeal_from_assessment_transition();
DROP TRIGGER IF EXISTS ratio_watch_appeal_resolutions_immutable
    ON ratio_watch.appeal_resolutions;
DROP TRIGGER IF EXISTS ratio_watch_appeals_immutable ON ratio_watch.appeals;
DROP FUNCTION IF EXISTS ratio_watch.reject_appeal_mutation();
DROP TRIGGER IF EXISTS ratio_watch_appeal_resolution_insert_valid
    ON ratio_watch.appeal_resolutions;
DROP FUNCTION IF EXISTS ratio_watch.validate_appeal_resolution_insert();
DROP TRIGGER IF EXISTS ratio_watch_appeal_insert_valid ON ratio_watch.appeals;
DROP FUNCTION IF EXISTS ratio_watch.validate_appeal_insert();
DROP TABLE IF EXISTS ratio_watch.appeal_resolutions;
DROP TABLE IF EXISTS ratio_watch.appeals;

DELETE FROM authz.role_permissions
WHERE role_id = 'member' AND action = 'ratio.appeal.create.self';
DELETE FROM authz.permissions WHERE action = 'ratio.appeal.create.self';
