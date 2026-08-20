-- +goose Up

-- Attendance policies form a complete, immutable timeline.  A settlement
-- keeps the exact revision it used, so changing tomorrow's reward never
-- reinterprets an older member statement.
CREATE TABLE economy.attendance_policy_revisions (
    revision text PRIMARY KEY CHECK (
        revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    effective_from timestamptz NOT NULL UNIQUE,
    enabled boolean NOT NULL,
    day_boundary_timezone text NOT NULL CHECK (
        char_length(day_boundary_timezone) BETWEEN 1 AND 64
    ),
    fixed_enabled boolean NOT NULL,
    fixed_reward bigint NOT NULL CHECK (fixed_reward BETWEEN 0 AND 1000000),
    random_enabled boolean NOT NULL,
    random_min bigint NOT NULL CHECK (random_min BETWEEN 0 AND 1000000),
    random_max bigint NOT NULL CHECK (random_max BETWEEN 0 AND 1000000),
    streak_enabled boolean NOT NULL,
    experience_reward bigint NOT NULL CHECK (experience_reward BETWEEN 0 AND 1000000),
    snapshot_json text NOT NULL CHECK (
        octet_length(snapshot_json) BETWEEN 2 AND 16384
        AND jsonb_typeof(snapshot_json::jsonb) = 'object'
    ),
    snapshot_sha256 bytea NOT NULL CHECK (octet_length(snapshot_sha256) = 32),
    issued_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    created_at timestamptz NOT NULL,
    CHECK (created_at <= effective_from),
    CHECK (NOT enabled OR fixed_enabled OR random_enabled),
    CHECK (NOT fixed_enabled OR fixed_reward > 0),
    CHECK (NOT random_enabled OR (random_min > 0 AND random_max >= random_min)),
    CHECK (
        (issued_by IS NULL AND authorization_decision_id IS NULL)
        OR (issued_by IS NOT NULL AND authorization_decision_id IS NOT NULL)
    )
);

CREATE INDEX attendance_policy_effective_idx
    ON economy.attendance_policy_revisions (effective_from DESC, revision DESC);

CREATE TABLE economy.attendance_policy_streak_milestones (
    policy_revision text NOT NULL
        REFERENCES economy.attendance_policy_revisions (revision) ON DELETE RESTRICT,
    position smallint NOT NULL CHECK (position BETWEEN 0 AND 31),
    days integer NOT NULL CHECK (days BETWEEN 2 AND 365),
    reward bigint NOT NULL CHECK (reward BETWEEN 1 AND 1000000),
    PRIMARY KEY (policy_revision, position),
    UNIQUE (policy_revision, days)
);

-- +goose StatementBegin
CREATE FUNCTION economy.require_attendance_policy_append()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    latest_effective_from timestamptz;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('peergo-attendance-policy-v1', 0));
    SELECT max(policy.effective_from)
    INTO latest_effective_from
    FROM economy.attendance_policy_revisions AS policy;

    IF latest_effective_from IS NOT NULL
       AND NEW.effective_from <= latest_effective_from THEN
        RAISE EXCEPTION 'attendance policy timeline must append after %', latest_effective_from;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER attendance_policy_append_only
BEFORE INSERT ON economy.attendance_policy_revisions
FOR EACH ROW EXECUTE FUNCTION economy.require_attendance_policy_append();

CREATE TRIGGER attendance_policy_immutable
BEFORE UPDATE OR DELETE ON economy.attendance_policy_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER attendance_policy_streak_milestones_immutable
BEFORE UPDATE OR DELETE ON economy.attendance_policy_streak_milestones
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- Experience and magic rewards use the same signed attendance snapshot. This
-- lets the transaction writer prove that both projections came from one rule.
-- +goose StatementBegin
CREATE FUNCTION progression.open_attendance_experience_policy()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO progression.experience_policy_revisions (
        revision, source_kind, effective_from, payload_sha256, created_at
    ) VALUES (
        NEW.revision, 'activity', NEW.effective_from,
        NEW.snapshot_sha256, NEW.created_at
    );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER attendance_experience_policy_opening
AFTER INSERT ON economy.attendance_policy_revisions
FOR EACH ROW EXECUTE FUNCTION progression.open_attendance_experience_policy();

-- The compatibility baseline preserves PtYes's familiar choices while moving
-- all amounts to PeerGo's integer ledger.  Later changes must append a new row.
INSERT INTO economy.attendance_policy_revisions (
    revision, effective_from, enabled, day_boundary_timezone,
    fixed_enabled, fixed_reward, random_enabled, random_min, random_max,
    streak_enabled, experience_reward, snapshot_json, snapshot_sha256,
    issued_by, authorization_decision_id, reason, created_at
) VALUES (
    'attendance-v1',
    '2026-08-16T00:00:00Z',
    true,
    'Asia/Shanghai',
    true,
    5,
    true,
    1,
    20,
    true,
    5,
    '{"revision":"attendance-v1","effective_from":"2026-08-16T00:00:00Z","enabled":true,"day_boundary_timezone":"Asia/Shanghai","fixed_enabled":true,"fixed_reward":5,"random_enabled":true,"random_min":1,"random_max":20,"streak_enabled":true,"streak_milestones":[{"days":7,"reward":5},{"days":14,"reward":10},{"days":30,"reward":20}],"experience_reward":5}',
    decode('5654531bc468c7a49856105b37988d5fc85de5952c82f5d03ea31d963b6e4eda', 'hex'),
    NULL,
    NULL,
    'PeerGo 首版签到兼容基线',
    '2026-08-16T00:00:00Z'
);

