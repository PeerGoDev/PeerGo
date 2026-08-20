-- name: GetRSSSettings :one
SELECT
    enabled,
    cache_ttl_seconds,
    max_items_per_feed,
    max_subscriptions_per_user,
    requests_per_minute,
    version,
    effective_at,
    updated_at
FROM rss.settings
WHERE singleton = true;

-- name: GetRSSSettingsForUpdate :one
SELECT
    enabled,
    cache_ttl_seconds,
    max_items_per_feed,
    max_subscriptions_per_user,
    requests_per_minute,
    version,
    effective_at,
    updated_at
FROM rss.settings
WHERE singleton = true
FOR UPDATE;

-- name: UpdateRSSSettings :one
UPDATE rss.settings
SET
    enabled = sqlc.arg(enabled)::boolean,
    cache_ttl_seconds = sqlc.arg(cache_ttl_seconds)::integer,
    max_items_per_feed = sqlc.arg(max_items_per_feed)::integer,
    max_subscriptions_per_user = sqlc.arg(max_subscriptions_per_user)::integer,
    requests_per_minute = sqlc.arg(requests_per_minute)::integer,
    version = version + 1,
    effective_at = sqlc.arg(occurred_at)::timestamptz,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE singleton = true
  AND version = sqlc.arg(expected_version)::bigint
RETURNING
    enabled,
    cache_ttl_seconds,
    max_items_per_feed,
    max_subscriptions_per_user,
    requests_per_minute,
    version,
    effective_at,
    updated_at;

-- name: InsertRSSSettingsChange :exec
INSERT INTO rss.settings_changes (
    id,
    actor_id,
    authorization_decision_id,
    expected_version,
    resulting_version,
    before_json,
    after_json,
    reason,
    occurred_at
) VALUES (
    sqlc.arg(id)::uuid,
    sqlc.arg(actor_id)::uuid,
    sqlc.arg(authorization_decision_id)::uuid,
    sqlc.arg(expected_version)::bigint,
    sqlc.arg(resulting_version)::bigint,
    sqlc.arg(before_json)::jsonb,
    sqlc.arg(after_json)::jsonb,
    sqlc.arg(reason)::text,
    sqlc.arg(occurred_at)::timestamptz
);

-- name: CountRSSSubscriptions :one
SELECT count(*)::bigint
FROM rss.subscriptions
WHERE user_id = sqlc.arg(user_id)::uuid
  AND revoked_at IS NULL;

-- name: ListRSSSubscriptions :many
SELECT
    id,
    name,
    enabled,
    token_version,
    category_ids,
    promotion_filters,
    price_filter,
    bookmarked_only,
    item_limit,
    include_category,
    include_subtitle,
    include_size,
    include_promotion,
    version,
    created_at,
    updated_at
FROM rss.subscriptions
WHERE user_id = sqlc.arg(user_id)::uuid
  AND revoked_at IS NULL
ORDER BY created_at DESC, id DESC;

-- name: CreateRSSSubscription :one
WITH valid_input AS (
    SELECT 1
    WHERE NOT EXISTS (
        SELECT 1
        FROM unnest(sqlc.arg(category_ids)::text[]) AS requested(category_id)
        LEFT JOIN catalog.categories AS category
          ON category.id = requested.category_id
         AND category.enabled = true
        WHERE category.id IS NULL
    )
)
INSERT INTO rss.subscriptions (
    id,
    user_id,
    name,
    enabled,
    token_sha256,
    category_ids,
    promotion_filters,
    price_filter,
    bookmarked_only,
    item_limit,
    include_category,
    include_subtitle,
    include_size,
    include_promotion,
    created_at,
    updated_at
)
SELECT
    sqlc.arg(id)::uuid,
    sqlc.arg(user_id)::uuid,
    sqlc.arg(name)::text,
    sqlc.arg(enabled)::boolean,
    sqlc.arg(token_sha256)::bytea,
    sqlc.arg(category_ids)::text[],
    sqlc.arg(promotion_filters)::text[],
    sqlc.arg(price_filter)::text,
    sqlc.arg(bookmarked_only)::boolean,
    sqlc.arg(item_limit)::integer,
    sqlc.arg(include_category)::boolean,
    sqlc.arg(include_subtitle)::boolean,
    sqlc.arg(include_size)::boolean,
    sqlc.arg(include_promotion)::boolean,
    sqlc.arg(created_at)::timestamptz,
    sqlc.arg(created_at)::timestamptz
