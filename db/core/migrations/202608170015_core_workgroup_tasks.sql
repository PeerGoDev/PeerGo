-- +goose Up

-- Workgroup tasks are deliberately narrower than a general project-management
-- system. A publication targets exactly one of PeerGo's three code-owned
-- groups, and the members who were active at publication time are frozen in
-- the same transaction. Later membership changes never rewrite that audience.
CREATE TABLE workgroups.tasks (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE,
    group_kind text NOT NULL
        REFERENCES workgroups.definitions (kind) ON DELETE RESTRICT,
    task_type text NOT NULL CHECK (task_type IN ('task', 'activity')),
    title text NOT NULL CHECK (char_length(btrim(title)) BETWEEN 2 AND 100),
    description text NOT NULL
        CHECK (char_length(btrim(description)) BETWEEN 10 AND 2000),
    starts_at timestamptz NOT NULL,
    due_at timestamptz NOT NULL,
    issued_by uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    CHECK (starts_at >= created_at),
    CHECK (due_at > starts_at),
    CHECK (due_at <= starts_at + interval '366 days')
);

CREATE INDEX workgroup_tasks_group_recent_idx
    ON workgroups.tasks (group_kind, starts_at DESC, id DESC);
CREATE INDEX workgroup_tasks_due_idx
    ON workgroups.tasks (due_at, id);

CREATE TABLE workgroups.task_assignments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id uuid NOT NULL REFERENCES workgroups.tasks (id) ON DELETE RESTRICT,
    membership_id uuid NOT NULL
        REFERENCES workgroups.memberships (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    assigned_at timestamptz NOT NULL,
    UNIQUE (task_id, membership_id),
    UNIQUE (task_id, user_id)
);

CREATE INDEX workgroup_task_assignments_user_recent_idx
    ON workgroups.task_assignments (user_id, assigned_at DESC, id DESC);