INSERT INTO economy.attendance_policy_streak_milestones (
    policy_revision, position, days, reward
) VALUES
    ('attendance-v1', 0, 7, 5),
    ('attendance-v1', 1, 14, 10),
    ('attendance-v1', 2, 30, 20);

-- A claim is the immutable receipt joining the selected policy, the balanced
-- magic transaction, and the optional experience entry.  Daily uniqueness is
-- enforced in the database as the final concurrency boundary.
CREATE TABLE economy.attendance_records (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    attendance_date date NOT NULL,
    day_boundary_timezone text NOT NULL CHECK (
        char_length(day_boundary_timezone) BETWEEN 1 AND 64
    ),
    mode text NOT NULL CHECK (mode IN ('fixed', 'random')),
    base_reward bigint NOT NULL CHECK (base_reward > 0),
    streak_reward bigint NOT NULL CHECK (streak_reward >= 0),
    total_reward bigint NOT NULL CHECK (
        total_reward > 0 AND total_reward = base_reward + streak_reward
    ),
    experience_reward bigint NOT NULL CHECK (experience_reward >= 0),
    current_streak integer NOT NULL CHECK (current_streak BETWEEN 1 AND 1000000),
    total_days integer NOT NULL CHECK (total_days BETWEEN 1 AND 1000000),
    longest_streak integer NOT NULL CHECK (
        longest_streak BETWEEN current_streak AND 1000000
    ),
    policy_revision text NOT NULL
        REFERENCES economy.attendance_policy_revisions (revision) ON DELETE RESTRICT,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    magic_transaction_id uuid NOT NULL UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    experience_entry_id uuid UNIQUE
        REFERENCES progression.experience_entries (id) ON DELETE RESTRICT,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    UNIQUE (user_id, attendance_date),
    CHECK (recorded_at >= occurred_at),
    CHECK ((experience_reward = 0) = (experience_entry_id IS NULL))
);

CREATE INDEX attendance_records_user_date_idx
    ON economy.attendance_records (user_id, attendance_date DESC, recorded_at DESC);

CREATE TRIGGER attendance_records_immutable
BEFORE UPDATE OR DELETE ON economy.attendance_records
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('economy.attendance.claim.self', '完成自己的每日签到并领取魔力值与经验', 'medium', 'self', 'web-session', true, true),
    ('economy.attendance.read.self', '查看自己的签到状态与历史', 'low', 'self', 'web-session', true, true),
    ('economy.attendance.policy.issue', '签发未来生效的不可变签到政策', 'high', 'none', 'staff-session', true, true),
    ('economy.attendance.policy.read', '读取签到政策时间线', 'medium', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'economy.attendance.claim.self'),
    ('member', 'economy.attendance.read.self'),
    ('site_admin', 'economy.attendance.policy.issue'),
    ('site_admin', 'economy.attendance.policy.read');

REVOKE ALL ON economy.attendance_policy_revisions FROM PUBLIC;
REVOKE ALL ON economy.attendance_policy_streak_milestones FROM PUBLIC;
REVOKE ALL ON economy.attendance_records FROM PUBLIC;

-- +goose Down

DELETE FROM authz.role_permissions
WHERE (role_id = 'member' AND action IN (
        'economy.attendance.claim.self',
        'economy.attendance.read.self'
    ))
   OR (role_id = 'site_admin' AND action IN (
        'economy.attendance.policy.issue',
        'economy.attendance.policy.read'
    ));

DELETE FROM authz.permissions
WHERE action IN (
    'economy.attendance.claim.self',
    'economy.attendance.read.self',
    'economy.attendance.policy.issue',
    'economy.attendance.policy.read'
);

DROP TRIGGER attendance_records_immutable ON economy.attendance_records;
DROP INDEX economy.attendance_records_user_date_idx;
DROP TABLE economy.attendance_records;
DROP TRIGGER attendance_experience_policy_opening ON economy.attendance_policy_revisions;
DROP FUNCTION progression.open_attendance_experience_policy();
DROP TRIGGER attendance_policy_streak_milestones_immutable
    ON economy.attendance_policy_streak_milestones;
DROP TRIGGER attendance_policy_immutable ON economy.attendance_policy_revisions;
DROP TRIGGER attendance_policy_append_only ON economy.attendance_policy_revisions;
DROP FUNCTION economy.require_attendance_policy_append();
DROP TABLE economy.attendance_policy_streak_milestones;
DROP INDEX economy.attendance_policy_effective_idx;
DROP TABLE economy.attendance_policy_revisions;
