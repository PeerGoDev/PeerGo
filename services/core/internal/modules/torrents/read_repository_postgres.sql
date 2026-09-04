-- name: GetPublishedTorrentDetail :one
SELECT
    torrent.id,
    torrent.category_id,
    category.name AS category_name,
    torrent.title,
    torrent.subtitle,
    torrent.content_name,
    (CASE WHEN torrent.anonymous THEN '匿名' ELSE uploader.display_name END)::text AS uploader_display_name,
    torrent.anonymous,
    coalesce(effective.promotion, CASE
        WHEN projection.promotion_ends_at IS NOT NULL
         AND projection.promotion_ends_at <= CURRENT_TIMESTAMP THEN 'none'
        ELSE projection.promotion
    END)::text AS promotion,
    (CASE WHEN effective.campaign_id IS NOT NULL THEN effective.ends_at ELSE CASE
        WHEN projection.promotion_ends_at IS NOT NULL
         AND projection.promotion_ends_at <= CURRENT_TIMESTAMP THEN NULL
        ELSE projection.promotion_ends_at
    END END)::timestamptz AS promotion_ends_at,
	sticky.sticky_ends_at AS sticky_until,
    torrent.info_hash_v1,
    torrent.total_size_bytes,
    torrent.payload_size_bytes,
    torrent.file_count,
    torrent.padding_file_count,
    coalesce((
        SELECT count(*)
        FROM torrents.torrent_screenshot_set_heads AS head
        JOIN torrents.torrent_screenshot_set_items AS screenshot
          ON screenshot.set_id = head.active_set_id
        WHERE head.torrent_id = torrent.id
    ), 0)::integer AS screenshot_count,
    torrent.piece_length_bytes,
    torrent.piece_count,
    torrent.state,
    torrent.submitted_at,
    torrent.published_at
FROM torrents.torrents AS torrent
JOIN catalog.categories AS category ON category.id = torrent.category_id
JOIN catalog.torrents AS projection ON projection.id = torrent.id
JOIN identity.users AS uploader ON uploader.id = torrent.uploader_id
LEFT JOIN LATERAL promotion.effective_for_torrent(torrent.id, CURRENT_TIMESTAMP) AS effective ON true
LEFT JOIN LATERAL (
    SELECT max(product_order.sticky_ends_at)::timestamptz AS sticky_ends_at
    FROM promotion.product_orders AS product_order
    WHERE product_order.torrent_id = torrent.id
      AND product_order.sticky_starts_at <= CURRENT_TIMESTAMP
      AND product_order.sticky_ends_at > CURRENT_TIMESTAMP
) AS sticky ON true
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
  AND torrent.state = 'published';

-- name: ListPublishedTorrentFacetValues :many
SELECT
    value.facet_id,
    facet.name AS facet_name,
    value.option_key,
    option.label AS option_label
FROM torrents.torrent_facet_values AS value
JOIN catalog.facet_definitions AS facet ON facet.id = value.facet_id
JOIN catalog.facet_options AS option
  ON option.facet_id = value.facet_id
 AND option.option_key = value.option_key
WHERE value.torrent_id = sqlc.arg(torrent_id)::bigint
ORDER BY facet.display_order, value.position, value.option_key;

-- name: GetPendingReviewEvidence :one
SELECT
    torrent.id,
    torrent.category_id,
    category.name AS category_name,
    torrent.title,
    torrent.subtitle,
    torrent.content_name,
    uploader.display_name AS uploader_display_name,
    torrent.anonymous,
    torrent.info_hash_v1,
    torrent.total_size_bytes,
    torrent.payload_size_bytes,
    torrent.file_count,
    torrent.padding_file_count,
    coalesce((
        SELECT count(*)
        FROM torrents.torrent_screenshot_set_heads AS head
        JOIN torrents.torrent_screenshot_set_items AS screenshot
          ON screenshot.set_id = head.active_set_id
        WHERE head.torrent_id = torrent.id
    ), 0)::integer AS screenshot_count,
    torrent.piece_length_bytes,
    torrent.piece_count,
    torrent.state,
    torrent.version,
    torrent.submitted_at,
    torrent.state_changed_at AS review_requested_at,
    torrent.description,
    torrent.description_format,
    torrent.media_info
