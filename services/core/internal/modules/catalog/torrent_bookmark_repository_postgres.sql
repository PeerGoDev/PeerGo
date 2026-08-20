-- name: CountTorrentBookmarks :one
SELECT count(*)::bigint
FROM catalog.torrent_bookmarks AS bookmark
JOIN catalog.torrents AS torrent ON torrent.id = bookmark.torrent_id
JOIN torrents.torrents AS aggregate
  ON aggregate.id = torrent.id
 AND aggregate.state = 'published'
JOIN catalog.categories AS category
    ON category.id = torrent.category_id
   AND category.enabled = true
WHERE bookmark.user_id = sqlc.arg(user_id)::uuid;

-- name: ListTorrentBookmarks :many
SELECT
    bookmark.torrent_id,
    bookmark.created_at AS bookmarked_at,
    torrent.name,
    torrent.subtitle,
    torrent.size_bytes,
    coalesce(effective.promotion, CASE
        WHEN torrent.promotion_ends_at IS NOT NULL
         AND torrent.promotion_ends_at <= CURRENT_TIMESTAMP THEN 'none'
        ELSE torrent.promotion
    END)::text AS promotion,
	sticky.sticky_ends_at AS sticky_until,
    torrent.published_at,
    category.id AS category_id,
    category.name AS category_name,
    coalesce(swarm.seeders, 0)::integer AS seeders,
    coalesce(swarm.leechers, 0)::integer AS leechers,
    coalesce(completion.completed, swarm.completed, 0)::integer AS completed,
    coalesce(swarm.observed_at, to_timestamp(0))::timestamptz AS observed_at
FROM catalog.torrent_bookmarks AS bookmark
JOIN catalog.torrents AS torrent ON torrent.id = bookmark.torrent_id
JOIN torrents.torrents AS aggregate
  ON aggregate.id = torrent.id
 AND aggregate.state = 'published'
JOIN catalog.categories AS category
    ON category.id = torrent.category_id
   AND category.enabled = true
LEFT JOIN catalog.torrent_swarm_stats AS swarm
    ON swarm.torrent_id = torrent.id
LEFT JOIN catalog.torrent_completion_stats AS completion
    ON completion.torrent_id = torrent.id
LEFT JOIN LATERAL promotion.effective_for_torrent(torrent.id, CURRENT_TIMESTAMP) AS effective ON true
LEFT JOIN LATERAL (
    SELECT max(product_order.sticky_ends_at)::timestamptz AS sticky_ends_at
    FROM promotion.product_orders AS product_order
    WHERE product_order.torrent_id = torrent.id
      AND product_order.sticky_starts_at <= CURRENT_TIMESTAMP
      AND product_order.sticky_ends_at > CURRENT_TIMESTAMP
) AS sticky ON true
WHERE bookmark.user_id = sqlc.arg(user_id)::uuid
ORDER BY bookmark.created_at DESC, bookmark.torrent_id DESC
LIMIT sqlc.arg(result_limit)::integer
OFFSET sqlc.arg(result_offset)::integer;

-- name: ListTorrentBookmarkStatuses :many
SELECT bookmark.torrent_id
FROM catalog.torrent_bookmarks AS bookmark
JOIN catalog.torrents AS torrent ON torrent.id = bookmark.torrent_id
JOIN torrents.torrents AS aggregate
  ON aggregate.id = torrent.id
 AND aggregate.state = 'published'
JOIN catalog.categories AS category
    ON category.id = torrent.category_id
   AND category.enabled = true
WHERE bookmark.user_id = sqlc.arg(user_id)::uuid
  AND bookmark.torrent_id = ANY(sqlc.arg(torrent_ids)::bigint[])
ORDER BY bookmark.torrent_id;

-- name: PutTorrentBookmark :one
WITH target AS (
    SELECT torrent.id
    FROM catalog.torrents AS torrent
    JOIN torrents.torrents AS aggregate
      ON aggregate.id = torrent.id
     AND aggregate.state = 'published'
    JOIN catalog.categories AS category
        ON category.id = torrent.category_id
       AND category.enabled = true
    WHERE torrent.id = sqlc.arg(torrent_id)::bigint
), inserted AS (
    INSERT INTO catalog.torrent_bookmarks (user_id, torrent_id, created_at)
    SELECT
        sqlc.arg(user_id)::uuid,
        target.id,
        sqlc.arg(created_at)::timestamptz
    FROM target
    ON CONFLICT (user_id, torrent_id) DO NOTHING
    RETURNING created_at
)
SELECT created_at FROM inserted
UNION ALL
SELECT bookmark.created_at
FROM catalog.torrent_bookmarks AS bookmark
JOIN target ON target.id = bookmark.torrent_id
WHERE bookmark.user_id = sqlc.arg(user_id)::uuid
  AND bookmark.torrent_id = sqlc.arg(torrent_id)::bigint
LIMIT 1;

-- name: DeleteTorrentBookmark :exec
DELETE FROM catalog.torrent_bookmarks
WHERE user_id = sqlc.arg(user_id)::uuid
  AND torrent_id = sqlc.arg(torrent_id)::bigint;
