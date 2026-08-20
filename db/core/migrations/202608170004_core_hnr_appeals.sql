-- +goose Up

-- H&R appeals are deliberately separate from ratio-watch appeals: one H&R
-- obligation belongs to one torrent completion, while a ratio appeal belongs
-- to the account-wide assessment. They share workflow semantics, not tables or
-- polymorphic target columns.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    (
        'hnr.appeal.create.self',
        '为自己的逾期 H&R 义务提交一次申诉',
        'medium', 'self', 'web-session', true, true
    ),
    (
        'hnr.assessment.manage',
        '批准或驳回 H&R 申诉并签发本地豁免',
        'high', 'none', 'staff-session', true, true
    );

INSERT INTO authz.role_permissions (role_id, action)
VALUES
    ('member', 'hnr.appeal.create.self'),
    ('site_admin', 'hnr.assessment.manage');

CREATE TABLE traffic.hnr_appeals (
    id uuid PRIMARY KEY,
    obligation_id uuid NOT NULL UNIQUE,
    user_id uuid NOT NULL,
    statement text NOT NULL CHECK (
        char_length(btrim(statement)) BETWEEN 20 AND 1000
        AND statement = btrim(statement)
    ),
    authorization_decision_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (user_id, id),
    FOREIGN KEY (user_id, obligation_id)
        REFERENCES traffic.user_hnr_obligations (user_id, obligation_id)
        ON DELETE RESTRICT
);

CREATE INDEX hnr_appeals_user_recent_idx
    ON traffic.hnr_appeals (user_id, created_at DESC, id DESC);

CREATE TABLE traffic.hnr_appeal_resolutions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    appeal_id uuid NOT NULL UNIQUE
        REFERENCES traffic.hnr_appeals (id) ON DELETE RESTRICT,
    outcome text NOT NULL CHECK (outcome IN (
        'approved', 'rejected', 'obligation_resolved'
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
        (outcome = 'obligation_resolved'
            AND response IS NULL
            AND actor_id IS NULL
            AND authorization_decision_id IS NULL)
    )
);

