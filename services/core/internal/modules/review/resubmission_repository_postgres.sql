-- name: GetTorrentResubmissionByID :one
SELECT
    resubmission.id,
    torrent.id AS torrent_id,
    torrent.uploader_id,
    resubmission.responds_to_decision_id,
    resubmission.expected_torrent_version,
    resubmission.resulting_torrent_version,
    resubmission.category_id,
    resubmission.title,
    resubmission.subtitle,
    resubmission.correction_note,
    resubmission.occurred_at
FROM review.torrent_resubmissions AS resubmission
JOIN torrents.torrents AS torrent ON torrent.id = resubmission.torrent_id
WHERE resubmission.id = sqlc.arg(resubmission_id)::uuid;

-- name: GetRejectedTorrentForResubmissionForUpdate :one
SELECT
    torrent.id,
    torrent.uploader_id,
    torrent.category_id,
    torrent.title,
    torrent.subtitle,
    torrent.info_hash_v1,
    torrent.total_size_bytes,
    torrent.state,
    torrent.version,
    torrent.submitted_at,
    torrent.published_at,
    torrent.state_changed_at,
    latest_decision.id AS decision_id,
    latest_decision.decision,
    latest_decision.reason_code,
    latest_decision.resulting_torrent_version,
    latest_decision.occurred_at AS decision_occurred_at
FROM torrents.torrents AS torrent
CROSS JOIN LATERAL (
    SELECT
        decision.id,
        decision.decision,
        decision.reason_code,
        decision.resulting_torrent_version,
        decision.occurred_at
    FROM review.torrent_decisions AS decision
    WHERE decision.torrent_id = torrent.id
    ORDER BY decision.occurred_at DESC, decision.id DESC
    LIMIT 1
) AS latest_decision
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
FOR UPDATE OF torrent;

-- name: GetEnabledTorrentResubmissionCategory :one
SELECT id
FROM catalog.categories
WHERE id = sqlc.arg(category_id)::text
  AND enabled = true
FOR KEY SHARE;

-- name: ResubmitRejectedTorrent :one
UPDATE torrents.torrents
SET
    category_id = sqlc.arg(category_id)::text,
    title = sqlc.arg(title)::text,
    subtitle = sqlc.arg(subtitle)::text,
    state = 'pending_review',
    version = version + 1,
    state_changed_at = sqlc.arg(occurred_at)::timestamptz,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE id = sqlc.arg(torrent_id)::bigint
  AND uploader_id = sqlc.arg(uploader_id)::uuid
  AND state = 'rejected'
  AND version = sqlc.arg(expected_version)::bigint
RETURNING id, state, version, state_changed_at;

-- name: InsertTorrentResubmission :exec
INSERT INTO review.torrent_resubmissions (
    id,
    torrent_id,
    responds_to_decision_id,
    expected_torrent_version,
    resulting_torrent_version,
    category_id,
    title,
    subtitle,
    correction_note,
    authorization_decision_id,
    occurred_at
) VALUES (
    sqlc.arg(resubmission_id)::uuid,
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(decision_id)::uuid,
    sqlc.arg(expected_torrent_version)::bigint,
    sqlc.arg(resulting_torrent_version)::bigint,
    sqlc.arg(category_id)::text,
    sqlc.arg(title)::text,
    sqlc.arg(subtitle)::text,
    sqlc.arg(correction_note)::text,
    sqlc.arg(authorization_decision_id)::uuid,
    sqlc.arg(occurred_at)::timestamptz
);
