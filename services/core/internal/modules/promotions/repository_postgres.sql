-- name: ListPromotionCampaigns :many
SELECT
    campaign.id,
	campaign.source_kind,
    campaign.scope_type,
    campaign.torrent_id,
    coalesce(torrent.title, '')::text AS torrent_title,
    campaign.promotion,
    campaign.starts_at,
    campaign.ends_at,
    campaign.override_lower_scopes,
    campaign.reason,
    campaign.actor_id,
    campaign.created_at,
    delivery.attempts,
    coalesce(delivery.last_error_code, '')::text AS last_error_code,
    delivery.delivered_at
FROM promotion.campaigns AS campaign
JOIN promotion.delivery_outbox AS delivery ON delivery.campaign_id = campaign.id
LEFT JOIN torrents.torrents AS torrent ON torrent.id = campaign.torrent_id
ORDER BY campaign.starts_at DESC, campaign.id DESC
LIMIT sqlc.arg(result_limit)::integer
OFFSET sqlc.arg(result_offset)::integer;

-- name: CountPromotionCampaigns :one
SELECT count(*)::bigint FROM promotion.campaigns;

-- name: LockPromotionScheduling :exec
SELECT pg_advisory_xact_lock(hashtextextended('peergo-core-promotion-scheduling-v1', 0));

-- name: GetPromotionCampaign :one
SELECT
    campaign.id,
    campaign.scope_type,
    campaign.torrent_id,
    campaign.promotion,
    campaign.starts_at,
    campaign.ends_at,
    campaign.reason,
    campaign.actor_id,
    campaign.authorization_decision_id,
    campaign.command_json,
    campaign.command_sha256,
    campaign.created_at
FROM promotion.campaigns AS campaign
WHERE campaign.id = sqlc.arg(campaign_id)::uuid;

-- name: PromotionCampaignScopeOverlaps :one
SELECT EXISTS (
    SELECT 1
    FROM promotion.campaigns
    WHERE scope_type = sqlc.arg(scope_type)::text
      AND (sqlc.arg(scope_type)::text = 'global' OR torrent_id = sqlc.narg(torrent_id)::bigint)
      AND starts_at < sqlc.arg(ends_at)::timestamptz
      AND ends_at > sqlc.arg(starts_at)::timestamptz
) AS overlaps;

-- name: GlobalPromotionOverlapsMemberPurchase :one
SELECT EXISTS (
    SELECT 1
    FROM promotion.campaigns
    WHERE source_kind = 'member_purchase'
      AND starts_at < sqlc.arg(ends_at)::timestamptz
      AND ends_at > sqlc.arg(starts_at)::timestamptz
) AS overlaps;

-- name: GetPromotionTorrentTarget :one
SELECT id, title, state
FROM torrents.torrents
WHERE id = sqlc.arg(torrent_id)::bigint;

-- name: InsertPromotionCampaign :exec
INSERT INTO promotion.campaigns (
    id, scope_type, torrent_id, promotion, starts_at, ends_at,
    override_lower_scopes, reason, actor_id, authorization_decision_id,
    command_json, command_sha256, created_at, source_kind
) VALUES (
    sqlc.arg(campaign_id)::uuid,
    sqlc.arg(scope_type)::text,
    sqlc.narg(torrent_id)::bigint,
    sqlc.arg(promotion)::text,
    sqlc.arg(starts_at)::timestamptz,
    sqlc.arg(ends_at)::timestamptz,
    sqlc.arg(override_lower_scopes)::boolean,
    sqlc.arg(reason)::text,
    sqlc.arg(actor_id)::uuid,
    sqlc.arg(authorization_decision_id)::uuid,
    sqlc.arg(command_json)::text,
    sqlc.arg(command_sha256)::bytea,
	sqlc.arg(created_at)::timestamptz,
	'staff_schedule'
);

-- name: EnqueuePromotionDelivery :exec
INSERT INTO promotion.delivery_outbox (
    campaign_id, available_at, created_at
) VALUES (
    sqlc.arg(campaign_id)::uuid,
    sqlc.arg(available_at)::timestamptz,
    sqlc.arg(created_at)::timestamptz
);

-- name: ClaimPromotionDeliveries :many
WITH candidates AS (
    SELECT delivery.campaign_id
    FROM promotion.delivery_outbox AS delivery
    WHERE delivery.delivered_at IS NULL
      AND delivery.available_at <= sqlc.arg(claimed_at)::timestamptz
      AND (delivery.lease_until IS NULL OR delivery.lease_until <= sqlc.arg(claimed_at)::timestamptz)
    ORDER BY delivery.available_at, delivery.campaign_id
    FOR UPDATE SKIP LOCKED
    LIMIT sqlc.arg(batch_size)::integer
)
UPDATE promotion.delivery_outbox AS delivery
SET
    lease_token = sqlc.arg(lease_token)::uuid,
    lease_until = sqlc.arg(lease_until)::timestamptz,
    attempts = delivery.attempts + 1,
    last_error_code = NULL
FROM candidates
JOIN promotion.campaigns AS campaign ON campaign.id = candidates.campaign_id
WHERE delivery.campaign_id = candidates.campaign_id
RETURNING
    delivery.campaign_id,
    campaign.command_json,
    campaign.command_sha256,
    delivery.lease_token,
    delivery.attempts;

-- name: MarkPromotionDeliveryDelivered :execrows
UPDATE promotion.delivery_outbox
SET delivered_at = sqlc.arg(delivered_at)::timestamptz,
    lease_token = NULL,
    lease_until = NULL,
    last_error_code = NULL
WHERE campaign_id = sqlc.arg(campaign_id)::uuid
  AND lease_token = sqlc.arg(lease_token)::uuid
  AND delivered_at IS NULL;

-- name: ReleasePromotionDelivery :execrows
UPDATE promotion.delivery_outbox
SET available_at = sqlc.arg(available_at)::timestamptz,
    lease_token = NULL,
    lease_until = NULL,
    last_error_code = sqlc.arg(last_error_code)::text
WHERE campaign_id = sqlc.arg(campaign_id)::uuid
  AND lease_token = sqlc.arg(lease_token)::uuid
  AND delivered_at IS NULL;
