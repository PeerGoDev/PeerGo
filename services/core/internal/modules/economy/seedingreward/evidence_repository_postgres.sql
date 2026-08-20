-- name: InsertSeedingRewardEvidenceInbox :one
INSERT INTO economy.seeding_reward_evidence_inbox (
    event_id,
    window_start,
    chunk_index,
    payload_sha256,
    payload_json,
    received_at,
    applied_at
) VALUES (
    sqlc.arg(event_id)::uuid,
    sqlc.arg(window_start)::timestamptz,
    sqlc.arg(chunk_index)::integer,
    sqlc.arg(payload_sha256)::bytea,
    sqlc.arg(payload_json)::text,
    sqlc.arg(received_at)::timestamptz,
    sqlc.arg(applied_at)::timestamptz
)
ON CONFLICT (event_id) DO NOTHING
RETURNING event_id;

-- name: GetSeedingRewardEvidenceInbox :one
SELECT window_start, chunk_index, payload_sha256, payload_json
FROM economy.seeding_reward_evidence_inbox
WHERE event_id = sqlc.arg(event_id)::uuid;

-- name: InsertSeedingRewardEvidenceWindow :one
INSERT INTO economy.seeding_reward_evidence_windows (
    window_start,
    window_end,
    built_at,
    window_evidence_sha256,
    projection_sha256,
    snapshot_id,
    snapshot_sequence,
    snapshot_observed_at,
    item_count,
    chunk_count,
    created_at
) VALUES (
    sqlc.arg(window_start)::timestamptz,
    sqlc.arg(window_end)::timestamptz,
    sqlc.arg(built_at)::timestamptz,
    sqlc.arg(window_evidence_sha256)::bytea,
    sqlc.arg(projection_sha256)::bytea,
    sqlc.arg(snapshot_id)::uuid,
    sqlc.arg(snapshot_sequence)::bigint,
    sqlc.arg(snapshot_observed_at)::timestamptz,
    sqlc.arg(item_count)::integer,
    sqlc.arg(chunk_count)::integer,
    sqlc.arg(created_at)::timestamptz
)
ON CONFLICT (window_start) DO NOTHING
RETURNING window_start;

-- name: GetSeedingRewardEvidenceWindowForUpdate :one
SELECT
    window_start,
    window_end,
    built_at,
    window_evidence_sha256,
    projection_sha256,
    snapshot_id,
    snapshot_sequence,
    snapshot_observed_at,
    item_count,
    chunk_count,
    received_chunk_count,
    status,
    completed_at
FROM economy.seeding_reward_evidence_windows
WHERE window_start = sqlc.arg(window_start)::timestamptz
FOR UPDATE;

-- name: InsertSeedingRewardEvidenceChunk :one
INSERT INTO economy.seeding_reward_evidence_chunks (
    window_start,
    chunk_index,
    event_id,
    payload_sha256
) VALUES (
    sqlc.arg(window_start)::timestamptz,
    sqlc.arg(chunk_index)::integer,
    sqlc.arg(event_id)::uuid,
    sqlc.arg(payload_sha256)::bytea
)
ON CONFLICT (window_start, chunk_index) DO NOTHING
RETURNING event_id;

-- name: GetSeedingRewardEvidenceChunk :one
SELECT event_id, payload_sha256
FROM economy.seeding_reward_evidence_chunks
WHERE window_start = sqlc.arg(window_start)::timestamptz
  AND chunk_index = sqlc.arg(chunk_index)::integer;

-- name: InsertSeedingRewardEvidenceItem :exec
INSERT INTO economy.seeding_reward_evidence_items (
    window_start,
    user_id,
    torrent_id,
    active_seconds,
    raw_uploaded_bytes,
    snapshot_seeders,
    snapshot_leechers,
    tracker_evidence_sha256
) VALUES (
    sqlc.arg(window_start)::timestamptz,
    sqlc.arg(user_id)::uuid,
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(active_seconds)::bigint,
    sqlc.arg(raw_uploaded_bytes)::bigint,
    sqlc.arg(snapshot_seeders)::integer,
    sqlc.arg(snapshot_leechers)::integer,
    sqlc.arg(tracker_evidence_sha256)::bytea
);

-- name: IncrementSeedingRewardEvidenceChunks :one
UPDATE economy.seeding_reward_evidence_windows
SET received_chunk_count = received_chunk_count + 1
WHERE window_start = sqlc.arg(window_start)::timestamptz
  AND status = 'collecting'
  AND received_chunk_count < chunk_count
RETURNING received_chunk_count, chunk_count;

-- name: ListSeedingRewardEvidenceItems :many
SELECT
    user_id,
    torrent_id,
    active_seconds,
    raw_uploaded_bytes,
    snapshot_seeders,
    snapshot_leechers,
    tracker_evidence_sha256
FROM economy.seeding_reward_evidence_items
WHERE window_start = sqlc.arg(window_start)::timestamptz
ORDER BY user_id, torrent_id;

-- name: MarkSeedingRewardEvidenceComplete :execrows
UPDATE economy.seeding_reward_evidence_windows
SET status = 'complete', completed_at = sqlc.arg(completed_at)::timestamptz
WHERE window_start = sqlc.arg(window_start)::timestamptz
  AND status = 'collecting'
  AND received_chunk_count = chunk_count;
