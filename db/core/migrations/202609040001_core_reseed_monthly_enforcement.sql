-- +goose Up

-- Restore the PtYes reseed-group rule without restoring its month-boundary
-- bug.  Policies stay append-only, while one compact assessment row freezes
-- the evidence and cumulative miss count for each completed calendar month.
ALTER TABLE workgroups.contribution_policy_revisions
    ADD COLUMN allowed_misses smallint NOT NULL DEFAULT 0
        CHECK (allowed_misses BETWEEN 0 AND 120),
    DROP CONSTRAINT contribution_policy_revisions_enforcement_mode_check,
    DROP CONSTRAINT contribution_policy_revisions_source_kind_check,
    DROP CONSTRAINT contribution_policy_revisions_check1,
    DROP CONSTRAINT workgroup_contribution_policy_staff_evidence_ck,
    ADD CONSTRAINT contribution_policy_revisions_enforcement_mode_check CHECK (
        (enforcement_mode = 'observe' AND allowed_misses = 0)
        OR (
            enforcement_mode = 'miss_limit'
            AND group_kind = 'reseed'
            AND allowed_misses > 0
        )
    ),
    ADD CONSTRAINT contribution_policy_revisions_source_kind_check CHECK (
        source_kind IN ('cutover_opening', 'staff', 'legacy_restoration')
    ),
    ADD CONSTRAINT contribution_policy_revisions_check1 CHECK (
        (
            source_kind = 'cutover_opening'
            AND revision = 1
            AND effective_from = '-infinity'::timestamptz
            AND authorization_decision_id IS NULL
        ) OR (
            source_kind = 'staff'
            AND effective_from = date_trunc('month', effective_from)
            AND authorization_decision_id IS NOT NULL
        ) OR (
            source_kind = 'legacy_restoration'
            AND group_kind = 'reseed'
            AND metric = 'trusted_torrents_published'
            AND target_value = 40
            AND enforcement_mode = 'miss_limit'
            AND allowed_misses = 3
            AND effective_from = '2026-09-01 00:00:00+00'::timestamptz
            AND authorization_decision_id IS NULL
        )
    ),
    ADD CONSTRAINT workgroup_contribution_policy_staff_evidence_ck CHECK (
        (
            source_kind = 'cutover_opening'
            AND request_id IS NULL
            AND issued_by IS NULL
            AND reason IS NULL
        ) OR (
            source_kind = 'staff'
            AND request_id IS NOT NULL
            AND issued_by IS NOT NULL
            AND char_length(btrim(reason)) BETWEEN 10 AND 1000
        ) OR (
            source_kind = 'legacy_restoration'
            AND request_id IS NULL
            AND issued_by IS NULL
            AND char_length(btrim(reason)) BETWEEN 10 AND 1000
        )
    );

INSERT INTO workgroups.contribution_policy_revisions (
    group_kind, revision, metric, period_kind, target_value,
    enforcement_mode, allowed_misses, effective_from, source_kind,
    source_reference, reason, created_at
) VALUES (
    'reseed', 2, 'trusted_torrents_published', 'calendar_month', 40,
    'miss_limit', 3, '2026-09-01 00:00:00+00', 'legacy_restoration',
    'legacy-restoration:ptyes-reseed-40-v1',
    '恢复 PtYes 转种组每月 40 个有效转种要求；前三次未达标仅标记，第四次自动结束资格。',
    now()
);

