-- +goose Up

-- A contribution reminder is an immutable observation, not a disciplinary
-- action. It freezes the membership period, policy revision, evidence state
-- and measured value that staff actually saw so later policy or projection
-- changes cannot rewrite why the member was contacted.
CREATE TABLE workgroups.contribution_reminders (
    id uuid PRIMARY KEY,
    membership_id uuid NOT NULL
        REFERENCES workgroups.memberships (id) ON DELETE RESTRICT,
    group_kind text NOT NULL
        REFERENCES workgroups.definitions (kind) ON DELETE RESTRICT,
    recipient_user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    period_starts_at timestamptz NOT NULL,
    period_ends_at timestamptz NOT NULL,
    metric text NOT NULL CHECK (metric IN (
        'trusted_torrents_published',
        'torrent_review_votes',
        'seeding_active_seconds'
    )),
    policy_revision bigint NOT NULL,
    observed_at timestamptz NOT NULL,
    evidence_through timestamptz,
    evidence_state text NOT NULL CHECK (evidence_state IN ('collecting', 'complete')),
    current_value bigint NOT NULL CHECK (current_value >= 0),
    target_value bigint NOT NULL CHECK (target_value > 0),
    assessment_state text NOT NULL CHECK (assessment_state IN ('in_progress', 'not_met')),
    explanation_code text NOT NULL CHECK (explanation_code IN (
        'period_in_progress', 'below_target', 'no_contribution'
    )),
    full_period_active boolean NOT NULL CHECK (full_period_active),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    issued_by uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (membership_id, period_starts_at),
    FOREIGN KEY (group_kind, policy_revision)
        REFERENCES workgroups.contribution_policy_revisions (group_kind, revision)
        ON DELETE RESTRICT,
    CHECK (period_starts_at = date_trunc('month', period_starts_at)),
    CHECK (period_ends_at = period_starts_at + interval '1 month'),
    CHECK (observed_at >= period_starts_at AND observed_at <= period_ends_at),
    CHECK (created_at >= observed_at),
    CHECK (current_value < target_value),
    CHECK (
        (assessment_state = 'in_progress'
            AND explanation_code = 'period_in_progress'
            AND observed_at < period_ends_at)
        OR (assessment_state = 'not_met'
            AND explanation_code IN ('below_target', 'no_contribution')
            AND observed_at = period_ends_at
            AND evidence_state = 'complete')
    ),
    CHECK (
        (group_kind = 'reseed' AND metric = 'trusted_torrents_published')
        OR (group_kind = 'review' AND metric = 'torrent_review_votes')
        OR (group_kind = 'retention' AND metric = 'seeding_active_seconds')
    )
);

CREATE INDEX workgroup_contribution_reminders_member_recent_idx
    ON workgroups.contribution_reminders (
        membership_id, period_starts_at DESC, created_at DESC
    );

-- +goose StatementBegin
CREATE FUNCTION workgroups.validate_contribution_reminder()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_group_kind text;
    source_user_id uuid;
BEGIN
    SELECT group_kind, user_id
    INTO STRICT source_group_kind, source_user_id
    FROM workgroups.memberships
    WHERE id = NEW.membership_id;

    IF source_group_kind <> NEW.group_kind
       OR source_user_id <> NEW.recipient_user_id THEN
        RAISE EXCEPTION 'invalid workgroup contribution reminder membership';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER workgroup_contribution_reminders_validated
BEFORE INSERT ON workgroups.contribution_reminders
FOR EACH ROW EXECUTE FUNCTION workgroups.validate_contribution_reminder();

CREATE TRIGGER workgroup_contribution_reminders_immutable
BEFORE UPDATE OR DELETE ON workgroups.contribution_reminders
FOR EACH ROW EXECUTE FUNCTION workgroups.reject_history_mutation();