FROM torrents.torrents AS torrent
JOIN catalog.categories AS category ON category.id = torrent.category_id
JOIN identity.users AS uploader ON uploader.id = torrent.uploader_id
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
  AND torrent.state = 'pending_review';

-- name: PendingReviewTorrentOwnedBy :one
SELECT EXISTS (
    SELECT 1
    FROM torrents.torrents AS torrent
    WHERE torrent.id = sqlc.arg(torrent_id)::bigint
      AND torrent.uploader_id = sqlc.arg(uploader_id)::uuid
      AND torrent.state = 'pending_review'
);

-- name: ListPublishedTorrentExternalIdentifiers :many
SELECT identifier.provider, identifier.external_id
FROM torrents.torrent_external_identifiers AS identifier
WHERE identifier.torrent_id = sqlc.arg(torrent_id)::bigint
ORDER BY identifier.provider;

-- name: GetPublishedTorrentCoverObject :one
SELECT
    torrent.id AS torrent_id,
    object.id AS object_id,
    object.content_sha256,
    object.byte_length,
    object.content_type,
    object.width,
    object.height
FROM torrents.torrents AS torrent
JOIN torrents.torrent_screenshot_set_heads AS head
  ON head.torrent_id = torrent.id
JOIN torrents.torrent_screenshot_set_items AS screenshot
  ON screenshot.set_id = head.active_set_id
 AND screenshot.position = 0
JOIN torrents.torrent_screenshot_objects AS object
  ON object.id = screenshot.object_id
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
  AND torrent.state = 'published';

-- name: GetPendingReviewCoverObject :one
SELECT
    torrent.id AS torrent_id,
    object.id AS object_id,
    object.content_sha256,
    object.byte_length,
    object.content_type,
    object.width,
    object.height
FROM torrents.torrents AS torrent
JOIN torrents.torrent_screenshot_set_heads AS head
  ON head.torrent_id = torrent.id
JOIN torrents.torrent_screenshot_set_items AS screenshot
  ON screenshot.set_id = head.active_set_id
 AND screenshot.position = 0
JOIN torrents.torrent_screenshot_objects AS object
  ON object.id = screenshot.object_id
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
  AND torrent.state = 'pending_review';

-- name: ListPublishedTorrentCoverLocations :many
SELECT
    location.id,
    location.object_id,
    location.backend_id,
    location.object_key,
    location.state,
    location.is_preferred,
    location.version_id,
    location.observed_byte_length,
    location.observed_sha256,
    location.verified_at
FROM torrents.torrent_screenshot_object_locations AS location
WHERE location.object_id = sqlc.arg(object_id)::uuid
  AND location.state IN ('verified', 'retiring')
ORDER BY location.is_preferred DESC, (location.state = 'verified') DESC,
         location.verified_at DESC, location.id;

-- name: GetPublishedTorrentScreenshotObject :one
SELECT
    torrent.id AS torrent_id,
    screenshot.position,
    object.id AS object_id,
    object.content_sha256,
    object.byte_length,
    object.content_type,
    object.width,
    object.height
FROM torrents.torrents AS torrent
JOIN torrents.torrent_screenshot_set_heads AS head
  ON head.torrent_id = torrent.id
JOIN torrents.torrent_screenshot_set_items AS screenshot
  ON screenshot.set_id = head.active_set_id
 AND screenshot.position = sqlc.arg(screenshot_position)::smallint
JOIN torrents.torrent_screenshot_objects AS object
  ON object.id = screenshot.object_id
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
  AND torrent.state = 'published';

-- name: GetPendingReviewScreenshotObject :one
SELECT
    torrent.id AS torrent_id,
    screenshot.position,
    object.id AS object_id,
    object.content_sha256,
    object.byte_length,
    object.content_type,
    object.width,
    object.height
FROM torrents.torrents AS torrent
JOIN torrents.torrent_screenshot_set_heads AS head
  ON head.torrent_id = torrent.id