FROM valid_input
RETURNING
    id,
    name,
    enabled,
    token_version,
    category_ids,
    promotion_filters,
    price_filter,
    bookmarked_only,
    item_limit,
    include_category,
    include_subtitle,
    include_size,
    include_promotion,
    version,
    created_at,
    updated_at;

-- name: GetRSSSubscriptionForUpdate :one
SELECT
    id,
    user_id,
    name,
    enabled,
    token_version,
    category_ids,
    promotion_filters,
    price_filter,
    bookmarked_only,
    item_limit,
    include_category,
    include_subtitle,
    include_size,
    include_promotion,
    version,
    created_at,
    updated_at
FROM rss.subscriptions
WHERE id = sqlc.arg(id)::uuid
  AND user_id = sqlc.arg(user_id)::uuid
  AND revoked_at IS NULL
FOR UPDATE;

-- name: UpdateRSSSubscription :one
WITH valid_input AS (
    SELECT 1
    WHERE NOT EXISTS (
        SELECT 1
        FROM unnest(sqlc.arg(category_ids)::text[]) AS requested(category_id)
        LEFT JOIN catalog.categories AS category
          ON category.id = requested.category_id
         AND category.enabled = true
        WHERE category.id IS NULL
    )
)
UPDATE rss.subscriptions AS subscription
SET
    name = sqlc.arg(name)::text,
    enabled = sqlc.arg(enabled)::boolean,
    category_ids = sqlc.arg(category_ids)::text[],
    promotion_filters = sqlc.arg(promotion_filters)::text[],
    price_filter = sqlc.arg(price_filter)::text,
    bookmarked_only = sqlc.arg(bookmarked_only)::boolean,
    item_limit = sqlc.arg(item_limit)::integer,
    include_category = sqlc.arg(include_category)::boolean,
    include_subtitle = sqlc.arg(include_subtitle)::boolean,
    include_size = sqlc.arg(include_size)::boolean,
    include_promotion = sqlc.arg(include_promotion)::boolean,
    version = subscription.version + 1,
    updated_at = sqlc.arg(updated_at)::timestamptz
FROM valid_input
WHERE subscription.id = sqlc.arg(id)::uuid
  AND subscription.user_id = sqlc.arg(user_id)::uuid
  AND subscription.version = sqlc.arg(expected_version)::bigint
  AND subscription.revoked_at IS NULL
RETURNING
    subscription.id,
    subscription.name,
    subscription.enabled,
    subscription.token_version,
    subscription.category_ids,
    subscription.promotion_filters,
    subscription.price_filter,
    subscription.bookmarked_only,
    subscription.item_limit,
    subscription.include_category,
    subscription.include_subtitle,
    subscription.include_size,
    subscription.include_promotion,
    subscription.version,
    subscription.created_at,
    subscription.updated_at;

-- name: RotateRSSSubscriptionToken :one
UPDATE rss.subscriptions
SET
    token_sha256 = sqlc.arg(token_sha256)::bytea,
    token_version = token_version + 1,
    version = version + 1,
    updated_at = sqlc.arg(updated_at)::timestamptz
WHERE id = sqlc.arg(id)::uuid
  AND user_id = sqlc.arg(user_id)::uuid
  AND version = sqlc.arg(expected_version)::bigint
  AND revoked_at IS NULL
RETURNING
    id,
    name,
    enabled,
    token_version,
    category_ids,
    promotion_filters,
    price_filter,
    bookmarked_only,
    item_limit,
    include_category,
    include_subtitle,
    include_size,
    include_promotion,
    version,
    created_at,
    updated_at;

-- name: RevokeRSSSubscription :execrows
UPDATE rss.subscriptions
SET
    enabled = false,
    version = version + 1,
    updated_at = sqlc.arg(revoked_at)::timestamptz,
    revoked_at = sqlc.arg(revoked_at)::timestamptz
WHERE id = sqlc.arg(id)::uuid
  AND user_id = sqlc.arg(user_id)::uuid
  AND version = sqlc.arg(expected_version)::bigint
  AND revoked_at IS NULL;

-- name: DeleteRSSFeedCache :exec
DELETE FROM rss.feed_cache
WHERE subscription_id = sqlc.arg(subscription_id)::uuid;

-- name: ResolveRSSSubscriptionByToken :one
SELECT
    subscription.id,
    subscription.user_id,
    subscription.name,
    subscription.token_version,
    subscription.category_ids,
    subscription.promotion_filters,
    subscription.price_filter,
    subscription.bookmarked_only,
    subscription.item_limit,
    subscription.include_category,
    subscription.include_subtitle,
    subscription.include_size,
    subscription.include_promotion,
    subscription.version,
    subscription.created_at,
    subscription.updated_at,
    users.credential_ref,
    users.username,
    users.display_name,
    users.email_verified_at,
    settings.cache_ttl_seconds,
    settings.max_items_per_feed,
    settings.requests_per_minute