CREATE TABLE workgroups.contribution_assessments (
    id uuid PRIMARY KEY,
    membership_id uuid NOT NULL
        REFERENCES workgroups.memberships (id) ON DELETE RESTRICT,
    tenure_transition_id uuid NOT NULL
        REFERENCES workgroups.membership_transitions (id) ON DELETE RESTRICT,
    group_kind text NOT NULL CHECK (group_kind = 'reseed'),
    recipient_user_id uuid NOT NULL
        REFERENCES identity.users (id) ON DELETE RESTRICT,
    period_starts_at timestamptz NOT NULL,
    period_ends_at timestamptz NOT NULL,
    metric text NOT NULL CHECK (metric = 'trusted_torrents_published'),
    policy_revision bigint NOT NULL,
    observed_at timestamptz NOT NULL,
    evidence_through timestamptz NOT NULL,
    evidence_state text NOT NULL CHECK (evidence_state = 'complete'),
    current_value bigint NOT NULL CHECK (current_value >= 0),
    target_value bigint NOT NULL CHECK (target_value > 0),
    assessment_state text NOT NULL CHECK (assessment_state IN ('met', 'not_met')),
    explanation_code text NOT NULL CHECK (explanation_code IN (
        'target_met', 'below_target', 'no_contribution'
    )),
    miss_count smallint NOT NULL CHECK (miss_count BETWEEN 0 AND 120),
    allowed_misses smallint NOT NULL CHECK (allowed_misses BETWEEN 1 AND 120),
    disciplinary_action text NOT NULL CHECK (disciplinary_action IN (
        'none', 'marked', 'membership_ended'
    )),
    membership_transition_id uuid UNIQUE
        REFERENCES workgroups.membership_transitions (id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    assessed_at timestamptz NOT NULL,
    UNIQUE (membership_id, period_starts_at),
    FOREIGN KEY (group_kind, policy_revision)
        REFERENCES workgroups.contribution_policy_revisions (group_kind, revision)
        ON DELETE RESTRICT,
    CHECK (period_starts_at = date_trunc('month', period_starts_at)),
    CHECK (period_ends_at = period_starts_at + interval '1 month'),
    CHECK (observed_at = period_ends_at),
    CHECK (evidence_through = period_ends_at),
    CHECK (assessed_at >= observed_at),
    CHECK (
        (assessment_state = 'met'
            AND explanation_code = 'target_met'
            AND current_value >= target_value
            AND disciplinary_action = 'none'
            AND membership_transition_id IS NULL)
        OR (
            assessment_state = 'not_met'
            AND explanation_code IN ('below_target', 'no_contribution')
            AND current_value < target_value
            AND miss_count > 0
            AND (
                (miss_count <= allowed_misses
                    AND disciplinary_action = 'marked'
                    AND membership_transition_id IS NULL)
                OR (miss_count > allowed_misses
                    AND disciplinary_action = 'membership_ended'
                    AND membership_transition_id IS NOT NULL)
            )
        )
    )
);

CREATE INDEX contribution_assessments_member_tenure_idx
    ON workgroups.contribution_assessments (
        membership_id, tenure_transition_id, period_starts_at DESC
    );

-- +goose StatementBegin
CREATE FUNCTION workgroups.validate_contribution_assessment()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_group_kind text;
    source_user_id uuid;
    tenure_membership_id uuid;
    tenure_to_status text;
    tenure_occurred_at timestamptz;
    policy_metric text;
    policy_target bigint;
    policy_mode text;
    policy_allowed_misses smallint;
    automatic_membership_id uuid;
    automatic_to_status text;
    automatic_source text;
BEGIN
    SELECT group_kind, user_id
    INTO STRICT source_group_kind, source_user_id
    FROM workgroups.memberships
    WHERE id = NEW.membership_id;

    SELECT membership_id, to_status, occurred_at
    INTO STRICT tenure_membership_id, tenure_to_status, tenure_occurred_at
    FROM workgroups.membership_transitions
    WHERE id = NEW.tenure_transition_id;

    SELECT metric, target_value, enforcement_mode, allowed_misses
    INTO STRICT policy_metric, policy_target, policy_mode, policy_allowed_misses
    FROM workgroups.contribution_policy_revisions
    WHERE group_kind = NEW.group_kind AND revision = NEW.policy_revision;

    IF source_group_kind <> NEW.group_kind
       OR source_user_id <> NEW.recipient_user_id
       OR tenure_membership_id <> NEW.membership_id
       OR tenure_to_status <> 'active'
       OR tenure_occurred_at > NEW.period_starts_at
       OR policy_metric <> NEW.metric
       OR policy_target <> NEW.target_value
       OR policy_mode <> 'miss_limit'
       OR policy_allowed_misses <> NEW.allowed_misses THEN
        RAISE EXCEPTION 'invalid workgroup contribution assessment source';
    END IF;

    IF NEW.membership_transition_id IS NOT NULL THEN
        SELECT membership_id, to_status, source
        INTO STRICT automatic_membership_id, automatic_to_status, automatic_source
        FROM workgroups.membership_transitions
        WHERE id = NEW.membership_transition_id;
        IF automatic_membership_id <> NEW.membership_id
           OR automatic_to_status <> 'ended'
           OR automatic_source <> 'automatic_contribution' THEN
            RAISE EXCEPTION 'invalid automatic contribution transition';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER contribution_assessments_validated
BEFORE INSERT ON workgroups.contribution_assessments
FOR EACH ROW EXECUTE FUNCTION workgroups.validate_contribution_assessment();

CREATE TRIGGER contribution_assessments_immutable
BEFORE UPDATE OR DELETE ON workgroups.contribution_assessments
FOR EACH ROW EXECUTE FUNCTION workgroups.reject_history_mutation();

ALTER TABLE workgroups.membership_transitions
    DROP CONSTRAINT membership_transitions_source_check,
    DROP CONSTRAINT membership_transitions_source_authority_check,
    ADD CONSTRAINT membership_transitions_source_check CHECK (
        source IN ('application', 'staff', 'legacy_migration', 'automatic_contribution')
    ),
    ADD CONSTRAINT membership_transitions_source_authority_check CHECK (
        (
            source = 'legacy_migration'
            AND actor_id IS NULL
            AND authorization_decision_id IS NULL
            AND source_application_id IS NULL
            AND (
                (transition = 'joined' AND from_status IS NULL
                    AND to_status = 'active' AND state_version = 1)
                OR (transition = 'suspended' AND from_status = 'active'
                    AND to_status = 'suspended' AND state_version = 2)
                OR (transition = 'ended' AND from_status = 'active'
                    AND to_status = 'ended' AND state_version = 2)
            )
        ) OR (
            source IN ('application', 'staff')
            AND actor_id IS NOT NULL
            AND authorization_decision_id IS NOT NULL
        ) OR (
            source = 'automatic_contribution'
            AND group_kind = 'reseed'
            AND transition = 'ended'
            AND from_status = 'active'
            AND to_status = 'ended'
            AND actor_id IS NULL
            AND authorization_decision_id IS NULL
            AND source_application_id IS NULL
            AND state_version > 1
        )
    );

CREATE TABLE workgroups.contribution_enforcement_worker_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    last_started_at timestamptz,
    last_completed_at timestamptz,
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    last_examined integer NOT NULL DEFAULT 0 CHECK (last_examined >= 0),
    last_recorded integer NOT NULL DEFAULT 0 CHECK (last_recorded >= 0),
    last_marked integer NOT NULL DEFAULT 0 CHECK (last_marked >= 0),
    last_ended integer NOT NULL DEFAULT 0 CHECK (last_ended >= 0),
    run_count bigint NOT NULL DEFAULT 0 CHECK (run_count >= 0)
);

