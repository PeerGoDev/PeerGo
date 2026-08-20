-- +goose Up

INSERT INTO authz.permissions (
    action, description, risk_level, relationship, credential_audience,
    grantable, discoverable
) VALUES
    ('progression.level.policy.read', '查看经验等级规则时间线和用户分布', 'medium', 'none', 'staff-session', true, true),
    ('progression.level.policy.issue', '签发未来生效的经验等级规则', 'high', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'progression.level.policy.read'),
    ('site_admin', 'progression.level.policy.issue');

-- A policy header makes the existing immutable definitions a real effective
-- timeline. A complete ladder and all of its doing-seed benefits are signed as
-- one canonical snapshot; partial edits can never produce a mixed version.
CREATE TABLE progression.level_policy_revisions (
    policy_version text PRIMARY KEY CHECK (
        policy_version ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    sequence bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    request_id uuid UNIQUE,
    effective_at timestamptz NOT NULL UNIQUE,
    snapshot_json jsonb NOT NULL,
    snapshot_sha256 bytea NOT NULL CHECK (octet_length(snapshot_sha256) = 32),
    issued_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    created_at timestamptz NOT NULL,
    CHECK (
        (
            request_id IS NULL
            AND issued_by IS NULL
            AND authorization_decision_id IS NULL
        ) OR (
            request_id IS NOT NULL
            AND issued_by IS NOT NULL
            AND authorization_decision_id IS NOT NULL
            AND created_at < effective_at
        )
    )
);

CREATE INDEX level_policy_effective_idx
    ON progression.level_policy_revisions (effective_at DESC, sequence DESC);

CREATE TRIGGER progression_level_policy_revisions_immutable
BEFORE UPDATE OR DELETE ON progression.level_policy_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO progression.level_policy_revisions (
    policy_version, effective_at, snapshot_json, snapshot_sha256,
    reason, created_at
) VALUES (
    'rousi-v1',
    '1970-01-01 00:00:00+00',
    '{"levels":[{"level":1,"minimum_experience":"0","karma_bonus_bps":0,"seeding_count_bonus":0},{"level":2,"minimum_experience":"1000","karma_bonus_bps":200,"seeding_count_bonus":0},{"level":3,"minimum_experience":"5000","karma_bonus_bps":400,"seeding_count_bonus":5},{"level":4,"minimum_experience":"15000","karma_bonus_bps":600,"seeding_count_bonus":10},{"level":5,"minimum_experience":"40000","karma_bonus_bps":800,"seeding_count_bonus":15},{"level":6,"minimum_experience":"100000","karma_bonus_bps":1000,"seeding_count_bonus":20},{"level":7,"minimum_experience":"250000","karma_bonus_bps":1300,"seeding_count_bonus":30},{"level":8,"minimum_experience":"600000","karma_bonus_bps":1600,"seeding_count_bonus":40},{"level":9,"minimum_experience":"1200000","karma_bonus_bps":2000,"seeding_count_bonus":55}]}'::jsonb,
    decode('e0658b047616a2dea6f5e1b7d55429b5460ae341f4e351eff8f66b7336c262d9', 'hex'),
    'Rousi 迁移等级、经验门槛与做种权益基线。',
    '1970-01-01 00:00:00+00'
);

ALTER TABLE progression.level_definitions
    ADD CONSTRAINT level_definitions_policy_revision_fk
        FOREIGN KEY (policy_version)
        REFERENCES progression.level_policy_revisions (policy_version)
        ON DELETE RESTRICT;

ALTER TABLE progression.seeding_reward_level_benefits
    ADD CONSTRAINT seeding_reward_level_benefits_policy_revision_fk
        FOREIGN KEY (policy_version)
        REFERENCES progression.level_policy_revisions (policy_version)
        ON DELETE RESTRICT;

-- Activation is a projection change, not experience income. It gets its own
-- immutable evidence instead of inventing a zero-value experience entry.
CREATE TABLE progression.level_policy_activation_entries (
    policy_version text NOT NULL
        REFERENCES progression.level_policy_revisions (policy_version)
        ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    experience numeric(38, 20) NOT NULL CHECK (experience >= 0),
    from_policy_version text NOT NULL,
    from_level smallint NOT NULL,
    to_level smallint NOT NULL,
    applied_at timestamptz NOT NULL,
    PRIMARY KEY (policy_version, user_id),
    FOREIGN KEY (from_policy_version, from_level)
        REFERENCES progression.level_definitions (policy_version, level)
        ON DELETE RESTRICT,
    FOREIGN KEY (policy_version, to_level)
        REFERENCES progression.level_definitions (policy_version, level)
        ON DELETE RESTRICT
);

CREATE TRIGGER progression_level_policy_activation_entries_immutable
BEFORE UPDATE OR DELETE ON progression.level_policy_activation_entries
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TABLE progression.level_policy_activation_runs (
    policy_version text PRIMARY KEY
        REFERENCES progression.level_policy_revisions (policy_version)
        ON DELETE RESTRICT,
    affected_user_count bigint NOT NULL CHECK (affected_user_count >= 0),
    changed_level_count bigint NOT NULL CHECK (
        changed_level_count BETWEEN 0 AND affected_user_count
    ),
    applied_at timestamptz NOT NULL
);

CREATE TRIGGER progression_level_policy_activation_runs_immutable
BEFORE UPDATE OR DELETE ON progression.level_policy_activation_runs
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- Existing users already use the baseline projection, so the migration marks
-- that revision applied without fabricating one activation row per account.
INSERT INTO progression.level_policy_activation_runs (
    policy_version, affected_user_count, changed_level_count, applied_at
)
SELECT 'rousi-v1', count(*), 0, '1970-01-01 00:00:00+00'
FROM progression.user_progress;

REVOKE ALL ON progression.level_policy_revisions FROM PUBLIC;
REVOKE ALL ON progression.level_policy_activation_entries FROM PUBLIC;
REVOKE ALL ON progression.level_policy_activation_runs FROM PUBLIC;

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM progression.level_policy_revisions
        WHERE policy_version <> 'rousi-v1'
    ) THEN
        RAISE EXCEPTION '202608180015 cannot roll back after a level policy was issued';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER progression_level_policy_activation_runs_immutable
    ON progression.level_policy_activation_runs;
DROP TABLE progression.level_policy_activation_runs;
DROP TRIGGER progression_level_policy_activation_entries_immutable
    ON progression.level_policy_activation_entries;
DROP TABLE progression.level_policy_activation_entries;
ALTER TABLE progression.seeding_reward_level_benefits
    DROP CONSTRAINT seeding_reward_level_benefits_policy_revision_fk;
ALTER TABLE progression.level_definitions
    DROP CONSTRAINT level_definitions_policy_revision_fk;
DROP TRIGGER progression_level_policy_revisions_immutable
    ON progression.level_policy_revisions;
DROP INDEX progression.level_policy_effective_idx;
DROP TABLE progression.level_policy_revisions;
DELETE FROM authz.role_permissions
WHERE action IN (
    'progression.level.policy.read',
    'progression.level.policy.issue'
);
DELETE FROM authz.permissions
WHERE action IN (
    'progression.level.policy.read',
    'progression.level.policy.issue'
);
