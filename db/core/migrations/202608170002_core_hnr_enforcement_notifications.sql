-- +goose Up

-- H&R enforcement is derived from the immutable obligation snapshot and the
-- PostgreSQL clock. It is deliberately not copied into user_access_states:
-- satisfying the last overdue obligation must remove only the H&R source and
-- must never erase an imported/manual or long-term-ratio restriction.
CREATE TABLE traffic.hnr_enforcement_worker_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    last_started_at timestamptz,
    last_succeeded_at timestamptz,
    last_error_code text,
    last_examined_count bigint NOT NULL DEFAULT 0 CHECK (last_examined_count >= 0),
    last_created_count bigint NOT NULL DEFAULT 0 CHECK (last_created_count >= 0),
    updated_at timestamptz NOT NULL
);

INSERT INTO traffic.hnr_enforcement_worker_state (singleton, updated_at)
VALUES (true, CURRENT_TIMESTAMP);

ALTER TABLE traffic.user_hnr_obligations
    ADD CONSTRAINT user_hnr_obligations_user_id_obligation_id_key
        UNIQUE (user_id, obligation_id);

-- Each inbox item is bound to one H&R obligation and one deterministic
-- lifecycle event. No free-form payload or staff identity is stored here.
CREATE TABLE community.hnr_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    obligation_id uuid NOT NULL,
    event_kind text NOT NULL CHECK (event_kind IN (
        'grace_started', 'download_restricted', 'satisfied'
    )),
    created_at timestamptz NOT NULL,
    projected_at timestamptz NOT NULL,
    read_at timestamptz,
    archived_at timestamptz,
    UNIQUE (obligation_id, event_kind),
    FOREIGN KEY (recipient_user_id, obligation_id)
        REFERENCES traffic.user_hnr_obligations (user_id, obligation_id)
        ON DELETE RESTRICT,
    CHECK (projected_at >= created_at),
    CHECK (read_at IS NULL OR read_at >= created_at),
    CHECK (archived_at IS NULL OR archived_at >= created_at)
);

CREATE INDEX hnr_notifications_recipient_recent_idx
    ON community.hnr_notifications (
        recipient_user_id, created_at DESC, id DESC
    ) WHERE archived_at IS NULL;

CREATE INDEX hnr_notifications_recipient_unread_idx
    ON community.hnr_notifications (
        recipient_user_id, created_at DESC, id DESC
    ) WHERE read_at IS NULL AND archived_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION community.protect_hnr_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_user_id uuid;
    source_state text;
    source_assessment_due_at timestamptz;
    source_grace_ends_at timestamptz;
    source_satisfied_at timestamptz;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'H&R notifications cannot be deleted';
    END IF;

    IF TG_OP = 'INSERT' THEN
        SELECT obligation.user_id, obligation.state,
               obligation.assessment_due_at, obligation.grace_ends_at,
               obligation.satisfied_at
        INTO STRICT source_user_id, source_state,
                    source_assessment_due_at, source_grace_ends_at,
                    source_satisfied_at
        FROM traffic.user_hnr_obligations AS obligation
        WHERE obligation.obligation_id = NEW.obligation_id;

        IF source_user_id <> NEW.recipient_user_id
           OR NEW.projected_at < NEW.created_at THEN
            RAISE EXCEPTION 'invalid H&R notification source';
        END IF;

        IF NEW.event_kind = 'grace_started' THEN
            IF source_grace_ends_at <= source_assessment_due_at
               OR NEW.created_at <> source_assessment_due_at
               OR NEW.projected_at < source_assessment_due_at
               OR NOT (
                   source_state = 'tracking'
                   OR source_satisfied_at > source_assessment_due_at
               ) THEN
                RAISE EXCEPTION 'invalid H&R grace notification source';
            END IF;
        ELSIF NEW.event_kind = 'download_restricted' THEN
            IF NEW.created_at <> source_grace_ends_at
               OR NEW.projected_at < source_grace_ends_at
               OR NOT (
                   source_state = 'tracking'
                   OR source_satisfied_at > source_grace_ends_at
               ) THEN
                RAISE EXCEPTION 'invalid H&R restriction notification source';
            END IF;
        ELSIF NEW.event_kind = 'satisfied' THEN
            IF source_state <> 'satisfied'
               OR source_satisfied_at IS NULL
               OR NEW.created_at <> source_satisfied_at
               OR NEW.projected_at < source_satisfied_at THEN
                RAISE EXCEPTION 'invalid H&R satisfaction notification source';
            END IF;
        ELSE
            RAISE EXCEPTION 'invalid H&R notification kind';
        END IF;
        RETURN NEW;
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.recipient_user_id IS DISTINCT FROM NEW.recipient_user_id
       OR OLD.obligation_id IS DISTINCT FROM NEW.obligation_id
       OR OLD.event_kind IS DISTINCT FROM NEW.event_kind
       OR OLD.created_at IS DISTINCT FROM NEW.created_at
       OR OLD.projected_at IS DISTINCT FROM NEW.projected_at THEN
        RAISE EXCEPTION 'H&R notification source is immutable';
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

