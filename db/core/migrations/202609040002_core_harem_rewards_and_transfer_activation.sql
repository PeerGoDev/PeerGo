-- +goose Up

-- PtYes paid an inviter a percentage of the current hourly seeding reward of
-- eligible descendants. PeerGo settles the same hourly entitlement in
-- six-hour batches: caps are still applied per source hour, while one compact
-- receipt and one balanced transaction are stored per inviter and batch.
INSERT INTO economy.magic_accounts (
    id, user_id, account_kind, account_code, balance, version, updated_at
) VALUES (
    '00000000-0000-7000-8000-000000000010', NULL, 'system',
    'system:mint:harem', 0, 1, clock_timestamp()
);

CREATE TABLE economy.harem_reward_policy_revisions (
    revision text PRIMARY KEY CHECK (revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
    enabled boolean NOT NULL,
    reward_bps integer NOT NULL CHECK (reward_bps BETWEEN 0 AND 10000),
    depth smallint NOT NULL CHECK (depth BETWEEN 1 AND 10),
    minimum_seed_count integer NOT NULL CHECK (minimum_seed_count BETWEEN 0 AND 100000),
    hourly_cap bigint NOT NULL CHECK (hourly_cap BETWEEN 0 AND 1000000000),
    activity_days integer NOT NULL CHECK (activity_days BETWEEN 0 AND 3650),
    settlement_hours smallint NOT NULL CHECK (
        settlement_hours BETWEEN 1 AND 24 AND 24 % settlement_hours = 0
    ),
    effective_from timestamptz NOT NULL UNIQUE,
    snapshot_json text NOT NULL CHECK (
        octet_length(snapshot_json) BETWEEN 2 AND 4096
        AND jsonb_typeof(snapshot_json::jsonb) = 'object'
    ),
    snapshot_sha256 bytea NOT NULL CHECK (octet_length(snapshot_sha256) = 32),
    issued_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    created_at timestamptz NOT NULL UNIQUE,
    CHECK (
        (issued_by IS NULL AND authorization_decision_id IS NULL)
        OR (issued_by IS NOT NULL AND authorization_decision_id IS NOT NULL)
    ),
    CHECK (effective_from = date_trunc('hour', effective_from))
);

-- +goose StatementBegin
CREATE FUNCTION economy.require_harem_reward_policy_append()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    latest_effective_from timestamptz;
    latest_created_at timestamptz;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('peergo-harem-reward-policy-v1', 0));
    SELECT effective_from, created_at
    INTO latest_effective_from, latest_created_at
    FROM economy.harem_reward_policy_revisions
    ORDER BY effective_from DESC
    LIMIT 1;

    IF latest_effective_from IS NOT NULL AND (
        NEW.effective_from <= latest_effective_from
        OR NEW.created_at <= latest_created_at
    ) THEN
        RAISE EXCEPTION 'harem reward policy timeline must append in order';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER harem_reward_policy_append_only
BEFORE INSERT ON economy.harem_reward_policy_revisions
FOR EACH ROW EXECUTE FUNCTION economy.require_harem_reward_policy_append();

CREATE TRIGGER harem_reward_policy_immutable
BEFORE UPDATE OR DELETE ON economy.harem_reward_policy_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO economy.harem_reward_policy_revisions (
    revision, enabled, reward_bps, depth, minimum_seed_count,
    hourly_cap, activity_days, settlement_hours, effective_from,
    snapshot_json, snapshot_sha256, issued_by,
    authorization_decision_id, reason, created_at
) VALUES (
    'harem-rousi-v1', true, 1000, 1, 1,
    100, 30, 6, '2026-08-21T05:00:00Z',
    '{"revision":"harem-rousi-v1","enabled":true,"reward_bps":1000,"depth":1,"minimum_seed_count":1,"hourly_cap":100,"activity_days":30,"settlement_hours":6,"effective_from":"2026-08-21T05:00:00Z","created_at":"2026-09-04T06:00:02Z"}',
    decode('4906cc74726208fa57d7a54e4e1a351a9904a91336ab98086571cb8347d88bf6', 'hex'),
    NULL, NULL, '恢复 Rousi 后宫规则，并按六小时聚合以减少长期流水体积',
    '2026-09-04T06:00:02Z'
);

CREATE TABLE economy.harem_reward_windows (
    window_start timestamptz PRIMARY KEY,
    window_end timestamptz NOT NULL UNIQUE,
    policy_revision text NOT NULL
        REFERENCES economy.harem_reward_policy_revisions (revision) ON DELETE RESTRICT,
    source_calculation_count bigint NOT NULL CHECK (source_calculation_count >= 0),
    eligible_relationship_count bigint NOT NULL CHECK (eligible_relationship_count >= 0),
    recipient_count integer NOT NULL CHECK (recipient_count >= 0),
    total_reward bigint NOT NULL CHECK (total_reward >= 0),
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    completed_at timestamptz NOT NULL,
    CHECK (window_end > window_start),
    CHECK (completed_at >= window_end)
);

CREATE TABLE economy.harem_reward_payouts (
    window_start timestamptz NOT NULL
        REFERENCES economy.harem_reward_windows (window_start) ON DELETE RESTRICT,
    inviter_user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    policy_revision text NOT NULL
        REFERENCES economy.harem_reward_policy_revisions (revision) ON DELETE RESTRICT,
    eligible_invitee_count integer NOT NULL CHECK (eligible_invitee_count > 0),
    eligible_invitee_hours integer NOT NULL CHECK (eligible_invitee_hours > 0),
    source_seeding_reward bigint NOT NULL CHECK (source_seeding_reward > 0),
    capped_hour_count integer NOT NULL CHECK (capped_hour_count >= 0),
    reward bigint NOT NULL CHECK (reward > 0),
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    magic_transaction_id uuid NOT NULL UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    settled_at timestamptz NOT NULL,
    PRIMARY KEY (window_start, inviter_user_id),
    CHECK (settled_at >= window_start)
);