FROM rss.subscriptions AS subscription
JOIN identity.users AS users ON users.id = subscription.user_id
JOIN rss.settings AS settings ON settings.singleton = true
WHERE subscription.token_sha256 = sqlc.arg(token_sha256)::bytea
  AND subscription.enabled = true
  AND subscription.revoked_at IS NULL
  AND settings.enabled = true
  AND users.status = 'active'
  AND users.email_verified_at IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= sqlc.arg(as_of)::timestamptz
        AND restriction.expires_at > sqlc.arg(as_of)::timestamptz
  );

-- name: ConsumeRSSRequestAllowance :one
INSERT INTO rss.user_rate_limits (
    user_id,
    window_started_at,
    request_count,
    updated_at
) VALUES (
    sqlc.arg(user_id)::uuid,
    sqlc.arg(requested_at)::timestamptz,
    1,
    sqlc.arg(requested_at)::timestamptz
)
ON CONFLICT (user_id) DO UPDATE
SET
    window_started_at = CASE
        WHEN rss.user_rate_limits.window_started_at + interval '1 minute' <= EXCLUDED.updated_at
        THEN EXCLUDED.updated_at
        ELSE rss.user_rate_limits.window_started_at
    END,
    request_count = CASE
        WHEN rss.user_rate_limits.window_started_at + interval '1 minute' <= EXCLUDED.updated_at
        THEN 1
        ELSE rss.user_rate_limits.request_count + 1
    END,
    updated_at = EXCLUDED.updated_at
RETURNING window_started_at, request_count;

-- name: GetRSSContentRevision :one
SELECT revision
FROM rss.content_state
WHERE singleton = true;

-- name: GetValidRSSFeedCache :one
SELECT
    observed_at,
    expires_at,
    next_boundary_at,
    item_projection
FROM rss.feed_cache
WHERE subscription_id = sqlc.arg(subscription_id)::uuid
  AND subscription_version = sqlc.arg(subscription_version)::bigint
  AND content_revision = sqlc.arg(content_revision)::bigint
  AND expires_at > sqlc.arg(as_of)::timestamptz;

-- name: UpsertRSSFeedCache :exec
INSERT INTO rss.feed_cache (
    subscription_id,
    subscription_version,
    content_revision,
    observed_at,
    expires_at,
    next_boundary_at,
    item_projection,
    updated_at
) VALUES (
    sqlc.arg(subscription_id)::uuid,
    sqlc.arg(subscription_version)::bigint,
    sqlc.arg(content_revision)::bigint,
    sqlc.arg(observed_at)::timestamptz,
    sqlc.arg(expires_at)::timestamptz,
    sqlc.narg(next_boundary_at)::timestamptz,
    sqlc.arg(item_projection)::jsonb,
    sqlc.arg(updated_at)::timestamptz
)
ON CONFLICT (subscription_id) DO UPDATE
SET
    subscription_version = EXCLUDED.subscription_version,
    content_revision = EXCLUDED.content_revision,
    observed_at = EXCLUDED.observed_at,
    expires_at = EXCLUDED.expires_at,
    next_boundary_at = EXCLUDED.next_boundary_at,
    item_projection = EXCLUDED.item_projection,
    updated_at = EXCLUDED.updated_at;

-- name: ListRSSFeedItems :many
SELECT
    torrent.id,
    torrent.name,
    torrent.subtitle,
    torrent.size_bytes,
    coalesce(effective.promotion, CASE
        WHEN torrent.promotion_ends_at IS NOT NULL
         AND torrent.promotion_ends_at <= sqlc.arg(observed_at)::timestamptz THEN 'none'
        ELSE torrent.promotion
    END)::text AS promotion,
    (CASE
        WHEN effective.campaign_id IS NOT NULL THEN effective.ends_at
        WHEN torrent.promotion_ends_at IS NOT NULL
         AND torrent.promotion_ends_at > sqlc.arg(observed_at)::timestamptz THEN torrent.promotion_ends_at
        ELSE NULL
    END)::timestamptz AS promotion_ends_at,
    sticky.sticky_ends_at AS sticky_until,
    torrent.published_at,
    category.id AS category_id,
    category.name AS category_name,
    coalesce(swarm.seeders, 0)::integer AS seeders,
    coalesce(swarm.leechers, 0)::integer AS leechers,
    coalesce(completion.completed, swarm.completed, 0)::integer AS completed,
    aggregate.purchase_price