INSERT INTO workgroups.contribution_enforcement_worker_state (singleton)
VALUES (true);

-- Reuse the typed private inbox. A notification now binds to exactly one
-- manual reminder or one automatic monthly assessment.
DROP TRIGGER workgroup_contribution_notifications_protected
    ON community.workgroup_contribution_notifications;
DROP FUNCTION community.protect_workgroup_contribution_notification();

ALTER TABLE community.workgroup_contribution_notifications
    ALTER COLUMN reminder_id DROP NOT NULL,
    ADD COLUMN assessment_id uuid UNIQUE
        REFERENCES workgroups.contribution_assessments (id) ON DELETE RESTRICT,
    ADD CONSTRAINT workgroup_contribution_notification_source_ck CHECK (
        num_nonnulls(reminder_id, assessment_id) = 1
    );

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
        IF NEW.reminder_id IS NOT NULL THEN
            SELECT recipient_user_id, created_at
            INTO STRICT source_user_id, source_created_at
            FROM workgroups.contribution_reminders
            WHERE id = NEW.reminder_id;
        ELSE
            SELECT recipient_user_id, assessed_at
            INTO STRICT source_user_id, source_created_at
            FROM workgroups.contribution_assessments
            WHERE id = NEW.assessment_id;
        END IF;
        IF source_user_id <> NEW.recipient_user_id
           OR source_created_at <> NEW.created_at THEN
            RAISE EXCEPTION 'invalid workgroup contribution notification source';
        END IF;
        RETURN NEW;
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
       OR OLD.recipient_user_id IS DISTINCT FROM NEW.recipient_user_id
       OR OLD.reminder_id IS DISTINCT FROM NEW.reminder_id
       OR OLD.assessment_id IS DISTINCT FROM NEW.assessment_id
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
CREATE FUNCTION community.project_workgroup_contribution_assessment_notification()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.assessment_state = 'not_met' THEN
        INSERT INTO community.workgroup_contribution_notifications (
            recipient_user_id, assessment_id, created_at
        ) VALUES (
            NEW.recipient_user_id, NEW.id, NEW.assessed_at
        );
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER workgroup_contribution_assessment_notification_projected
AFTER INSERT ON workgroups.contribution_assessments
FOR EACH ROW EXECUTE FUNCTION community.project_workgroup_contribution_assessment_notification();

