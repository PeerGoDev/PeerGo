-- +goose Up

-- System baselines are migration-owned immutable policy revisions.  They have
-- no human issuer or authorization decision; every later administrator change
-- still requires the normal staff authorization path and retains its issuer.
ALTER TABLE economy.seeding_reward_policy_revisions
    ALTER COLUMN issued_by DROP NOT NULL,
    ALTER COLUMN authorization_decision_id DROP NOT NULL,
    ADD CONSTRAINT seeding_reward_policy_issuer_pair CHECK (
        (issued_by IS NULL AND authorization_decision_id IS NULL)
        OR (issued_by IS NOT NULL AND authorization_decision_id IS NOT NULL)
    );

-- Rousi's live formula as captured at the migration cutover.  This row makes
-- a restored site operational without requiring an administrator to discover
-- and manually reproduce twenty interdependent parameters before Tracker
-- evidence can settle.
INSERT INTO economy.seeding_reward_policy_revisions (
    revision, formula_version, effective_from,
    curve_hourly_cap_milli, age_saturation_seconds, seeder_decay,
    curve_scale_milli, size_multiplier_bps, official_bonus_bps,
    upload_contribution_bonus_bps, per_torrent_hourly_milli,
    base_linear_torrent_limit, maximum_level_torrent_bonus,
    minimum_torrent_bytes, minimum_active_seconds,
    maximum_snapshot_age_seconds, vip_bonus_bps,
    maximum_medal_bonus_bps, maximum_level_bonus_bps,
    maximum_hourly_reward, experience_per_magic_bps,
    snapshot_json, snapshot_sha256, issued_by,
    authorization_decision_id, reason, created_at
)
SELECT
    'rousi-reward-v1', 'nexus-atan-active-v1', '2026-08-20T00:00:00Z',
    100000, 2419200, 7,
    300000, 10000, 10000,
    5000, 500,
    60, 55,
    52428800, 300,
    600, 2000,
    10000, 2000,
    500, 200,
    '{"revision":"rousi-reward-v1","formula_version":"nexus-atan-active-v1","effective_from":"2026-08-20T00:00:00Z","curve_hourly_cap_milli":100000,"age_saturation_seconds":2419200,"seeder_decay":7,"curve_scale_milli":300000,"size_multiplier_bps":10000,"official_bonus_bps":10000,"upload_contribution_bonus_bps":5000,"per_torrent_hourly_milli":500,"base_linear_torrent_limit":60,"maximum_level_torrent_bonus":55,"minimum_torrent_bytes":52428800,"minimum_active_seconds":300,"maximum_snapshot_age_seconds":600,"vip_bonus_bps":2000,"maximum_medal_bonus_bps":10000,"maximum_level_bonus_bps":2000,"maximum_hourly_reward":500,"experience_per_magic_bps":200}',
    decode('6fe0453ae61cd51b5dfc5e8b466c76fa480eb3be05abc6d433137560a2ae963b', 'hex'),
    NULL, NULL,
    'Rousi 在线参数迁移后的首版做种奖励基线。',
    '2026-08-19T23:00:00Z'
WHERE NOT EXISTS (
    SELECT 1 FROM economy.seeding_reward_policy_revisions
);