-- Approval is a Core-owned overlay instead of a mutation of the Settlement
-- obligation. Later contiguous Settlement versions can therefore keep
-- advancing without colliding with a locally manufactured terminal state.
CREATE TABLE traffic.hnr_appeal_exemptions (
    obligation_id uuid PRIMARY KEY,
    user_id uuid NOT NULL,
    appeal_resolution_id uuid NOT NULL UNIQUE
        REFERENCES traffic.hnr_appeal_resolutions (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    UNIQUE (user_id, obligation_id),
    FOREIGN KEY (user_id, obligation_id)
        REFERENCES traffic.user_hnr_obligations (user_id, obligation_id)
        ON DELETE RESTRICT
);

-- +goose StatementBegin
CREATE FUNCTION traffic.validate_hnr_appeal_insert()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_state text;
    source_grace_ends_at timestamptz;
BEGIN
    SELECT obligation.state, obligation.grace_ends_at
    INTO STRICT source_state, source_grace_ends_at
    FROM traffic.user_hnr_obligations AS obligation
    WHERE obligation.obligation_id = NEW.obligation_id
      AND obligation.user_id = NEW.user_id;

    IF source_state <> 'tracking'
       OR NEW.created_at < source_grace_ends_at THEN
        RAISE EXCEPTION 'H&R appeal must bind an overdue active obligation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER hnr_appeal_insert_valid
BEFORE INSERT ON traffic.hnr_appeals
FOR EACH ROW EXECUTE FUNCTION traffic.validate_hnr_appeal_insert();

-- +goose StatementBegin
CREATE FUNCTION traffic.validate_hnr_appeal_resolution_insert()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_user_id uuid;
    source_created_at timestamptz;
    source_state text;
BEGIN
    SELECT appeal.user_id, appeal.created_at, obligation.state
    INTO STRICT source_user_id, source_created_at, source_state
    FROM traffic.hnr_appeals AS appeal
    JOIN traffic.user_hnr_obligations AS obligation
      ON obligation.obligation_id = appeal.obligation_id
    WHERE appeal.id = NEW.appeal_id;

    IF NEW.created_at < source_created_at THEN
        RAISE EXCEPTION 'H&R appeal resolution predates its request';
    END IF;

    IF NEW.outcome IN ('approved', 'rejected') THEN
        IF source_state <> 'tracking' OR NEW.actor_id = source_user_id THEN
            RAISE EXCEPTION 'staff H&R appeal decision must target an active foreign obligation';
        END IF;
    ELSIF source_state = 'tracking' THEN
        RAISE EXCEPTION 'active H&R obligation cannot automatically resolve its appeal';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER hnr_appeal_resolution_insert_valid
BEFORE INSERT ON traffic.hnr_appeal_resolutions
FOR EACH ROW EXECUTE FUNCTION traffic.validate_hnr_appeal_resolution_insert();

-- +goose StatementBegin
CREATE FUNCTION traffic.reject_hnr_appeal_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'H&R appeal evidence is immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER hnr_appeals_immutable
BEFORE UPDATE OR DELETE ON traffic.hnr_appeals
FOR EACH ROW EXECUTE FUNCTION traffic.reject_hnr_appeal_mutation();

CREATE TRIGGER hnr_appeal_resolutions_immutable
BEFORE UPDATE OR DELETE ON traffic.hnr_appeal_resolutions
FOR EACH ROW EXECUTE FUNCTION traffic.reject_hnr_appeal_mutation();

CREATE TRIGGER hnr_appeal_exemptions_immutable
BEFORE UPDATE OR DELETE ON traffic.hnr_appeal_exemptions
FOR EACH ROW EXECUTE FUNCTION traffic.reject_hnr_appeal_mutation();

-- +goose StatementBegin
CREATE FUNCTION traffic.project_hnr_appeal_exemption()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.outcome = 'approved' THEN
        INSERT INTO traffic.hnr_appeal_exemptions (
            obligation_id, user_id, appeal_resolution_id, created_at
        )
        SELECT appeal.obligation_id, appeal.user_id, NEW.id, NEW.created_at
        FROM traffic.hnr_appeals AS appeal
        WHERE appeal.id = NEW.appeal_id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER hnr_appeal_exemption_projected
AFTER INSERT ON traffic.hnr_appeal_resolutions
FOR EACH ROW EXECUTE FUNCTION traffic.project_hnr_appeal_exemption();

-- A natural Settlement terminal projection closes a still-pending request.
-- The normal H&R satisfied notification already describes that outcome, so no
-- duplicate appeal notification is generated for this automatic resolution.
-- +goose StatementBegin
CREATE FUNCTION traffic.resolve_hnr_appeal_from_obligation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.state = 'tracking' AND NEW.state = 'satisfied' THEN
        INSERT INTO traffic.hnr_appeal_resolutions (
            appeal_id, outcome, created_at
        )
        SELECT appeal.id, 'obligation_resolved', NEW.occurred_at
        FROM traffic.hnr_appeals AS appeal
        WHERE appeal.obligation_id = NEW.obligation_id
        ON CONFLICT (appeal_id) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER hnr_appeal_resolved_from_obligation
AFTER UPDATE ON traffic.user_hnr_obligations
FOR EACH ROW EXECUTE FUNCTION traffic.resolve_hnr_appeal_from_obligation();

CREATE TABLE community.hnr_appeal_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    appeal_id uuid NOT NULL,
    resolution_id uuid NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    read_at timestamptz,
    archived_at timestamptz,
    FOREIGN KEY (recipient_user_id, appeal_id)
        REFERENCES traffic.hnr_appeals (user_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (resolution_id)
        REFERENCES traffic.hnr_appeal_resolutions (id) ON DELETE RESTRICT,
    CHECK (read_at IS NULL OR read_at >= created_at),
    CHECK (archived_at IS NULL OR archived_at >= created_at)
);

CREATE INDEX hnr_appeal_notifications_recipient_recent_idx
    ON community.hnr_appeal_notifications (
        recipient_user_id, created_at DESC, id DESC
    ) WHERE archived_at IS NULL;

CREATE INDEX hnr_appeal_notifications_recipient_unread_idx
    ON community.hnr_appeal_notifications (
        recipient_user_id, created_at DESC, id DESC
    ) WHERE read_at IS NULL AND archived_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION community.protect_hnr_appeal_notification()
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
        RAISE EXCEPTION 'H&R appeal notifications cannot be deleted';
    END IF;

    IF TG_OP = 'INSERT' THEN
        SELECT appeal.user_id, appeal.id, resolution.created_at,
               resolution.outcome
        INTO STRICT source_user_id, source_appeal_id,
                    source_created_at, source_outcome
        FROM traffic.hnr_appeal_resolutions AS resolution
        JOIN traffic.hnr_appeals AS appeal ON appeal.id = resolution.appeal_id
        WHERE resolution.id = NEW.resolution_id;

        IF source_outcome NOT IN ('approved', 'rejected')
           OR source_user_id <> NEW.recipient_user_id
           OR source_appeal_id <> NEW.appeal_id
           OR source_created_at <> NEW.created_at THEN
            RAISE EXCEPTION 'invalid H&R appeal notification source';
        END IF;
        RETURN NEW;
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.recipient_user_id IS DISTINCT FROM NEW.recipient_user_id
       OR OLD.appeal_id IS DISTINCT FROM NEW.appeal_id
       OR OLD.resolution_id IS DISTINCT FROM NEW.resolution_id
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'H&R appeal notification source is immutable';
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

CREATE TRIGGER hnr_appeal_notifications_protected
BEFORE INSERT OR UPDATE OR DELETE ON community.hnr_appeal_notifications
FOR EACH ROW EXECUTE FUNCTION community.protect_hnr_appeal_notification();

-- +goose StatementBegin
CREATE FUNCTION community.project_hnr_appeal_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.outcome IN ('approved', 'rejected') THEN
        INSERT INTO community.hnr_appeal_notifications (
            recipient_user_id, appeal_id, resolution_id, created_at
        )
        SELECT appeal.user_id, appeal.id, NEW.id, NEW.created_at
        FROM traffic.hnr_appeals AS appeal
        WHERE appeal.id = NEW.appeal_id;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER hnr_appeal_notification_projected
AFTER INSERT ON traffic.hnr_appeal_resolutions
FOR EACH ROW EXECUTE FUNCTION community.project_hnr_appeal_notification();

-- Manual H&R exemption participates in the one canonical predicate used by
-- both HTTP torrent download and Tracker subject-control projection.
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
        );
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS hnr_appeal_notification_projected
    ON traffic.hnr_appeal_resolutions;