JOIN torrents.torrent_screenshot_set_items AS screenshot
  ON screenshot.set_id = head.active_set_id
 AND screenshot.position = sqlc.arg(screenshot_position)::smallint
JOIN torrents.torrent_screenshot_objects AS object
  ON object.id = screenshot.object_id
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
  AND torrent.state = 'pending_review';

-- name: ListPublishedTorrentScreenshotLocations :many
SELECT
    location.id,
    location.object_id,
    location.backend_id,
    location.object_key,
    location.state,
    location.is_preferred,
    location.version_id,
    location.observed_byte_length,
    location.observed_sha256,
    location.verified_at
FROM torrents.torrent_screenshot_object_locations AS location
WHERE location.object_id = sqlc.arg(object_id)::uuid
  AND location.state IN ('verified', 'retiring')
ORDER BY location.is_preferred DESC, (location.state = 'verified') DESC,
         location.verified_at DESC, location.id;

-- name: GetPublishedTorrentContent :one
SELECT
    torrent.id AS torrent_id,
    torrent.description,
    torrent.description_format,
    torrent.media_info
FROM torrents.torrents AS torrent
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
  AND torrent.state = 'published';

-- name: ListPublishedRelatedTorrentVersions :many
WITH target AS (
    SELECT torrent.resource_group_id
    FROM torrents.torrents AS torrent
    WHERE torrent.id = sqlc.arg(torrent_id)::bigint
      AND torrent.state = 'published'
)
SELECT
    projection.id,
    projection.name,
    projection.subtitle,
    projection.size_bytes,
    coalesce(effective.promotion, CASE
        WHEN projection.promotion_ends_at IS NOT NULL
         AND projection.promotion_ends_at <= CURRENT_TIMESTAMP THEN 'none'
        ELSE projection.promotion
    END)::text AS promotion,
	sticky.sticky_ends_at AS sticky_until,
    projection.published_at,
    category.id AS category_id,
    category.name AS category_name,
    coalesce(swarm.seeders, 0)::integer AS seeders,
    coalesce(swarm.leechers, 0)::integer AS leechers,
    coalesce(completion.completed, swarm.completed, 0)::integer AS completed,
    coalesce(swarm.observed_at, to_timestamp(0))::timestamptz AS observed_at
FROM target
JOIN torrents.torrents AS sibling
  ON sibling.resource_group_id = target.resource_group_id
 AND sibling.id <> sqlc.arg(torrent_id)::bigint
 AND sibling.state = 'published'
JOIN catalog.torrents AS projection ON projection.id = sibling.id
JOIN catalog.categories AS category
  ON category.id = projection.category_id
 AND category.enabled = true
LEFT JOIN catalog.torrent_swarm_stats AS swarm ON swarm.torrent_id = projection.id
LEFT JOIN catalog.torrent_completion_stats AS completion ON completion.torrent_id = projection.id
LEFT JOIN LATERAL promotion.effective_for_torrent(projection.id, CURRENT_TIMESTAMP) AS effective ON true
LEFT JOIN LATERAL (
    SELECT max(product_order.sticky_ends_at)::timestamptz AS sticky_ends_at
    FROM promotion.product_orders AS product_order
    WHERE product_order.torrent_id = projection.id
      AND product_order.sticky_starts_at <= CURRENT_TIMESTAMP
      AND product_order.sticky_ends_at > CURRENT_TIMESTAMP
) AS sticky ON true
WHERE target.resource_group_id IS NOT NULL
ORDER BY projection.published_at DESC, projection.id DESC
LIMIT sqlc.arg(result_limit)::integer;

-- name: GetPublishedTorrentFileTarget :one
SELECT
    torrent.id,
    torrent.file_count
FROM torrents.torrents AS torrent
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
  AND torrent.state = 'published';

-- name: GetPendingReviewFileTarget :one
SELECT
    torrent.id,
    torrent.file_count
FROM torrents.torrents AS torrent
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
  AND torrent.state = 'pending_review';

-- name: ListPublishedTorrentFiles :many
SELECT
    file.file_index,
    file.display_path,
    file.size_bytes,
    file.is_padding