CREATE INDEX harem_reward_payouts_inviter_recent_idx
    ON economy.harem_reward_payouts (inviter_user_id, window_start DESC);

CREATE TRIGGER harem_reward_windows_immutable
BEFORE UPDATE OR DELETE ON economy.harem_reward_windows
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER harem_reward_payouts_immutable
BEFORE UPDATE OR DELETE ON economy.harem_reward_payouts
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TABLE economy.harem_reward_worker_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    last_started_at timestamptz,
    last_completed_at timestamptz,
    last_error_code text CHECK (
        last_error_code IS NULL OR last_error_code ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    last_window_start timestamptz,
    last_window_end timestamptz,
    last_recipient_count integer NOT NULL DEFAULT 0 CHECK (last_recipient_count >= 0),
    last_total_reward bigint NOT NULL DEFAULT 0 CHECK (last_total_reward >= 0),
    run_count bigint NOT NULL DEFAULT 0 CHECK (run_count >= 0),
    CHECK ((last_window_start IS NULL) = (last_window_end IS NULL)),
    CHECK (last_window_end IS NULL OR last_window_end > last_window_start)
);

INSERT INTO economy.harem_reward_worker_state (singleton) VALUES (true);

-- Restore the transfer behavior that was active in the Rousi source. The
-- bounded PeerGo limits replace PtYes's unbounded float inputs. Existing staff
-- revisions newer than this migration timestamp remain authoritative.
INSERT INTO economy.member_gift_policy_revisions (
    revision, enabled, minimum_amount, maximum_amount, daily_gross_limit,
    fee_bps, snapshot_json, snapshot_sha256, issued_by,
    authorization_decision_id, reason, created_at
)
SELECT
    'member-gift-rousi-enabled-v1', true, 1, 1000000000, 1000000000000,
    0,
    '{"revision":"member-gift-rousi-enabled-v1","enabled":true,"minimum_amount":1,"maximum_amount":1000000000,"daily_gross_limit":1000000000000,"fee_bps":0,"created_at":"2026-09-04T06:00:00Z"}',
    decode('7eaba2d3c41c92cc65eca35f73e5cd82ce156df69dd57bdddcf6d94065bdbd75', 'hex'),
    NULL, NULL, '恢复 Rousi 已启用的成员魔力值赠送规则',
    '2026-09-04T06:00:00Z'
WHERE NOT EXISTS (
    SELECT 1 FROM economy.member_gift_policy_revisions
    WHERE created_at >= '2026-09-04T06:00:00Z'
);

INSERT INTO economy.content_tip_policy_revisions (
    revision, enabled, minimum_amount, maximum_amount, daily_gross_limit,
    fee_bps, snapshot_json, snapshot_sha256, issued_by,
    authorization_decision_id, reason, created_at
)
SELECT
    'content-tip-rousi-enabled-v1', true, 2, 1000000000, 1000000000000,
    3000,
    '{"revision":"content-tip-rousi-enabled-v1","enabled":true,"minimum_amount":2,"maximum_amount":1000000000,"daily_gross_limit":1000000000000,"fee_bps":3000,"created_at":"2026-09-04T06:00:01Z"}',
    decode('1df026c6b98acf5f3dcecf90a932d9dea840d6a8f8c150d2033dfad272dc0d8e', 'hex'),
    NULL, NULL, '恢复 Rousi 内容打赏规则与百分之三十手续费',
    '2026-09-04T06:00:01Z'
WHERE NOT EXISTS (
    SELECT 1 FROM economy.content_tip_policy_revisions
    WHERE created_at >= '2026-09-04T06:00:01Z'
);

REVOKE ALL ON economy.harem_reward_policy_revisions FROM PUBLIC;
REVOKE ALL ON economy.harem_reward_windows FROM PUBLIC;
REVOKE ALL ON economy.harem_reward_payouts FROM PUBLIC;
REVOKE ALL ON economy.harem_reward_worker_state FROM PUBLIC;

-- +goose Down

ALTER TABLE economy.content_tip_policy_revisions
    DISABLE TRIGGER content_tip_policy_immutable;
DELETE FROM economy.content_tip_policy_revisions
WHERE revision = 'content-tip-rousi-enabled-v1';
ALTER TABLE economy.content_tip_policy_revisions
    ENABLE TRIGGER content_tip_policy_immutable;

ALTER TABLE economy.member_gift_policy_revisions
    DISABLE TRIGGER member_gift_policy_immutable;
DELETE FROM economy.member_gift_policy_revisions
WHERE revision = 'member-gift-rousi-enabled-v1';
ALTER TABLE economy.member_gift_policy_revisions
    ENABLE TRIGGER member_gift_policy_immutable;

DROP TABLE economy.harem_reward_worker_state;
DROP TRIGGER harem_reward_payouts_immutable ON economy.harem_reward_payouts;
DROP TRIGGER harem_reward_windows_immutable ON economy.harem_reward_windows;
DROP INDEX economy.harem_reward_payouts_inviter_recent_idx;
DROP TABLE economy.harem_reward_payouts;
DROP TABLE economy.harem_reward_windows;
DROP TRIGGER harem_reward_policy_immutable ON economy.harem_reward_policy_revisions;
DROP TRIGGER harem_reward_policy_append_only ON economy.harem_reward_policy_revisions;
DROP FUNCTION economy.require_harem_reward_policy_append();
DROP TABLE economy.harem_reward_policy_revisions;
DELETE FROM economy.magic_accounts
WHERE id = '00000000-0000-7000-8000-000000000010';