CREATE TRIGGER hnr_notifications_protected
BEFORE INSERT OR UPDATE OR DELETE ON community.hnr_notifications
FOR EACH ROW EXECUTE FUNCTION community.protect_hnr_notification();

-- One projector function owns the lifecycle-to-inbox mapping. The H&R event
-- consumer calls it on projection changes, while the bounded policy worker
-- calls it after clock-only grace/restriction boundaries pass.
-- +goose StatementBegin
CREATE FUNCTION community.project_hnr_notifications_for_obligation(
    source_obligation_id uuid,
    source_projected_at timestamptz
)
RETURNS bigint
LANGUAGE sql
VOLATILE
AS $$
    WITH source AS (
        SELECT obligation.*
        FROM traffic.user_hnr_obligations AS obligation
        WHERE obligation.obligation_id = source_obligation_id
    ),
    candidates AS (
        SELECT source.user_id, source.obligation_id,
               event.event_kind, event.created_at
        FROM source
        CROSS JOIN LATERAL (
            SELECT 'grace_started'::text AS event_kind,
                   source.assessment_due_at AS created_at
            WHERE source.grace_ends_at > source.assessment_due_at
              AND source_projected_at >= source.assessment_due_at
              AND (
                  source.state = 'tracking'
                  OR source.satisfied_at > source.assessment_due_at
              )

            UNION ALL

            SELECT 'download_restricted'::text,
                   source.grace_ends_at
            WHERE source_projected_at >= source.grace_ends_at
              AND (
                  source.state = 'tracking'
                  OR source.satisfied_at > source.grace_ends_at
              )

            UNION ALL

            SELECT 'satisfied'::text, source.satisfied_at
            WHERE source.state = 'satisfied'
              AND source.satisfied_at IS NOT NULL
              AND source_projected_at >= source.satisfied_at
        ) AS event
    ),
    inserted AS (
        INSERT INTO community.hnr_notifications (
            recipient_user_id, obligation_id, event_kind,
            created_at, projected_at
        )
        SELECT user_id, obligation_id, event_kind,
               created_at, source_projected_at
        FROM candidates
        ON CONFLICT (obligation_id, event_kind) DO NOTHING
        RETURNING 1
    )
    SELECT count(*)::bigint FROM inserted;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION community.project_hnr_notifications_from_projection()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM community.project_hnr_notifications_for_obligation(
        NEW.obligation_id,
        CURRENT_TIMESTAMP
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER user_hnr_notifications_projected
AFTER INSERT OR UPDATE ON traffic.user_hnr_obligations
FOR EACH ROW EXECUTE FUNCTION community.project_hnr_notifications_from_projection();

-- Backfill only events supported by the immutable projection. This can create
-- an old warning followed by a newer satisfied item, but never invents H&R
-- history that is absent from PeerGo's own post-cutover facts.
SELECT community.project_hnr_notifications_for_obligation(
    obligation.obligation_id,
    CURRENT_TIMESTAMP
)
FROM traffic.user_hnr_obligations AS obligation;

-- This remains the one read predicate for HTTP downloads, user management and
-- Tracker subject snapshots. H&R blocks only while at least one obligation is
-- still tracking after its grace deadline; a later satisfied projection makes
-- the predicate false without mutating any other restriction source.
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
        );
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS user_hnr_notifications_projected
    ON traffic.user_hnr_obligations;
DROP FUNCTION IF EXISTS community.project_hnr_notifications_from_projection();
DROP FUNCTION IF EXISTS community.project_hnr_notifications_for_obligation(uuid, timestamptz);

DROP TRIGGER IF EXISTS hnr_notifications_protected
    ON community.hnr_notifications;
DROP FUNCTION IF EXISTS community.protect_hnr_notification();
DROP TABLE IF EXISTS community.hnr_notifications;

ALTER TABLE traffic.user_hnr_obligations
    DROP CONSTRAINT IF EXISTS user_hnr_obligations_user_id_obligation_id_key;
DROP TABLE IF EXISTS traffic.hnr_enforcement_worker_state;
