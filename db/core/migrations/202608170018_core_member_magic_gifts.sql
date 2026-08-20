-- +goose Up

-- Member gifts use an immutable policy timeline. Receipts copy the exact
-- revision used, so later fee or limit changes never reinterpret old ledger
-- entries. The upgrade baseline is deliberately disabled.
CREATE TABLE economy.member_gift_policy_revisions (
    revision text PRIMARY KEY CHECK (
        revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    enabled boolean NOT NULL,
    minimum_amount bigint NOT NULL CHECK (minimum_amount BETWEEN 1 AND 1000000000),
    maximum_amount bigint NOT NULL CHECK (maximum_amount BETWEEN minimum_amount AND 1000000000),
    daily_gross_limit bigint NOT NULL CHECK (daily_gross_limit BETWEEN maximum_amount AND 1000000000000),
    fee_bps integer NOT NULL CHECK (fee_bps BETWEEN 0 AND 5000),
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
    )
);

CREATE INDEX member_gift_policy_recent_idx
    ON economy.member_gift_policy_revisions (created_at DESC, revision DESC);

-- +goose StatementBegin
CREATE FUNCTION economy.require_member_gift_policy_append()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    latest_created_at timestamptz;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('peergo-member-gift-policy-v1', 0));
    SELECT max(policy.created_at)
    INTO latest_created_at
    FROM economy.member_gift_policy_revisions AS policy;

    IF latest_created_at IS NOT NULL AND NEW.created_at <= latest_created_at THEN
        RAISE EXCEPTION 'member gift policy timeline must append after %', latest_created_at;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER member_gift_policy_append_only
BEFORE INSERT ON economy.member_gift_policy_revisions
FOR EACH ROW EXECUTE FUNCTION economy.require_member_gift_policy_append();

CREATE TRIGGER member_gift_policy_immutable
BEFORE UPDATE OR DELETE ON economy.member_gift_policy_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO economy.member_gift_policy_revisions (
    revision, enabled, minimum_amount, maximum_amount, daily_gross_limit,
    fee_bps, snapshot_json, snapshot_sha256, issued_by,
    authorization_decision_id, reason, created_at
) VALUES (
    'member-gift-disabled-v1',
    false,
    1,
    10000,
    20000,
    0,
    '{"revision":"member-gift-disabled-v1","enabled":false,"minimum_amount":1,"maximum_amount":10000,"daily_gross_limit":20000,"fee_bps":0,"created_at":"2026-08-17T00:00:00Z"}',
    decode('79cccf4c56fd285411837109cd62b9cbea43009a23a7db908b36932b396dd1b7', 'hex'),
    NULL,
    NULL,
    'PeerGo 升级基线默认关闭成员赠送',
    '2026-08-17T00:00:00Z'
);

CREATE TABLE economy.member_gifts (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE,
    sender_user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    recipient_user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    gross_amount bigint NOT NULL CHECK (gross_amount > 0),
    fee_amount bigint NOT NULL CHECK (fee_amount >= 0 AND fee_amount < gross_amount),
    net_amount bigint NOT NULL CHECK (
        net_amount > 0 AND net_amount = gross_amount - fee_amount
    ),
    message text NOT NULL CHECK (char_length(message) <= 200),
    policy_revision text NOT NULL
        REFERENCES economy.member_gift_policy_revisions (revision) ON DELETE RESTRICT,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    magic_transaction_id uuid NOT NULL UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    CHECK (sender_user_id <> recipient_user_id),
    CHECK (recorded_at >= occurred_at)
);

CREATE INDEX member_gifts_sender_recent_idx
    ON economy.member_gifts (sender_user_id, occurred_at DESC, id DESC);

CREATE INDEX member_gifts_recipient_recent_idx
    ON economy.member_gifts (recipient_user_id, occurred_at DESC, id DESC);

CREATE INDEX member_gifts_sender_daily_idx
    ON economy.member_gifts (sender_user_id, occurred_at);

CREATE TRIGGER member_gifts_immutable
BEFORE UPDATE OR DELETE ON economy.member_gifts
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('economy.membergift.create.self', '向一名正常成员赠送自己的整数魔力值', 'medium', 'self', 'web-session', true, true),
    ('economy.membergift.policy.issue', '签发立即生效的不可变成员赠送政策', 'high', 'none', 'staff-session', true, true),
    ('economy.membergift.policy.read', '读取成员赠送政策时间线', 'medium', 'none', 'staff-session', true, true),
    ('economy.membergift.read.self', '查看自己的成员赠送规则与收发记录', 'low', 'self', 'web-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'economy.membergift.create.self'),
    ('member', 'economy.membergift.read.self'),
    ('site_admin', 'economy.membergift.policy.issue'),
    ('site_admin', 'economy.membergift.policy.read');

REVOKE ALL ON economy.member_gift_policy_revisions FROM PUBLIC;
REVOKE ALL ON economy.member_gifts FROM PUBLIC;

-- +goose Down

DELETE FROM authz.role_permissions
WHERE (role_id = 'member' AND action IN (
        'economy.membergift.create.self',
        'economy.membergift.read.self'
    ))
   OR (role_id = 'site_admin' AND action IN (
        'economy.membergift.policy.issue',
        'economy.membergift.policy.read'
    ));

DELETE FROM authz.permissions
WHERE action IN (
    'economy.membergift.create.self',
    'economy.membergift.policy.issue',
    'economy.membergift.policy.read',
    'economy.membergift.read.self'
);

DROP TRIGGER member_gifts_immutable ON economy.member_gifts;
DROP INDEX economy.member_gifts_sender_daily_idx;
DROP INDEX economy.member_gifts_recipient_recent_idx;
DROP INDEX economy.member_gifts_sender_recent_idx;
DROP TABLE economy.member_gifts;
DROP TRIGGER member_gift_policy_immutable ON economy.member_gift_policy_revisions;
DROP TRIGGER member_gift_policy_append_only ON economy.member_gift_policy_revisions;
DROP FUNCTION economy.require_member_gift_policy_append();
DROP INDEX economy.member_gift_policy_recent_idx;
DROP TABLE economy.member_gift_policy_revisions;
