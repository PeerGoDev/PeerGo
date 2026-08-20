-- +goose Up

-- Content tips share one immutable integer-magic policy across torrents,
-- posts and comments. The baseline is deliberately disabled until staff
-- explicitly reviews the limits in the existing magic-usage settings area.
CREATE TABLE economy.content_tip_policy_revisions (
    revision text PRIMARY KEY CHECK (revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
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

CREATE INDEX content_tip_policy_recent_idx
    ON economy.content_tip_policy_revisions (created_at DESC, revision DESC);

-- +goose StatementBegin
CREATE FUNCTION economy.require_content_tip_policy_append()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    latest_created_at timestamptz;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('peergo-content-tip-policy-v1', 0));
    SELECT max(created_at) INTO latest_created_at
    FROM economy.content_tip_policy_revisions;
    IF latest_created_at IS NOT NULL AND NEW.created_at <= latest_created_at THEN
        RAISE EXCEPTION 'content tip policy timeline must append after %', latest_created_at;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER content_tip_policy_append_only
BEFORE INSERT ON economy.content_tip_policy_revisions
FOR EACH ROW EXECUTE FUNCTION economy.require_content_tip_policy_append();

CREATE TRIGGER content_tip_policy_immutable
BEFORE UPDATE OR DELETE ON economy.content_tip_policy_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO economy.content_tip_policy_revisions (
    revision, enabled, minimum_amount, maximum_amount, daily_gross_limit,
    fee_bps, snapshot_json, snapshot_sha256, issued_by,
    authorization_decision_id, reason, created_at
) VALUES (
    'content-tip-disabled-v1', false, 1, 10000, 20000, 0,
    '{"revision":"content-tip-disabled-v1","enabled":false,"minimum_amount":1,"maximum_amount":10000,"daily_gross_limit":20000,"fee_bps":0,"created_at":"2026-08-17T00:00:01Z"}',
    decode('b487c1e65db7ddd583a8d7595287b801a1b6d2411260eb558b7efa57b5fc663f', 'hex'),
    NULL, NULL, 'PeerGo 升级基线默认关闭内容打赏', '2026-08-17T00:00:01Z'
);

CREATE TABLE economy.content_tips (
    id uuid PRIMARY KEY,
    request_id uuid NOT NULL UNIQUE,
    tipper_user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    recipient_user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    target_kind text NOT NULL CHECK (target_kind IN ('torrent', 'post', 'comment')),
    target_title text NOT NULL CHECK (char_length(btrim(target_title)) BETWEEN 1 AND 240),
    gross_amount bigint NOT NULL CHECK (gross_amount > 0),
    fee_amount bigint NOT NULL CHECK (fee_amount >= 0 AND fee_amount < gross_amount),
    net_amount bigint NOT NULL CHECK (net_amount > 0 AND net_amount = gross_amount - fee_amount),
    policy_revision text NOT NULL
        REFERENCES economy.content_tip_policy_revisions (revision) ON DELETE RESTRICT,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    magic_transaction_id uuid NOT NULL UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    UNIQUE (id, target_kind),
    CHECK (tipper_user_id <> recipient_user_id),
    CHECK (recorded_at >= occurred_at)
);

CREATE TABLE economy.torrent_content_tips (
    content_tip_id uuid PRIMARY KEY,
    target_kind text NOT NULL DEFAULT 'torrent' CHECK (target_kind = 'torrent'),
    torrent_id bigint NOT NULL REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    FOREIGN KEY (content_tip_id, target_kind)
        REFERENCES economy.content_tips (id, target_kind) ON DELETE RESTRICT
);

CREATE TABLE economy.post_content_tips (
    content_tip_id uuid PRIMARY KEY,
    target_kind text NOT NULL DEFAULT 'post' CHECK (target_kind = 'post'),
    post_id bigint NOT NULL REFERENCES social.posts (id) ON DELETE RESTRICT,
    FOREIGN KEY (content_tip_id, target_kind)
        REFERENCES economy.content_tips (id, target_kind) ON DELETE RESTRICT
);

CREATE TABLE economy.comment_content_tips (
    content_tip_id uuid PRIMARY KEY,
    target_kind text NOT NULL DEFAULT 'comment' CHECK (target_kind = 'comment'),
    comment_id bigint NOT NULL REFERENCES social.comments (id) ON DELETE RESTRICT,
    FOREIGN KEY (content_tip_id, target_kind)
        REFERENCES economy.content_tips (id, target_kind) ON DELETE RESTRICT
);

-- The receipt is inserted before its typed binding. A deferred constraint
-- verifies the complete aggregate at commit instead of permitting an untyped
-- nullable target key on the receipt itself.
-- +goose StatementBegin
CREATE FUNCTION economy.require_content_tip_binding()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    binding_count integer;
BEGIN
    SELECT
        (SELECT count(*) FROM economy.torrent_content_tips WHERE content_tip_id = NEW.id)
      + (SELECT count(*) FROM economy.post_content_tips WHERE content_tip_id = NEW.id)
      + (SELECT count(*) FROM economy.comment_content_tips WHERE content_tip_id = NEW.id)
    INTO binding_count;
    IF binding_count <> 1 THEN
        RAISE EXCEPTION 'content tip must have exactly one typed target binding';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER content_tip_binding_complete
AFTER INSERT ON economy.content_tips
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION economy.require_content_tip_binding();

CREATE INDEX content_tips_tipper_recent_idx
    ON economy.content_tips (tipper_user_id, occurred_at DESC, id DESC);
CREATE INDEX content_tips_recipient_recent_idx
    ON economy.content_tips (recipient_user_id, occurred_at DESC, id DESC);
CREATE INDEX content_tips_tipper_daily_idx
    ON economy.content_tips (tipper_user_id, occurred_at);
CREATE INDEX torrent_content_tips_target_idx
    ON economy.torrent_content_tips (torrent_id, content_tip_id);
CREATE INDEX post_content_tips_target_idx
    ON economy.post_content_tips (post_id, content_tip_id);
CREATE INDEX comment_content_tips_target_idx
    ON economy.comment_content_tips (comment_id, content_tip_id);

CREATE TRIGGER content_tips_immutable
BEFORE UPDATE OR DELETE ON economy.content_tips
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();
CREATE TRIGGER torrent_content_tips_immutable
BEFORE UPDATE OR DELETE ON economy.torrent_content_tips
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();
CREATE TRIGGER post_content_tips_immutable
BEFORE UPDATE OR DELETE ON economy.post_content_tips
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();
CREATE TRIGGER comment_content_tips_immutable
BEFORE UPDATE OR DELETE ON economy.comment_content_tips
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('economy.contenttip.create.self', '给一条公开内容的作者打赏自己的整数魔力值', 'medium', 'self', 'web-session', true, true),
    ('economy.contenttip.policy.issue', '签发立即生效的不可变内容打赏政策', 'high', 'none', 'staff-session', true, true),
    ('economy.contenttip.policy.read', '读取内容打赏政策时间线', 'medium', 'none', 'staff-session', true, true),
    ('economy.contenttip.read.self', '查看自己的内容打赏规则与收发记录', 'low', 'self', 'web-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'economy.contenttip.create.self'),
    ('member', 'economy.contenttip.read.self'),
    ('site_admin', 'economy.contenttip.policy.issue'),
    ('site_admin', 'economy.contenttip.policy.read');

REVOKE ALL ON economy.content_tip_policy_revisions FROM PUBLIC;
REVOKE ALL ON economy.content_tips FROM PUBLIC;
REVOKE ALL ON economy.torrent_content_tips FROM PUBLIC;
REVOKE ALL ON economy.post_content_tips FROM PUBLIC;
REVOKE ALL ON economy.comment_content_tips FROM PUBLIC;

-- +goose Down

DELETE FROM authz.role_permissions
WHERE (role_id = 'member' AND action IN (
        'economy.contenttip.create.self', 'economy.contenttip.read.self'
    ))
   OR (role_id = 'site_admin' AND action IN (
        'economy.contenttip.policy.issue', 'economy.contenttip.policy.read'
    ));
DELETE FROM authz.permissions
WHERE action IN (
    'economy.contenttip.create.self', 'economy.contenttip.policy.issue',
    'economy.contenttip.policy.read', 'economy.contenttip.read.self'
);

DROP TRIGGER comment_content_tips_immutable ON economy.comment_content_tips;
DROP TRIGGER post_content_tips_immutable ON economy.post_content_tips;
DROP TRIGGER torrent_content_tips_immutable ON economy.torrent_content_tips;
DROP TRIGGER content_tips_immutable ON economy.content_tips;
DROP INDEX economy.comment_content_tips_target_idx;
DROP INDEX economy.post_content_tips_target_idx;
DROP INDEX economy.torrent_content_tips_target_idx;
DROP INDEX economy.content_tips_tipper_daily_idx;
DROP INDEX economy.content_tips_recipient_recent_idx;
DROP INDEX economy.content_tips_tipper_recent_idx;
DROP TRIGGER content_tip_binding_complete ON economy.content_tips;
DROP FUNCTION economy.require_content_tip_binding();
DROP TABLE economy.comment_content_tips;
DROP TABLE economy.post_content_tips;
DROP TABLE economy.torrent_content_tips;
DROP TABLE economy.content_tips;
DROP TRIGGER content_tip_policy_immutable ON economy.content_tip_policy_revisions;
DROP TRIGGER content_tip_policy_append_only ON economy.content_tip_policy_revisions;
DROP FUNCTION economy.require_content_tip_policy_append();
DROP INDEX economy.content_tip_policy_recent_idx;
DROP TABLE economy.content_tip_policy_revisions;