-- Upload, torrent publication and account-age experience share one compact
-- timeline.  Seeding conversion remains in the seeding policy, and attendance
-- experience remains in the attendance policy, because those values must be
-- cryptographically coupled to their own settlement receipts.
CREATE TABLE progression.contribution_experience_policy_revisions (
    revision text PRIMARY KEY CHECK (
        revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    effective_from timestamptz NOT NULL UNIQUE,
    experience_per_upload_gib_milli bigint NOT NULL CHECK (
        experience_per_upload_gib_milli BETWEEN 0 AND 1000000000
    ),
    experience_per_torrent_milli bigint NOT NULL CHECK (
        experience_per_torrent_milli BETWEEN 0 AND 1000000000
    ),
    experience_per_account_day_milli bigint NOT NULL CHECK (
        experience_per_account_day_milli BETWEEN 0 AND 1000000000
    ),
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
    CHECK (mod(extract(epoch FROM effective_from)::numeric, 3600) = 0),
    CHECK (
        (issued_by IS NULL AND authorization_decision_id IS NULL)
        OR (issued_by IS NOT NULL AND authorization_decision_id IS NOT NULL)
    )
);

CREATE INDEX contribution_experience_policy_effective_idx
    ON progression.contribution_experience_policy_revisions
    (effective_from DESC, revision DESC);

-- +goose StatementBegin
CREATE FUNCTION progression.require_contribution_experience_policy_append()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    latest_effective_from timestamptz;
BEGIN
    PERFORM pg_advisory_xact_lock(
        hashtextextended('peergo-contribution-experience-policy-v1', 0)
    );
    SELECT max(policy.effective_from)
    INTO latest_effective_from
    FROM progression.contribution_experience_policy_revisions AS policy;

    IF latest_effective_from IS NOT NULL
       AND NEW.effective_from <= latest_effective_from THEN
        RAISE EXCEPTION 'contribution experience policy timeline must append after %', latest_effective_from;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER contribution_experience_policy_append_only
BEFORE INSERT ON progression.contribution_experience_policy_revisions
FOR EACH ROW EXECUTE FUNCTION progression.require_contribution_experience_policy_append();

CREATE TRIGGER contribution_experience_policy_immutable
BEFORE UPDATE OR DELETE ON progression.contribution_experience_policy_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO progression.contribution_experience_policy_revisions (
    revision, effective_from,
    experience_per_upload_gib_milli,
    experience_per_torrent_milli,
    experience_per_account_day_milli,
    snapshot_json, snapshot_sha256,
    issued_by, authorization_decision_id, reason, created_at
) VALUES (
    'rousi-contribution-v1', '2026-08-20T00:00:00Z',
    100, 2000, 1000,
    '{"revision":"rousi-contribution-v1","effective_from":"2026-08-20T00:00:00Z","experience_per_upload_gib_milli":100,"experience_per_torrent_milli":2000,"experience_per_account_day_milli":1000}',
    decode('eb3b67e8b61747c97c3a97b2d7fa854a4fd26a6618f53e095e1955a879259d3b', 'hex'),
    NULL, NULL,
    'Rousi 在线经验来源参数迁移后的首版基线。',
    '2026-08-19T23:00:00Z'
);

-- Rousi's live attendance settings also provide the fifth experience source.
-- Do not override an operator-issued attendance timeline during upgrades.
INSERT INTO economy.attendance_policy_revisions (
    revision, effective_from, enabled, day_boundary_timezone,
    fixed_enabled, fixed_reward, random_enabled, random_min, random_max,
    streak_enabled, experience_reward, snapshot_json, snapshot_sha256,
    issued_by, authorization_decision_id, reason, created_at
)
SELECT
    'attendance-rousi-v2', '2026-08-20T00:00:00Z', true, 'Asia/Shanghai',
    true, 100, true, 1, 300,
    true, 1,
    '{"revision":"attendance-rousi-v2","effective_from":"2026-08-20T00:00:00Z","enabled":true,"day_boundary_timezone":"Asia/Shanghai","fixed_enabled":true,"fixed_reward":100,"random_enabled":true,"random_min":1,"random_max":300,"streak_enabled":true,"streak_milestones":[{"days":7,"reward":50},{"days":15,"reward":100},{"days":30,"reward":200}],"experience_reward":1}',
    decode('f3a60136f6eb19f52ea70a9d8f4d2c7ac726670559cd2d74334e33d2427e2e98', 'hex'),
    NULL, NULL,
    'Rousi 在线签到与经验参数迁移后的首版基线。',
    '2026-08-19T23:00:00Z'
WHERE NOT EXISTS (
    SELECT 1
    FROM economy.attendance_policy_revisions
    WHERE issued_by IS NOT NULL
       OR effective_from >= '2026-08-20T00:00:00Z'
);

INSERT INTO economy.attendance_policy_streak_milestones (
    policy_revision, position, days, reward
)
SELECT 'attendance-rousi-v2', values.position, values.days, values.reward
FROM (VALUES
    (0::smallint, 7, 50),
    (1::smallint, 15, 100),
    (2::smallint, 30, 200)
) AS values(position, days, reward)
WHERE EXISTS (
    SELECT 1 FROM economy.attendance_policy_revisions
    WHERE revision = 'attendance-rousi-v2'
);

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('progression.contribution.policy.read', '读取上传、发种与账号时长经验政策', 'medium', 'none', 'staff-session', true, true),
    ('progression.contribution.policy.issue', '签发上传、发种与账号时长经验政策', 'high', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'progression.contribution.policy.read'),
    ('site_admin', 'progression.contribution.policy.issue');

REVOKE ALL ON progression.contribution_experience_policy_revisions FROM PUBLIC;

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'site_admin'
  AND action IN (
      'progression.contribution.policy.read',
      'progression.contribution.policy.issue'
  );

DELETE FROM authz.permissions
WHERE action IN (
    'progression.contribution.policy.read',
    'progression.contribution.policy.issue'
);

-- The rows removed by this down migration are append-only in normal runtime.
-- Temporarily disable only user-defined immutability triggers while rolling
-- back the migration-owned baselines, then restore the protection before the
-- migration completes.
ALTER TABLE economy.attendance_policy_streak_milestones DISABLE TRIGGER USER;
ALTER TABLE economy.attendance_policy_revisions DISABLE TRIGGER USER;
DELETE FROM economy.attendance_policy_streak_milestones
WHERE policy_revision = 'attendance-rousi-v2';
DELETE FROM economy.attendance_policy_revisions
WHERE revision = 'attendance-rousi-v2';
ALTER TABLE economy.attendance_policy_revisions ENABLE TRIGGER USER;
ALTER TABLE economy.attendance_policy_streak_milestones ENABLE TRIGGER USER;

ALTER TABLE progression.experience_policy_revisions DISABLE TRIGGER USER;
DELETE FROM progression.experience_policy_revisions
WHERE revision IN ('attendance-rousi-v2', 'rousi-reward-v1');
ALTER TABLE progression.experience_policy_revisions ENABLE TRIGGER USER;

DROP TRIGGER contribution_experience_policy_immutable
    ON progression.contribution_experience_policy_revisions;
DROP TRIGGER contribution_experience_policy_append_only
    ON progression.contribution_experience_policy_revisions;
DROP FUNCTION progression.require_contribution_experience_policy_append();
DROP INDEX progression.contribution_experience_policy_effective_idx;
DROP TABLE progression.contribution_experience_policy_revisions;

ALTER TABLE economy.seeding_reward_policy_revisions DISABLE TRIGGER USER;
DELETE FROM economy.seeding_reward_policy_revisions
WHERE revision = 'rousi-reward-v1'
  AND issued_by IS NULL;
ALTER TABLE economy.seeding_reward_policy_revisions ENABLE TRIGGER USER;

ALTER TABLE economy.seeding_reward_policy_revisions
    DROP CONSTRAINT seeding_reward_policy_issuer_pair,
    ALTER COLUMN authorization_decision_id SET NOT NULL,
    ALTER COLUMN issued_by SET NOT NULL;
