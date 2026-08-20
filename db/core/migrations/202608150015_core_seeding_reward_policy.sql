-- +goose Up

-- Reward policies are append-only formula snapshots. They are scheduled on a
-- UTC hour boundary so a single closed evidence window can never straddle two
-- formula revisions. No mutable "current settings" row is used for replay.
CREATE TABLE economy.seeding_reward_policy_revisions (
    revision text PRIMARY KEY CHECK (
        revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    formula_version text NOT NULL CHECK (
        formula_version = 'nexus-atan-active-v1'
    ),
    effective_from timestamptz NOT NULL UNIQUE,
    curve_hourly_cap_milli bigint NOT NULL CHECK (curve_hourly_cap_milli BETWEEN 1 AND 10000000000),
    age_saturation_seconds bigint NOT NULL CHECK (age_saturation_seconds BETWEEN 3600 AND 315360000),
    seeder_decay integer NOT NULL CHECK (seeder_decay BETWEEN 2 AND 1000),
    curve_scale_milli bigint NOT NULL CHECK (curve_scale_milli BETWEEN 1 AND 10000000000000),
    size_multiplier_bps bigint NOT NULL CHECK (size_multiplier_bps BETWEEN 1 AND 100000),
    official_bonus_bps bigint NOT NULL CHECK (official_bonus_bps BETWEEN 0 AND 100000),
    upload_contribution_bonus_bps bigint NOT NULL CHECK (upload_contribution_bonus_bps BETWEEN 0 AND 100000),
    per_torrent_hourly_milli bigint NOT NULL CHECK (per_torrent_hourly_milli BETWEEN 0 AND 10000000000),
    base_linear_torrent_limit integer NOT NULL CHECK (base_linear_torrent_limit BETWEEN 0 AND 100000),
    maximum_level_torrent_bonus integer NOT NULL CHECK (maximum_level_torrent_bonus BETWEEN 0 AND 100000),
    minimum_torrent_bytes bigint NOT NULL CHECK (minimum_torrent_bytes BETWEEN 0 AND 1152921504606846976),
    minimum_active_seconds integer NOT NULL CHECK (minimum_active_seconds BETWEEN 1 AND 3600),
    maximum_snapshot_age_seconds integer NOT NULL CHECK (maximum_snapshot_age_seconds BETWEEN 1 AND 86400),
    vip_bonus_bps bigint NOT NULL CHECK (vip_bonus_bps BETWEEN 0 AND 100000),
    maximum_medal_bonus_bps bigint NOT NULL CHECK (maximum_medal_bonus_bps BETWEEN 0 AND 100000),
    maximum_level_bonus_bps bigint NOT NULL CHECK (maximum_level_bonus_bps BETWEEN 0 AND 100000),
    maximum_hourly_reward bigint NOT NULL CHECK (maximum_hourly_reward BETWEEN 1 AND 1000000000),
    experience_per_magic_bps bigint NOT NULL CHECK (experience_per_magic_bps BETWEEN 0 AND 100000),
    snapshot_json text NOT NULL CHECK (
        octet_length(snapshot_json) BETWEEN 2 AND 16384
        AND jsonb_typeof(snapshot_json::jsonb) = 'object'
    ),
    snapshot_sha256 bytea NOT NULL CHECK (octet_length(snapshot_sha256) = 32),
    issued_by uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid NOT NULL,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    created_at timestamptz NOT NULL,
    CHECK (created_at <= effective_from),
    CHECK (mod(extract(epoch FROM effective_from)::numeric, 3600) = 0)
);

CREATE INDEX seeding_reward_policy_effective_idx
    ON economy.seeding_reward_policy_revisions (effective_from DESC, revision DESC);

-- Direct writers cannot insert an older point into an already signed future
-- timeline. The transaction-level lock also serializes concurrent issuers.
-- +goose StatementBegin
CREATE FUNCTION economy.require_seeding_reward_policy_append()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    latest_effective_from timestamptz;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('peergo-seeding-reward-policy-v1', 0));
    SELECT max(policy.effective_from)
    INTO latest_effective_from
    FROM economy.seeding_reward_policy_revisions AS policy;

    IF latest_effective_from IS NOT NULL
       AND NEW.effective_from <= latest_effective_from THEN
        RAISE EXCEPTION 'seeding reward policy timeline must append after %', latest_effective_from;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER seeding_reward_policy_append_only
BEFORE INSERT ON economy.seeding_reward_policy_revisions
FOR EACH ROW EXECUTE FUNCTION economy.require_seeding_reward_policy_append();

CREATE TRIGGER seeding_reward_policy_immutable
BEFORE UPDATE OR DELETE ON economy.seeding_reward_policy_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('economy.seedingreward.policy.read', '读取做种奖励政策时间线与预览', 'medium', 'none', 'staff-session', true, true),
    ('economy.seedingreward.policy.issue', '签发未来生效的不可变做种奖励政策', 'high', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('site_admin', 'economy.seedingreward.policy.read'),
    ('site_admin', 'economy.seedingreward.policy.issue');

REVOKE ALL ON economy.seeding_reward_policy_revisions FROM PUBLIC;

-- +goose Down

DELETE FROM authz.role_permissions
WHERE role_id = 'site_admin'
  AND action IN (
      'economy.seedingreward.policy.read',
      'economy.seedingreward.policy.issue'
  );

DELETE FROM authz.permissions
WHERE action IN (
    'economy.seedingreward.policy.read',
    'economy.seedingreward.policy.issue'
);

DROP TRIGGER seeding_reward_policy_immutable
    ON economy.seeding_reward_policy_revisions;
DROP TRIGGER seeding_reward_policy_append_only
    ON economy.seeding_reward_policy_revisions;
DROP FUNCTION economy.require_seeding_reward_policy_append();
DROP INDEX economy.seeding_reward_policy_effective_idx;
DROP TABLE economy.seeding_reward_policy_revisions;