REVOKE ALL ON workgroups.contribution_assessments FROM PUBLIC;
REVOKE ALL ON workgroups.contribution_enforcement_worker_state FROM PUBLIC;

-- +goose Down

DROP TRIGGER workgroup_contribution_assessment_notification_projected
    ON workgroups.contribution_assessments;
DROP FUNCTION community.project_workgroup_contribution_assessment_notification();

DROP TRIGGER workgroup_contribution_notifications_protected
    ON community.workgroup_contribution_notifications;
DROP FUNCTION community.protect_workgroup_contribution_notification();

DELETE FROM community.workgroup_contribution_notifications
WHERE assessment_id IS NOT NULL;

ALTER TABLE community.workgroup_contribution_notifications
    DROP CONSTRAINT workgroup_contribution_notification_source_ck,
    DROP COLUMN assessment_id,
    ALTER COLUMN reminder_id SET NOT NULL;

-- Restore the original reminder-only protection function.
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

DROP TABLE workgroups.contribution_enforcement_worker_state;

ALTER TABLE workgroups.membership_transitions
    DROP CONSTRAINT membership_transitions_source_authority_check,
    DROP CONSTRAINT membership_transitions_source_check,
    ADD CONSTRAINT membership_transitions_source_check CHECK (
        source IN ('application', 'staff', 'legacy_migration')
    ),
    ADD CONSTRAINT membership_transitions_source_authority_check CHECK (
        (
            source = 'legacy_migration'
            AND actor_id IS NULL
            AND authorization_decision_id IS NULL
            AND source_application_id IS NULL
            AND (
                (transition = 'joined' AND from_status IS NULL
                    AND to_status = 'active' AND state_version = 1)
                OR (transition = 'suspended' AND from_status = 'active'
                    AND to_status = 'suspended' AND state_version = 2)
                OR (transition = 'ended' AND from_status = 'active'
                    AND to_status = 'ended' AND state_version = 2)
            )
        ) OR (
            source IN ('application', 'staff')
            AND actor_id IS NOT NULL
            AND authorization_decision_id IS NOT NULL
        )
    );

DROP TRIGGER contribution_assessments_immutable
    ON workgroups.contribution_assessments;
DROP TRIGGER contribution_assessments_validated
    ON workgroups.contribution_assessments;
DROP FUNCTION workgroups.validate_contribution_assessment();
DROP TABLE workgroups.contribution_assessments;

ALTER TABLE workgroups.contribution_policy_revisions
    DISABLE TRIGGER workgroup_contribution_policy_immutable;
DELETE FROM workgroups.contribution_policy_revisions
WHERE source_reference = 'legacy-restoration:ptyes-reseed-40-v1';
ALTER TABLE workgroups.contribution_policy_revisions
    ENABLE TRIGGER workgroup_contribution_policy_immutable;

ALTER TABLE workgroups.contribution_policy_revisions
    DROP CONSTRAINT workgroup_contribution_policy_staff_evidence_ck,
    DROP CONSTRAINT contribution_policy_revisions_check1,
    DROP CONSTRAINT contribution_policy_revisions_source_kind_check,
    DROP CONSTRAINT contribution_policy_revisions_enforcement_mode_check,
    DROP COLUMN allowed_misses,
    ADD CONSTRAINT contribution_policy_revisions_enforcement_mode_check CHECK (
        enforcement_mode IN ('observe')
    ),
    ADD CONSTRAINT contribution_policy_revisions_source_kind_check CHECK (
        source_kind IN ('cutover_opening', 'staff')
    ),
    ADD CONSTRAINT contribution_policy_revisions_check1 CHECK (
        (source_kind = 'cutover_opening'
            AND revision = 1
            AND effective_from = '-infinity'::timestamptz
            AND authorization_decision_id IS NULL)
        OR (source_kind = 'staff'
            AND effective_from = date_trunc('month', effective_from)
            AND authorization_decision_id IS NOT NULL)
    ),
    ADD CONSTRAINT workgroup_contribution_policy_staff_evidence_ck CHECK (
        (source_kind = 'cutover_opening'
            AND request_id IS NULL
            AND issued_by IS NULL
            AND reason IS NULL)
        OR (source_kind = 'staff'
            AND request_id IS NOT NULL
            AND issued_by IS NOT NULL
            AND char_length(btrim(reason)) BETWEEN 10 AND 1000)
    );