-- Notification read/archive state stays in the existing typed private inbox.
-- The source binding is validated at insertion and can never be replaced by a
-- free-form message or a notification addressed to another account.
CREATE TABLE community.workgroup_contribution_notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    reminder_id uuid NOT NULL UNIQUE
        REFERENCES workgroups.contribution_reminders (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    read_at timestamptz,
    archived_at timestamptz,
    CHECK (read_at IS NULL OR read_at >= created_at),
    CHECK (archived_at IS NULL OR archived_at >= created_at)
);

CREATE INDEX workgroup_contribution_notifications_recipient_recent_idx
    ON community.workgroup_contribution_notifications (
        recipient_user_id, created_at DESC, id DESC
    ) WHERE archived_at IS NULL;

CREATE INDEX workgroup_contribution_notifications_recipient_unread_idx
    ON community.workgroup_contribution_notifications (
        recipient_user_id, created_at DESC, id DESC
    ) WHERE read_at IS NULL AND archived_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION community.protect_workgroup_contribution_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_user_id uuid;
    source_created_at timestamptz;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'workgroup contribution notifications cannot be deleted';
    END IF;

    IF TG_OP = 'INSERT' THEN
        SELECT recipient_user_id, created_at
        INTO STRICT source_user_id, source_created_at
        FROM workgroups.contribution_reminders
        WHERE id = NEW.reminder_id;

        IF source_user_id <> NEW.recipient_user_id
           OR source_created_at <> NEW.created_at THEN
            RAISE EXCEPTION 'invalid workgroup contribution notification source';
        END IF;
        RETURN NEW;
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.recipient_user_id IS DISTINCT FROM NEW.recipient_user_id
       OR OLD.reminder_id IS DISTINCT FROM NEW.reminder_id
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'workgroup contribution notification source is immutable';
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

CREATE TRIGGER workgroup_contribution_notifications_protected
BEFORE INSERT OR UPDATE OR DELETE
ON community.workgroup_contribution_notifications
FOR EACH ROW EXECUTE FUNCTION community.protect_workgroup_contribution_notification();

-- +goose StatementBegin
CREATE FUNCTION community.project_workgroup_contribution_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO community.workgroup_contribution_notifications (
        recipient_user_id, reminder_id, created_at
    ) VALUES (
        NEW.recipient_user_id, NEW.id, NEW.created_at
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER workgroup_contribution_reminder_notification_projected
AFTER INSERT ON workgroups.contribution_reminders
FOR EACH ROW EXECUTE FUNCTION community.project_workgroup_contribution_notification();

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES (
    'workgroup.contribution.reminder.issue',
    '依据已冻结的贡献周期快照向工作组成员发送人工提醒',
    'medium', 'none', 'staff-session', true, true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'workgroup.contribution.reminder.issue'),
    ('workgroup_manager', 'workgroup.contribution.reminder.issue');

REVOKE ALL ON workgroups.contribution_reminders FROM PUBLIC;
REVOKE ALL ON community.workgroup_contribution_notifications FROM PUBLIC;

-- +goose Down

DELETE FROM authz.role_permissions
WHERE action = 'workgroup.contribution.reminder.issue';
DELETE FROM authz.permissions
WHERE action = 'workgroup.contribution.reminder.issue';

DROP TRIGGER workgroup_contribution_reminder_notification_projected
    ON workgroups.contribution_reminders;
DROP FUNCTION community.project_workgroup_contribution_notification();

DROP TRIGGER workgroup_contribution_notifications_protected
    ON community.workgroup_contribution_notifications;
DROP FUNCTION community.protect_workgroup_contribution_notification();
DROP TABLE community.workgroup_contribution_notifications;

DROP TRIGGER workgroup_contribution_reminders_immutable
    ON workgroups.contribution_reminders;
DROP TRIGGER workgroup_contribution_reminders_validated
    ON workgroups.contribution_reminders;
DROP FUNCTION workgroups.validate_contribution_reminder();
DROP TABLE workgroups.contribution_reminders;
