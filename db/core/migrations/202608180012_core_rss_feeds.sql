-- +goose Up

-- RSS subscriptions are member-owned delegated read credentials.  Raw tokens
-- are returned once and never persisted; only their SHA-256 lookup digests
-- enter Core PostgreSQL.
ALTER TABLE authz.permissions
    DROP CONSTRAINT permissions_credential_audience_check,
    ADD CONSTRAINT permissions_credential_audience_check CHECK (
        credential_audience IN ('anonymous', 'web-session', 'staff-session', 'service', 'rss-token')
    );

INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES
    ('rss.subscription.read.self', '查看自己的 RSS 订阅', 'low', 'self', 'web-session', true, true),
    ('rss.subscription.manage.self', '创建、修改或撤销自己的 RSS 订阅', 'medium', 'self', 'web-session', true, true),
    ('rss.subscription.token', '使用独立令牌读取一个固定 RSS 订阅及其种子附件', 'medium', 'none', 'rss-token', false, false),
    ('rss.settings.manage.read', '读取 RSS 服务设置', 'medium', 'none', 'staff-session', true, true),
    ('rss.settings.update', '修改 RSS 服务设置', 'high', 'none', 'staff-session', true, true);

INSERT INTO authz.role_permissions (role_id, action) VALUES
    ('member', 'rss.subscription.read.self'),
    ('member', 'rss.subscription.manage.self'),
    ('site_admin', 'rss.settings.manage.read'),
    ('site_admin', 'rss.settings.update');

CREATE SCHEMA rss;

-- Operators edit only these product-facing controls.  Revision counters,
-- cache locks and promotion boundaries remain internal implementation details.
CREATE TABLE rss.settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    enabled boolean NOT NULL DEFAULT true,
    cache_ttl_seconds integer NOT NULL DEFAULT 300
        CHECK (cache_ttl_seconds BETWEEN 60 AND 900),
    max_items_per_feed integer NOT NULL DEFAULT 50
        CHECK (max_items_per_feed BETWEEN 1 AND 50),
    max_subscriptions_per_user integer NOT NULL DEFAULT 10
        CHECK (max_subscriptions_per_user BETWEEN 1 AND 20),
    requests_per_minute integer NOT NULL DEFAULT 30
        CHECK (requests_per_minute BETWEEN 1 AND 120),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    effective_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO rss.settings (singleton) VALUES (true);

-- RSS settings are operational policy, so every optimistic update leaves an
-- append-only before/after snapshot and the exact authorization decision.  No
-- raw feed token is part of this evidence.
CREATE TABLE rss.settings_changes (
    id uuid PRIMARY KEY,
    actor_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    authorization_decision_id uuid NOT NULL,
    expected_version bigint NOT NULL CHECK (expected_version > 0),
    resulting_version bigint NOT NULL CHECK (resulting_version = expected_version + 1),
    before_json jsonb NOT NULL CHECK (jsonb_typeof(before_json) = 'object'),
    after_json jsonb NOT NULL CHECK (jsonb_typeof(after_json) = 'object'),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 500),
    occurred_at timestamptz NOT NULL
);

CREATE INDEX rss_settings_changes_recent_idx
    ON rss.settings_changes (occurred_at DESC, id DESC);

