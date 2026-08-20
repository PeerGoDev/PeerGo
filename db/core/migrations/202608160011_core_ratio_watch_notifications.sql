-- +goose Up

-- Notification sources remain typed business bindings. A ratio notification
-- can only point at one immutable assessment transition owned by its recipient;
-- arbitrary source identifiers or free-form payloads are intentionally absent.
ALTER TABLE ratio_watch.assessment_transitions
    ADD CONSTRAINT ratio_watch_assessment_transitions_assessment_sequence_key
        UNIQUE (assessment_id, sequence);

CREATE TABLE community.ratio_watch_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    assessment_id uuid NOT NULL,
    transition_sequence bigint NOT NULL,
    created_at timestamptz NOT NULL,
    read_at timestamptz,
    archived_at timestamptz,
    UNIQUE (transition_sequence),
    FOREIGN KEY (recipient_user_id, assessment_id)
        REFERENCES ratio_watch.assessments (user_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (assessment_id, transition_sequence)
        REFERENCES ratio_watch.assessment_transitions (assessment_id, sequence)
        ON DELETE RESTRICT,
    CHECK (read_at IS NULL OR read_at >= created_at),
    CHECK (archived_at IS NULL OR archived_at >= created_at)
);

CREATE INDEX ratio_watch_notifications_recipient_recent_idx
    ON community.ratio_watch_notifications (
        recipient_user_id,
        created_at DESC,
        id DESC
    )
    WHERE archived_at IS NULL;

CREATE INDEX ratio_watch_notifications_recipient_unread_idx
    ON community.ratio_watch_notifications (
        recipient_user_id,
        created_at DESC,
        id DESC
    )
    WHERE read_at IS NULL AND archived_at IS NULL;

-- The guard checks the source transition on insertion and then makes that
-- binding immutable. Only the four member-facing lifecycle events are valid
-- notification sources; VIP and account-eligibility resolutions remain
-- operational facts rather than inbox messages.
-- +goose StatementBegin
CREATE FUNCTION community.protect_ratio_watch_notification()
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
-- +goose StatementEnd

CREATE TRIGGER ratio_watch_notifications_protected
BEFORE INSERT OR UPDATE OR DELETE ON community.ratio_watch_notifications
FOR EACH ROW EXECUTE FUNCTION community.protect_ratio_watch_notification();

-- Every future state transition is projected inside the same database
-- transaction as the assessment update. A failed notification projection
-- therefore cannot leave the member-facing state behind the authoritative
-- assessment, and worker retries remain idempotent through the unique source.
-- +goose StatementBegin
CREATE FUNCTION community.project_ratio_watch_notification()
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
            recipient_user_id,
            assessment_id,
            transition_sequence,
            created_at
        )
        SELECT assessment.user_id, NEW.assessment_id, NEW.sequence, NEW.occurred_at
        FROM ratio_watch.assessments AS assessment
        WHERE assessment.id = NEW.assessment_id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER ratio_watch_transition_notification_projected
AFTER INSERT ON ratio_watch.assessment_transitions
FOR EACH ROW EXECUTE FUNCTION community.project_ratio_watch_notification();

-- Backfill from immutable transition facts so enabling this feature does not
-- depend on old application DTOs and cannot invent events that never occurred.
INSERT INTO community.ratio_watch_notifications (
    recipient_user_id,
    assessment_id,
    transition_sequence,
    created_at
)
SELECT
    assessment.user_id,
    transition.assessment_id,
    transition.sequence,
    transition.occurred_at
FROM ratio_watch.assessment_transitions AS transition
JOIN ratio_watch.assessments AS assessment
  ON assessment.id = transition.assessment_id
WHERE (transition.to_status = 'watching' AND transition.reason_code = 'entered_watch')
   OR (transition.to_status = 'warning' AND transition.reason_code = 'deadline_warning')
   OR (transition.to_status = 'download_restricted'
       AND transition.reason_code IN ('deadline_restricted', 'warning_restricted'))
   OR (transition.to_status = 'satisfied' AND transition.reason_code = 'ratio_recovered');

-- +goose Down

DROP TRIGGER IF EXISTS ratio_watch_transition_notification_projected
    ON ratio_watch.assessment_transitions;
DROP FUNCTION IF EXISTS community.project_ratio_watch_notification();

DROP TRIGGER IF EXISTS ratio_watch_notifications_protected
    ON community.ratio_watch_notifications;
DROP FUNCTION IF EXISTS community.protect_ratio_watch_notification();
DROP TABLE IF EXISTS community.ratio_watch_notifications;

ALTER TABLE ratio_watch.assessment_transitions
    DROP CONSTRAINT IF EXISTS ratio_watch_assessment_transitions_assessment_sequence_key;