DROP FUNCTION IF EXISTS community.project_hnr_appeal_notification();
DROP TRIGGER IF EXISTS hnr_appeal_notifications_protected
    ON community.hnr_appeal_notifications;
DROP FUNCTION IF EXISTS community.protect_hnr_appeal_notification();
DROP TABLE IF EXISTS community.hnr_appeal_notifications;

DROP TRIGGER IF EXISTS hnr_appeal_resolved_from_obligation
    ON traffic.user_hnr_obligations;
DROP FUNCTION IF EXISTS traffic.resolve_hnr_appeal_from_obligation();
DROP TRIGGER IF EXISTS hnr_appeal_exemption_projected
    ON traffic.hnr_appeal_resolutions;
DROP FUNCTION IF EXISTS traffic.project_hnr_appeal_exemption();
DROP TRIGGER IF EXISTS hnr_appeal_exemptions_immutable
    ON traffic.hnr_appeal_exemptions;
DROP TRIGGER IF EXISTS hnr_appeal_resolutions_immutable
    ON traffic.hnr_appeal_resolutions;
DROP TRIGGER IF EXISTS hnr_appeals_immutable ON traffic.hnr_appeals;
DROP FUNCTION IF EXISTS traffic.reject_hnr_appeal_mutation();
DROP TRIGGER IF EXISTS hnr_appeal_resolution_insert_valid
    ON traffic.hnr_appeal_resolutions;
DROP FUNCTION IF EXISTS traffic.validate_hnr_appeal_resolution_insert();
DROP TRIGGER IF EXISTS hnr_appeal_insert_valid ON traffic.hnr_appeals;
DROP FUNCTION IF EXISTS traffic.validate_hnr_appeal_insert();
DROP TABLE IF EXISTS traffic.hnr_appeal_exemptions;
DROP TABLE IF EXISTS traffic.hnr_appeal_resolutions;
DROP TABLE IF EXISTS traffic.hnr_appeals;

DELETE FROM authz.role_permissions
WHERE (role_id = 'member' AND action = 'hnr.appeal.create.self')
   OR (role_id = 'site_admin' AND action = 'hnr.assessment.manage');
DELETE FROM authz.permissions
WHERE action IN ('hnr.appeal.create.self', 'hnr.assessment.manage');