FROM catalog.torrents AS torrent
JOIN torrents.torrents AS aggregate
  ON aggregate.id = torrent.id
 AND aggregate.state = 'published'
JOIN catalog.categories AS category
  ON category.id = torrent.category_id
 AND category.enabled = true
LEFT JOIN catalog.torrent_swarm_stats AS swarm ON swarm.torrent_id = torrent.id
LEFT JOIN catalog.torrent_completion_stats AS completion ON completion.torrent_id = torrent.id
LEFT JOIN LATERAL promotion.effective_for_torrent(
    torrent.id,
    sqlc.arg(observed_at)::timestamptz
) AS effective ON true
LEFT JOIN LATERAL (
    SELECT max(product_order.sticky_ends_at)::timestamptz AS sticky_ends_at
    FROM promotion.product_orders AS product_order
    WHERE product_order.torrent_id = torrent.id
      AND product_order.sticky_starts_at <= sqlc.arg(observed_at)::timestamptz
      AND product_order.sticky_ends_at > sqlc.arg(observed_at)::timestamptz
) AS sticky ON true
WHERE (
        cardinality(sqlc.arg(category_ids)::text[]) = 0
        OR torrent.category_id = ANY(sqlc.arg(category_ids)::text[])
    )
  AND (
        cardinality(sqlc.arg(promotion_filters)::text[]) = 0
        OR coalesce(effective.promotion, CASE
            WHEN torrent.promotion_ends_at IS NOT NULL
             AND torrent.promotion_ends_at <= sqlc.arg(observed_at)::timestamptz THEN 'none'
            ELSE torrent.promotion
        END) = ANY(sqlc.arg(promotion_filters)::text[])
    )
  AND CASE sqlc.arg(price_filter)::text
        WHEN 'free' THEN aggregate.purchase_price = 0
        WHEN 'paid' THEN aggregate.purchase_price > 0
        ELSE true
      END
  AND (
        NOT sqlc.arg(bookmarked_only)::boolean
        OR EXISTS (
            SELECT 1
            FROM catalog.torrent_bookmarks AS bookmark
            WHERE bookmark.user_id = sqlc.arg(user_id)::uuid
              AND bookmark.torrent_id = torrent.id
        )
    )
ORDER BY
    (sticky.sticky_ends_at IS NOT NULL) DESC,
    sticky.sticky_ends_at DESC,
    torrent.published_at DESC,
    torrent.id DESC
LIMIT sqlc.arg(result_limit)::integer;

-- name: GetNextRSSContentBoundary :one
SELECT min(boundary)::timestamptz AS boundary
FROM (
    SELECT campaign.starts_at AS boundary
    FROM promotion.campaigns AS campaign
    JOIN promotion.delivery_outbox AS delivery
      ON delivery.campaign_id = campaign.id
     AND delivery.delivered_at IS NOT NULL
    WHERE campaign.starts_at > sqlc.arg(observed_at)::timestamptz
    UNION ALL
    SELECT campaign.ends_at
    FROM promotion.campaigns AS campaign
    JOIN promotion.delivery_outbox AS delivery
      ON delivery.campaign_id = campaign.id
     AND delivery.delivered_at IS NOT NULL
    WHERE campaign.ends_at > sqlc.arg(observed_at)::timestamptz
    UNION ALL
    SELECT torrent.promotion_ends_at
    FROM catalog.torrents AS torrent
    WHERE torrent.promotion_ends_at > sqlc.arg(observed_at)::timestamptz
    UNION ALL
    SELECT product_order.promotion_starts_at
    FROM promotion.product_orders AS product_order
    WHERE product_order.promotion_starts_at > sqlc.arg(observed_at)::timestamptz
    UNION ALL
    SELECT product_order.promotion_ends_at
    FROM promotion.product_orders AS product_order
    WHERE product_order.promotion_ends_at > sqlc.arg(observed_at)::timestamptz
    UNION ALL
    SELECT product_order.sticky_starts_at
    FROM promotion.product_orders AS product_order
    WHERE product_order.sticky_starts_at > sqlc.arg(observed_at)::timestamptz
    UNION ALL
    SELECT product_order.sticky_ends_at
    FROM promotion.product_orders AS product_order
    WHERE product_order.sticky_ends_at > sqlc.arg(observed_at)::timestamptz
) AS boundaries;