-- Submissions are append-only attempts. A rejected attempt may be followed by
-- another sequence before the deadline; pending and accepted attempts are
-- protected by the repository transaction and the unique sequence fence.
CREATE TABLE workgroups.task_submissions (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE,
    assignment_id uuid NOT NULL
        REFERENCES workgroups.task_assignments (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    sequence bigint NOT NULL CHECK (sequence > 0),
    statement text NOT NULL
        CHECK (char_length(btrim(statement)) BETWEEN 10 AND 2000),
    authorization_decision_id uuid NOT NULL,
    submitted_at timestamptz NOT NULL,
    UNIQUE (assignment_id, sequence)
);

CREATE INDEX workgroup_task_submissions_assignment_recent_idx
    ON workgroups.task_submissions (assignment_id, sequence DESC);

CREATE TABLE workgroups.task_reviews (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE,
    submission_id uuid NOT NULL UNIQUE
        REFERENCES workgroups.task_submissions (id) ON DELETE RESTRICT,
    decision text NOT NULL CHECK (decision IN ('accepted', 'rejected')),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    decided_by uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid NOT NULL,
    decided_at timestamptz NOT NULL
);

CREATE INDEX workgroup_task_reviews_decided_recent_idx
    ON workgroups.task_reviews (decided_at DESC, id DESC);

-- +goose StatementBegin
CREATE FUNCTION workgroups.validate_task_assignment()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_group_kind text;
    source_user_id uuid;
    source_status text;
    task_group_kind text;
    task_created_at timestamptz;
BEGIN
    SELECT group_kind, user_id, status
    INTO STRICT source_group_kind, source_user_id, source_status
    FROM workgroups.memberships
    WHERE id = NEW.membership_id;

    SELECT group_kind, created_at
    INTO STRICT task_group_kind, task_created_at
    FROM workgroups.tasks
    WHERE id = NEW.task_id;

    IF source_group_kind <> task_group_kind
       OR source_user_id <> NEW.user_id
       OR source_status <> 'active'
       OR task_created_at <> NEW.assigned_at THEN
        RAISE EXCEPTION 'invalid workgroup task assignment';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER workgroup_task_assignments_validated
BEFORE INSERT ON workgroups.task_assignments
FOR EACH ROW EXECUTE FUNCTION workgroups.validate_task_assignment();

-- +goose StatementBegin
CREATE FUNCTION workgroups.validate_task_submission()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_user_id uuid;
    source_status text;
    source_starts_at timestamptz;
    source_due_at timestamptz;
BEGIN
    SELECT assignment.user_id, membership.status,
           task.starts_at, task.due_at
    INTO STRICT source_user_id, source_status,
                source_starts_at, source_due_at
    FROM workgroups.task_assignments AS assignment
    JOIN workgroups.memberships AS membership
      ON membership.id = assignment.membership_id
    JOIN workgroups.tasks AS task ON task.id = assignment.task_id
    WHERE assignment.id = NEW.assignment_id;

    IF source_user_id <> NEW.user_id
       OR source_status <> 'active'
       OR NEW.submitted_at < source_starts_at
       OR NEW.submitted_at > source_due_at THEN
        RAISE EXCEPTION 'invalid workgroup task submission';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER workgroup_task_submissions_validated
BEFORE INSERT ON workgroups.task_submissions
FOR EACH ROW EXECUTE FUNCTION workgroups.validate_task_submission();

CREATE TRIGGER workgroup_tasks_immutable
BEFORE UPDATE OR DELETE ON workgroups.tasks
FOR EACH ROW EXECUTE FUNCTION workgroups.reject_history_mutation();
CREATE TRIGGER workgroup_task_assignments_immutable
BEFORE UPDATE OR DELETE ON workgroups.task_assignments
FOR EACH ROW EXECUTE FUNCTION workgroups.reject_history_mutation();
CREATE TRIGGER workgroup_task_submissions_immutable
BEFORE UPDATE OR DELETE ON workgroups.task_submissions
FOR EACH ROW EXECUTE FUNCTION workgroups.reject_history_mutation();
CREATE TRIGGER workgroup_task_reviews_immutable
BEFORE UPDATE OR DELETE ON workgroups.task_reviews
FOR EACH ROW EXECUTE FUNCTION workgroups.reject_history_mutation();

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('workgroup.task.publish', '向一个固定工作组发布任务或活动并冻结成员名单', 'high', 'none', 'staff-session', true, true),
    ('workgroup.task.review', '人工验收工作组成员提交的任务成果', 'medium', 'none', 'staff-session', true, true),
    ('workgroup.task.submit.self', '提交自己被分配的工作组任务成果', 'medium', 'self', 'web-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'workgroup.task.submit.self'),
    ('site_admin', 'workgroup.task.publish'),
    ('site_admin', 'workgroup.task.review'),
    ('workgroup_manager', 'workgroup.task.publish'),
    ('workgroup_manager', 'workgroup.task.review');

REVOKE ALL ON workgroups.tasks FROM PUBLIC;
REVOKE ALL ON workgroups.task_assignments FROM PUBLIC;
REVOKE ALL ON workgroups.task_submissions FROM PUBLIC;
REVOKE ALL ON workgroups.task_reviews FROM PUBLIC;

-- +goose Down

DELETE FROM authz.role_permissions
WHERE action IN (
    'workgroup.task.publish',
    'workgroup.task.review',
    'workgroup.task.submit.self'
);
DELETE FROM authz.permissions
WHERE action IN (
    'workgroup.task.publish',
    'workgroup.task.review',
    'workgroup.task.submit.self'
);

DROP TRIGGER workgroup_task_reviews_immutable ON workgroups.task_reviews;
DROP TRIGGER workgroup_task_submissions_immutable ON workgroups.task_submissions;
DROP TRIGGER workgroup_task_assignments_immutable ON workgroups.task_assignments;
DROP TRIGGER workgroup_tasks_immutable ON workgroups.tasks;
DROP TRIGGER workgroup_task_submissions_validated ON workgroups.task_submissions;
DROP FUNCTION workgroups.validate_task_submission();
DROP TRIGGER workgroup_task_assignments_validated ON workgroups.task_assignments;
DROP FUNCTION workgroups.validate_task_assignment();
DROP TABLE workgroups.task_reviews;
DROP TABLE workgroups.task_submissions;
DROP TABLE workgroups.task_assignments;
DROP TABLE workgroups.tasks;
