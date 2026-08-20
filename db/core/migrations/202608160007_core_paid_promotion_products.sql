-- +goose Up

-- Members may spend their own integer magic balance on a published torrent.
-- Reading staff promotion settings continues to use promotion.manage.read;
-- changing prices continues to use promotion.schedule, so the permission
-- catalog does not grow another pair of equivalent staff capabilities.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES (
    'torrent.promotion.purchase.self',
    '使用自己的整数魔力值为已发布种子购买限时优惠或置顶',
    'medium', 'self', 'web-session', true, true
);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'torrent.promotion.purchase.self');

-- Staff and member campaigns share the same Settlement delivery timeline.
-- Existing rows are staff-issued; new member orders explicitly set their
-- source so administration can distinguish policy from paid activity.
ALTER TABLE promotion.campaigns
    ADD COLUMN source_kind text NOT NULL DEFAULT 'staff_schedule'
        CHECK (source_kind IN ('staff_schedule', 'member_purchase'));

-- Pricing is append-only. Orders copy the exact revision and unit prices so a
-- later setting change never rewrites a historical charge.
CREATE TABLE promotion.product_policy_revisions (
    revision text PRIMARY KEY CHECK (
        revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    effective_from timestamptz NOT NULL UNIQUE,
    promotion_enabled boolean NOT NULL,
    sticky_enabled boolean NOT NULL,
    free_price_per_day bigint NOT NULL CHECK (free_price_per_day BETWEEN 0 AND 1000000),
    double_upload_price_per_day bigint NOT NULL CHECK (double_upload_price_per_day BETWEEN 0 AND 1000000),
    double_upload_free_price_per_day bigint NOT NULL CHECK (double_upload_free_price_per_day BETWEEN 0 AND 1000000),
    half_download_price_per_day bigint NOT NULL CHECK (half_download_price_per_day BETWEEN 0 AND 1000000),
    double_upload_half_download_price_per_day bigint NOT NULL CHECK (double_upload_half_download_price_per_day BETWEEN 0 AND 1000000),
    thirty_percent_download_price_per_day bigint NOT NULL CHECK (thirty_percent_download_price_per_day BETWEEN 0 AND 1000000),
    sticky_price_per_day bigint NOT NULL CHECK (sticky_price_per_day BETWEEN 0 AND 1000000),
    max_promotion_days integer NOT NULL CHECK (max_promotion_days BETWEEN 1 AND 30),
    max_sticky_days integer NOT NULL CHECK (max_sticky_days BETWEEN 1 AND 30),
    snapshot_json text NOT NULL CHECK (
        octet_length(snapshot_json) BETWEEN 2 AND 16384
        AND jsonb_typeof(snapshot_json::jsonb) = 'object'
    ),
    snapshot_sha256 bytea NOT NULL CHECK (octet_length(snapshot_sha256) = 32),
    issued_by uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    created_at timestamptz NOT NULL,
    request_id uuid UNIQUE,
    CHECK (created_at <= effective_from),
    CHECK (
        (issued_by IS NULL AND authorization_decision_id IS NULL AND request_id IS NULL)
        OR
        (issued_by IS NOT NULL AND authorization_decision_id IS NOT NULL AND request_id IS NOT NULL)
    )
);

CREATE INDEX promotion_product_policy_effective_idx
    ON promotion.product_policy_revisions (effective_from DESC, revision DESC);

CREATE TRIGGER promotion_product_policy_immutable
BEFORE UPDATE OR DELETE ON promotion.product_policy_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

INSERT INTO promotion.product_policy_revisions (
    revision, effective_from, promotion_enabled, sticky_enabled,
    free_price_per_day, double_upload_price_per_day,
    double_upload_free_price_per_day, half_download_price_per_day,
    double_upload_half_download_price_per_day,
    thirty_percent_download_price_per_day, sticky_price_per_day,
    max_promotion_days, max_sticky_days,
    snapshot_json, snapshot_sha256, issued_by,
    authorization_decision_id, reason, created_at, request_id
) VALUES (
    'promotion-products-v1', '2026-08-16T00:00:00Z', true, true,
    50, 30, 80, 25, 55, 35, 200, 30, 30,
    '{"revision":"promotion-products-v1","effective_from":"2026-08-16T00:00:00Z","promotion_enabled":true,"sticky_enabled":true,"free_price_per_day":50,"double_upload_price_per_day":30,"double_upload_free_price_per_day":80,"half_download_price_per_day":25,"double_upload_half_download_price_per_day":55,"thirty_percent_download_price_per_day":35,"sticky_price_per_day":200,"max_promotion_days":30,"max_sticky_days":30,"currency":"magic"}',
    decode('ce6745dcefb6203bc46fd75e51b9cd6224cd6dc9a580e10dc24bbc867fdb601c', 'hex'),
    NULL, NULL,
    'PtYes 用户付费促销兼容基线：统一使用整数魔力值并限制最长三十天',
    '2026-08-16T00:00:00Z', NULL
);

INSERT INTO economy.magic_accounts (
    id, user_id, account_kind, account_code, balance, version, updated_at
) VALUES (
    '00000000-0000-7000-8000-000000000006', NULL, 'system',
    'system:sink:promotion_product', 0, 1, clock_timestamp()
);

-- One request can purchase a traffic promotion, a catalog pin, or both. The
-- order, optional Settlement campaign and ledger charge are committed in one
-- serializable transaction; a partially successful combined purchase is
-- therefore impossible.
CREATE TABLE promotion.product_orders (
    id uuid PRIMARY KEY,
    buyer_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    torrent_id bigint NOT NULL REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    campaign_id uuid UNIQUE REFERENCES promotion.campaigns (id) ON DELETE RESTRICT,
    promotion text CHECK (promotion IN (
        'free', 'double_upload', 'double_upload_free', 'half_download',
        'double_upload_half_download', 'thirty_percent_download'
    )),
    promotion_days integer CHECK (promotion_days BETWEEN 1 AND 30),
    promotion_unit_price bigint CHECK (promotion_unit_price BETWEEN 0 AND 1000000),
    promotion_starts_at timestamptz,
    promotion_ends_at timestamptz,
    sticky_days integer CHECK (sticky_days BETWEEN 1 AND 30),
    sticky_unit_price bigint CHECK (sticky_unit_price BETWEEN 0 AND 1000000),
    sticky_starts_at timestamptz,
    sticky_ends_at timestamptz,
    total_price bigint NOT NULL CHECK (total_price BETWEEN 0 AND 60000000),
    policy_revision text NOT NULL REFERENCES promotion.product_policy_revisions (revision) ON DELETE RESTRICT,
    authorization_decision_id uuid NOT NULL,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    magic_transaction_id uuid UNIQUE REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    balance_after bigint NOT NULL,
    purchased_at timestamptz NOT NULL,
    CHECK (promotion IS NOT NULL OR sticky_days IS NOT NULL),
    CHECK (
        (promotion IS NULL AND promotion_days IS NULL AND promotion_unit_price IS NULL
            AND promotion_starts_at IS NULL AND promotion_ends_at IS NULL AND campaign_id IS NULL)
        OR
        (promotion IS NOT NULL AND promotion_days IS NOT NULL AND promotion_unit_price IS NOT NULL
            AND promotion_starts_at IS NOT NULL AND promotion_ends_at > promotion_starts_at
            AND campaign_id IS NOT NULL)
    ),
    CHECK (
        (sticky_days IS NULL AND sticky_unit_price IS NULL
            AND sticky_starts_at IS NULL AND sticky_ends_at IS NULL)
        OR
        (sticky_days IS NOT NULL AND sticky_unit_price IS NOT NULL
            AND sticky_starts_at IS NOT NULL AND sticky_ends_at > sticky_starts_at)
    ),
    CHECK (
        total_price =
            coalesce(promotion_unit_price * promotion_days, 0)
            + coalesce(sticky_unit_price * sticky_days, 0)
    ),
    CHECK (
        (total_price = 0 AND magic_transaction_id IS NULL)
        OR (total_price > 0 AND magic_transaction_id IS NOT NULL)
    )
);

CREATE INDEX promotion_product_orders_buyer_time_idx
    ON promotion.product_orders (buyer_id, purchased_at DESC, id DESC);
CREATE INDEX promotion_product_orders_torrent_time_idx
    ON promotion.product_orders (torrent_id, purchased_at DESC, id DESC);
CREATE INDEX promotion_product_orders_sticky_timeline_idx
    ON promotion.product_orders (torrent_id, sticky_starts_at, sticky_ends_at)
    WHERE sticky_ends_at IS NOT NULL;

CREATE TRIGGER promotion_product_orders_immutable
BEFORE UPDATE OR DELETE ON promotion.product_orders
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- Pins expire by timestamp at read time. No cleanup worker mutates history and
-- a stopped worker cannot accidentally leave a torrent pinned forever.
CREATE FUNCTION promotion.effective_sticky_for_torrent(
    target_torrent_id bigint,
    effective_at timestamptz
)
RETURNS TABLE (
    order_id uuid,
    ends_at timestamptz
)
LANGUAGE sql
STABLE
AS $$
    SELECT product_order.id, product_order.sticky_ends_at
    FROM promotion.product_orders AS product_order
    WHERE product_order.torrent_id = target_torrent_id
      AND product_order.sticky_starts_at <= effective_at
      AND product_order.sticky_ends_at > effective_at
    ORDER BY product_order.sticky_ends_at DESC, product_order.id DESC
    LIMIT 1
$$;

REVOKE ALL ON promotion.product_policy_revisions FROM PUBLIC;
REVOKE ALL ON promotion.product_orders FROM PUBLIC;

-- +goose Down

DROP FUNCTION promotion.effective_sticky_for_torrent(bigint, timestamptz);
DROP TRIGGER promotion_product_orders_immutable ON promotion.product_orders;
DROP INDEX promotion.promotion_product_orders_sticky_timeline_idx;
DROP INDEX promotion.promotion_product_orders_torrent_time_idx;
DROP INDEX promotion.promotion_product_orders_buyer_time_idx;
DROP TABLE promotion.product_orders;

DELETE FROM economy.magic_accounts
WHERE id = '00000000-0000-7000-8000-000000000006';

DROP TRIGGER promotion_product_policy_immutable ON promotion.product_policy_revisions;
DROP INDEX promotion.promotion_product_policy_effective_idx;
DROP TABLE promotion.product_policy_revisions;

ALTER TABLE promotion.campaigns DROP COLUMN source_kind;

DELETE FROM authz.role_permissions
WHERE role_id = 'member' AND action = 'torrent.promotion.purchase.self';
DELETE FROM authz.permissions
WHERE action = 'torrent.promotion.purchase.self';