CREATE TRIGGER rss_settings_changes_immutable
BEFORE UPDATE OR DELETE ON rss.settings_changes
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TABLE rss.subscriptions (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 80),
    enabled boolean NOT NULL DEFAULT true,
    token_sha256 bytea NOT NULL UNIQUE CHECK (octet_length(token_sha256) = 32),
    token_version bigint NOT NULL DEFAULT 1 CHECK (token_version > 0),
    category_ids text[] NOT NULL DEFAULT ARRAY[]::text[] CHECK (
        cardinality(category_ids) <= 20
        AND array_position(category_ids, NULL) IS NULL
    ),
    promotion_filters text[] NOT NULL DEFAULT ARRAY[]::text[] CHECK (
        cardinality(promotion_filters) <= 6
        AND array_position(promotion_filters, NULL) IS NULL
        AND promotion_filters <@ ARRAY[
            'free', 'double_upload', 'double_upload_free', 'half_download',
            'double_upload_half_download', 'thirty_percent_download'
        ]::text[]
    ),
    price_filter text NOT NULL DEFAULT 'all'
        CHECK (price_filter IN ('all', 'free', 'paid')),
    bookmarked_only boolean NOT NULL DEFAULT false,
    item_limit integer NOT NULL DEFAULT 50 CHECK (item_limit BETWEEN 1 AND 50),
    include_category boolean NOT NULL DEFAULT true,
    include_subtitle boolean NOT NULL DEFAULT true,
    include_size boolean NOT NULL DEFAULT true,
    include_promotion boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    revoked_at timestamptz,
    CHECK (updated_at >= created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE INDEX rss_subscriptions_user_recent_idx
    ON rss.subscriptions (user_id, created_at DESC, id DESC)
    WHERE revoked_at IS NULL;

-- This small global revision invalidates safe item projections after catalog
-- state changes.  It contains no token and is intentionally independent of a
-- process-local cache, so horizontally scaled Core instances agree.
CREATE TABLE rss.content_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO rss.content_state (singleton) VALUES (true);

CREATE UNLOGGED TABLE rss.feed_cache (
    subscription_id uuid PRIMARY KEY REFERENCES rss.subscriptions (id) ON DELETE CASCADE,
    subscription_version bigint NOT NULL CHECK (subscription_version > 0),
    content_revision bigint NOT NULL CHECK (content_revision > 0),
    observed_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    next_boundary_at timestamptz,
    item_projection jsonb NOT NULL CHECK (jsonb_typeof(item_projection) = 'array'),
    updated_at timestamptz NOT NULL,
    CHECK (expires_at > observed_at),
    CHECK (next_boundary_at IS NULL OR next_boundary_at > observed_at)
);

CREATE INDEX rss_feed_cache_expiry_idx ON rss.feed_cache (expires_at);

-- All subscriptions owned by one member share a fixed-window allowance.  A
-- user cannot multiply request volume merely by creating more RSS URLs.
CREATE UNLOGGED TABLE rss.user_rate_limits (
    user_id uuid PRIMARY KEY REFERENCES identity.users (id) ON DELETE CASCADE,
    window_started_at timestamptz NOT NULL,
    request_count integer NOT NULL CHECK (request_count > 0),
    updated_at timestamptz NOT NULL
);

-- Statement triggers avoid one revision update per affected torrent while
-- still invalidating every Core instance after a committed content change.
-- +goose StatementBegin
CREATE FUNCTION rss.bump_content_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE rss.content_state
    SET revision = revision + 1, updated_at = clock_timestamp()
    WHERE singleton = true;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER rss_catalog_torrents_changed
AFTER INSERT OR UPDATE OR DELETE ON catalog.torrents
FOR EACH STATEMENT EXECUTE FUNCTION rss.bump_content_revision();

CREATE TRIGGER rss_torrent_aggregates_changed
AFTER INSERT OR UPDATE ON torrents.torrents
FOR EACH STATEMENT EXECUTE FUNCTION rss.bump_content_revision();

CREATE TRIGGER rss_categories_changed
AFTER INSERT OR UPDATE OR DELETE ON catalog.categories
FOR EACH STATEMENT EXECUTE FUNCTION rss.bump_content_revision();

CREATE TRIGGER rss_bookmarks_changed
AFTER INSERT OR DELETE ON catalog.torrent_bookmarks
FOR EACH STATEMENT EXECUTE FUNCTION rss.bump_content_revision();

CREATE TRIGGER rss_product_orders_changed
AFTER INSERT ON promotion.product_orders
FOR EACH STATEMENT EXECUTE FUNCTION rss.bump_content_revision();

-- Campaign rows remain invisible until Settlement acknowledges delivery.
-- Claim/lease retries do not affect feeds, so only the first delivery changes
-- the revision and avoids pointless cache churn.
-- +goose StatementBegin
CREATE FUNCTION rss.bump_on_campaign_delivery()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.delivered_at IS NULL AND NEW.delivered_at IS NOT NULL THEN
        UPDATE rss.content_state
        SET revision = revision + 1, updated_at = clock_timestamp()
        WHERE singleton = true;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER rss_campaign_delivery_changed
AFTER UPDATE OF delivered_at ON promotion.delivery_outbox
FOR EACH ROW EXECUTE FUNCTION rss.bump_on_campaign_delivery();

REVOKE ALL ON rss.settings FROM PUBLIC;
REVOKE ALL ON rss.settings_changes FROM PUBLIC;
REVOKE ALL ON rss.subscriptions FROM PUBLIC;
REVOKE ALL ON rss.content_state FROM PUBLIC;
REVOKE ALL ON rss.feed_cache FROM PUBLIC;
REVOKE ALL ON rss.user_rate_limits FROM PUBLIC;

-- +goose Down

DROP TRIGGER rss_campaign_delivery_changed ON promotion.delivery_outbox;
DROP FUNCTION rss.bump_on_campaign_delivery();
DROP TRIGGER rss_product_orders_changed ON promotion.product_orders;
DROP TRIGGER rss_bookmarks_changed ON catalog.torrent_bookmarks;
DROP TRIGGER rss_categories_changed ON catalog.categories;
DROP TRIGGER rss_torrent_aggregates_changed ON torrents.torrents;
DROP TRIGGER rss_catalog_torrents_changed ON catalog.torrents;
DROP FUNCTION rss.bump_content_revision();
DROP SCHEMA rss CASCADE;

DELETE FROM authz.role_permissions
WHERE (role_id = 'member' AND action IN ('rss.subscription.read.self', 'rss.subscription.manage.self'))
   OR (role_id = 'site_admin' AND action IN ('rss.settings.manage.read', 'rss.settings.update'));
DELETE FROM authz.permissions
WHERE action IN (
    'rss.subscription.read.self', 'rss.subscription.manage.self',
    'rss.subscription.token', 'rss.settings.manage.read', 'rss.settings.update'
);

ALTER TABLE authz.permissions
    DROP CONSTRAINT permissions_credential_audience_check,
    ADD CONSTRAINT permissions_credential_audience_check CHECK (
        credential_audience IN ('anonymous', 'web-session', 'staff-session', 'service')
    );