FROM torrents.torrent_files AS file
WHERE file.torrent_id = sqlc.arg(torrent_id)::bigint
ORDER BY file.file_index
LIMIT sqlc.arg(result_limit)::integer
OFFSET sqlc.arg(result_offset)::integer;

-- name: ListUserTorrentSubmissions :many
SELECT
    torrent.id,
    torrent.category_id,
    category.name AS category_name,
    torrent.title,
    torrent.subtitle,
    torrent.content_name,
    torrent.info_hash_v1,
    torrent.total_size_bytes,
    torrent.file_count,
    torrent.state,
    torrent.version,
    torrent.submitted_at,
    torrent.published_at,
    torrent.state_changed_at,
    COALESCE(latest_review.resulting_state, '') AS review_outcome,
    COALESCE(latest_review.reason_code, '') AS review_reason_code,
    COALESCE(latest_review.reason, '') AS review_reason,
    latest_review.occurred_at AS review_occurred_at,
    COALESCE(latest_content_change.status, '') AS content_change_status,
    latest_content_change.created_at AS content_change_created_at,
    latest_content_change.decided_at AS content_change_decided_at,
    COALESCE(latest_content_change.decision_reason, '') AS content_change_decision_reason,
    COALESCE(latest_screenshot_change.status, '') AS screenshot_change_status,
    latest_screenshot_change.created_at AS screenshot_change_created_at,
    latest_screenshot_change.decided_at AS screenshot_change_decided_at,
    COALESCE(latest_screenshot_change.decision_reason, '') AS screenshot_change_decision_reason,
    COALESCE(latest_withdrawal.status, '') AS withdrawal_status,
    latest_withdrawal.created_at AS withdrawal_created_at,
    latest_withdrawal.decided_at AS withdrawal_decided_at,
    COALESCE(latest_withdrawal.decision_reason, '') AS withdrawal_decision_reason,
    count(*) OVER ()::bigint AS total_count
FROM torrents.torrents AS torrent
JOIN catalog.categories AS category ON category.id = torrent.category_id
LEFT JOIN LATERAL (
    SELECT
        decision.resulting_state,
        decision.reason_code,
        decision.reason,
        decision.occurred_at
    FROM review.torrent_decisions AS decision
    WHERE decision.torrent_id = torrent.id
    ORDER BY decision.occurred_at DESC, decision.id DESC
    LIMIT 1
) AS latest_review ON true
LEFT JOIN LATERAL (
    SELECT
        request.status,
        request.created_at,
        request.decided_at,
        decision.reason AS decision_reason
    FROM torrents.torrent_content_change_requests AS request
    LEFT JOIN torrents.torrent_content_change_decisions AS decision
      ON decision.request_id = request.id
    WHERE request.torrent_id = torrent.id
    ORDER BY request.created_at DESC, request.id DESC
    LIMIT 1
) AS latest_content_change ON true
LEFT JOIN LATERAL (
    SELECT
        request.status,
        request.created_at,
        request.decided_at,
        decision.reason AS decision_reason
    FROM torrents.torrent_screenshot_change_requests AS request
    LEFT JOIN torrents.torrent_screenshot_change_decisions AS decision
      ON decision.request_id = request.id
    WHERE request.torrent_id = torrent.id
    ORDER BY request.created_at DESC, request.id DESC
    LIMIT 1
) AS latest_screenshot_change ON true
LEFT JOIN LATERAL (
    SELECT
        request.status,
        request.created_at,
        request.decided_at,
        decision.reason AS decision_reason
    FROM torrents.torrent_withdrawal_requests AS request
    LEFT JOIN torrents.torrent_withdrawal_decisions AS decision
      ON decision.request_id = request.id
    WHERE request.torrent_id = torrent.id
    ORDER BY request.created_at DESC, request.id DESC
    LIMIT 1
) AS latest_withdrawal ON true
WHERE torrent.uploader_id = sqlc.arg(uploader_id)::uuid
ORDER BY torrent.submitted_at DESC, torrent.id DESC
LIMIT sqlc.arg(result_limit)::integer;
