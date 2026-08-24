-- name: ListManagedTorrents :many
SELECT
    torrent.id,
    uploader.numeric_id AS uploader_numeric_id,
    uploader.username AS uploader_username,
    uploader.display_name AS uploader_display_name,
    torrent.category_id,
    category.name AS category_name,
    torrent.title,
    torrent.subtitle,
    torrent.total_size_bytes,
    torrent.purchase_price,
    torrent.state,
    torrent.version,
    coalesce(effective.promotion, CASE
        WHEN projection.promotion_ends_at IS NOT NULL
         AND projection.promotion_ends_at <= CURRENT_TIMESTAMP THEN 'none'
        ELSE coalesce(projection.promotion, 'none')
    END)::text AS promotion,
    (CASE WHEN effective.campaign_id IS NOT NULL THEN effective.ends_at ELSE CASE
        WHEN projection.promotion_ends_at IS NOT NULL
         AND projection.promotion_ends_at <= CURRENT_TIMESTAMP THEN NULL
        ELSE projection.promotion_ends_at
    END END)::timestamptz AS promotion_ends_at,
    coalesce(swarm.seeders, 0)::integer AS seeders,
    coalesce(swarm.leechers, 0)::integer AS leechers,
    coalesce(completion.completed, swarm.completed, 0)::integer AS completed,
    torrent.submitted_at,
    torrent.published_at,
    torrent.state_changed_at,
    torrent.updated_at
FROM torrents.torrents AS torrent
JOIN identity.users AS uploader ON uploader.id = torrent.uploader_id
JOIN catalog.categories AS category ON category.id = torrent.category_id
LEFT JOIN catalog.torrents AS projection ON projection.id = torrent.id
LEFT JOIN catalog.torrent_swarm_stats AS swarm ON swarm.torrent_id = torrent.id
LEFT JOIN catalog.torrent_completion_stats AS completion ON completion.torrent_id = torrent.id
LEFT JOIN LATERAL promotion.effective_for_torrent(torrent.id, CURRENT_TIMESTAMP) AS effective ON true
WHERE
    (
        sqlc.arg(search_text)::text = ''
        OR position(lower(sqlc.arg(search_text)::text) IN lower(torrent.title || ' ' || torrent.subtitle)) > 0
        OR torrent.id::text = sqlc.arg(search_text)::text
        OR position(lower(sqlc.arg(search_text)::text) IN lower(uploader.username || ' ' || uploader.display_name)) > 0
    )
    AND (sqlc.arg(torrent_state)::text = '' OR torrent.state = sqlc.arg(torrent_state)::text)
    AND (sqlc.arg(category_id)::text = '' OR torrent.category_id = sqlc.arg(category_id)::text)
ORDER BY torrent.updated_at DESC, torrent.id DESC
LIMIT sqlc.arg(result_limit)::integer
OFFSET sqlc.arg(result_offset)::integer;

-- name: CountManagedTorrents :one
SELECT count(*)::bigint
FROM torrents.torrents AS torrent
JOIN identity.users AS uploader ON uploader.id = torrent.uploader_id
WHERE
    (
        sqlc.arg(search_text)::text = ''
        OR position(lower(sqlc.arg(search_text)::text) IN lower(torrent.title || ' ' || torrent.subtitle)) > 0
        OR torrent.id::text = sqlc.arg(search_text)::text
        OR position(lower(sqlc.arg(search_text)::text) IN lower(uploader.username || ' ' || uploader.display_name)) > 0
    )
    AND (sqlc.arg(torrent_state)::text = '' OR torrent.state = sqlc.arg(torrent_state)::text)
    AND (sqlc.arg(category_id)::text = '' OR torrent.category_id = sqlc.arg(category_id)::text);

-- name: ListManagedTorrentFilterCategories :many
SELECT id, name, enabled
FROM catalog.categories
ORDER BY display_order, id;

-- name: CountManagedTorrentsByState :many
SELECT state, count(*)::bigint AS torrent_count
FROM torrents.torrents
GROUP BY state
ORDER BY state;

-- name: GetManagedTorrentPeerTarget :one
SELECT
    torrent.id,
    torrent.info_hash_v1,
    torrent.total_size_bytes,
    torrent.uploader_id,
    torrent.anonymous
FROM torrents.torrents AS torrent
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
  AND torrent.state = 'published';

-- name: ListManagedTorrentPeerIdentities :many
SELECT users.id, users.numeric_id, users.username, users.display_name
FROM identity.users AS users
WHERE users.id = ANY(sqlc.arg(user_ids)::uuid[])
ORDER BY lower(users.username), users.id;

-- name: GetManagedTorrentLifecycleChange :one
SELECT
    id,
    torrent_id,
    actor_id,
    action,
    reason,
    expected_torrent_version,
    resulting_torrent_version,
    before_state,
    after_state,
    occurred_at
FROM torrents.torrent_lifecycle_changes
WHERE id = sqlc.arg(change_id)::uuid;

-- name: GetManagedTorrentForAvailabilityUpdate :one
SELECT
    torrent.id,
    torrent.uploader_id,
    torrent.category_id,
    category.enabled AS category_enabled,
    torrent.info_hash_v1,
    torrent.total_size_bytes,
    torrent.state,
    torrent.version,
    torrent.submitted_at,
    torrent.published_at,
    torrent.state_changed_at,
    torrent.updated_at,
    EXISTS (
        SELECT 1
        FROM torrents.torrent_object_locations AS location
        WHERE location.object_id = torrent.object_id
          AND location.state = 'verified'
          AND location.verified_at IS NOT NULL
    ) AS has_verified_location
FROM torrents.torrents AS torrent
JOIN catalog.categories AS category ON category.id = torrent.category_id
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
FOR UPDATE OF torrent;

-- name: ChangeManagedTorrentAvailability :one
UPDATE torrents.torrents
SET
    state = sqlc.arg(resulting_state)::text,
    version = version + 1,
    state_changed_at = sqlc.arg(occurred_at)::timestamptz,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE id = sqlc.arg(torrent_id)::bigint
  AND state = sqlc.arg(expected_state)::text
  AND version = sqlc.arg(expected_version)::bigint
RETURNING id, state, version, state_changed_at;

-- name: InsertManagedTorrentLifecycleChange :exec
INSERT INTO torrents.torrent_lifecycle_changes (
    id,
    torrent_id,
    actor_id,
    action,
    reason,
    expected_torrent_version,
    resulting_torrent_version,
    before_state,
    after_state,
    authorization_decision_id,
    occurred_at
) VALUES (
    sqlc.arg(change_id)::uuid,
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(actor_id)::uuid,
    sqlc.arg(lifecycle_action)::text,
    sqlc.arg(reason)::text,
    sqlc.arg(expected_version)::bigint,
    sqlc.arg(resulting_version)::bigint,
    sqlc.arg(before_state)::text,
    sqlc.arg(after_state)::text,
    sqlc.arg(authorization_decision_id)::uuid,
    sqlc.arg(occurred_at)::timestamptz
);
