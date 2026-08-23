-- name: ListPendingTorrentReviews :many
SELECT
    torrent.id,
    torrent.uploader_id,
    uploader.display_name AS uploader_display_name,
    torrent.category_id,
    category.name AS category_name,
    torrent.title,
    torrent.subtitle,
    torrent.content_name,
    torrent.info_hash_v1,
    torrent.total_size_bytes,
    torrent.file_count,
    torrent.version,
    torrent.submitted_at,
    torrent.state_changed_at AS review_requested_at,
    count(*) OVER ()::bigint AS total_count
FROM torrents.torrents AS torrent
JOIN identity.users AS uploader ON uploader.id = torrent.uploader_id
JOIN catalog.categories AS category ON category.id = torrent.category_id
WHERE torrent.state = 'pending_review'
ORDER BY torrent.state_changed_at, torrent.id
LIMIT sqlc.arg(result_limit)::integer;

-- name: ListTorrentReviewAssignments :many
SELECT
    torrent.id,
    torrent.uploader_id,
    uploader.display_name AS uploader_display_name,
    torrent.category_id,
    category.name AS category_name,
    torrent.title,
    torrent.subtitle,
    torrent.content_name,
    torrent.info_hash_v1,
    torrent.total_size_bytes,
    torrent.file_count,
    torrent.version,
    torrent.submitted_at,
    torrent.state_changed_at AS review_requested_at,
    COALESCE(round.approve_count + round.reject_count, 0)::integer AS votes_cast,
    COALESCE(round.required_votes, 3)::integer AS required_votes,
    COALESCE(round.maximum_votes, 4)::integer AS maximum_votes,
    count(*) OVER ()::bigint AS total_count
FROM torrents.torrents AS torrent
JOIN identity.users AS uploader ON uploader.id = torrent.uploader_id
JOIN catalog.categories AS category ON category.id = torrent.category_id
LEFT JOIN review.torrent_review_rounds AS round
  ON round.torrent_id = torrent.id
 AND round.expected_torrent_version = torrent.version
WHERE torrent.state = 'pending_review'
  AND torrent.uploader_id <> sqlc.arg(reviewer_id)::uuid
  AND (round.id IS NULL OR round.status = 'open')
  AND NOT EXISTS (
      SELECT 1
      FROM review.torrent_review_votes AS vote
      WHERE vote.round_id = round.id
        AND vote.voter_id = sqlc.arg(reviewer_id)::uuid
  )
ORDER BY torrent.state_changed_at, torrent.id
LIMIT sqlc.arg(result_limit)::integer;

-- name: GetTorrentReviewAssignment :one
SELECT
    torrent.id,
    torrent.uploader_id,
    uploader.display_name AS uploader_display_name,
    torrent.category_id,
    category.name AS category_name,
    torrent.title,
    torrent.subtitle,
    torrent.content_name,
    torrent.info_hash_v1,
    torrent.total_size_bytes,
    torrent.file_count,
    torrent.version,
    torrent.submitted_at,
    torrent.state_changed_at AS review_requested_at,
    COALESCE(round.approve_count + round.reject_count, 0)::integer AS votes_cast,
    COALESCE(round.required_votes, 3)::integer AS required_votes,
    COALESCE(round.maximum_votes, 4)::integer AS maximum_votes
FROM torrents.torrents AS torrent
JOIN identity.users AS uploader ON uploader.id = torrent.uploader_id
JOIN catalog.categories AS category ON category.id = torrent.category_id
LEFT JOIN review.torrent_review_rounds AS round
  ON round.torrent_id = torrent.id
 AND round.expected_torrent_version = torrent.version
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
  AND torrent.state = 'pending_review'
  AND torrent.uploader_id <> sqlc.arg(reviewer_id)::uuid
  AND (round.id IS NULL OR round.status = 'open')
  AND NOT EXISTS (
      SELECT 1
      FROM review.torrent_review_votes AS vote
      WHERE vote.round_id = round.id
        AND vote.voter_id = sqlc.arg(reviewer_id)::uuid
  );

-- name: ListReviewedTorrentReviews :many
SELECT
    torrent.id,
    torrent.uploader_id,
    uploader.display_name AS uploader_display_name,
    torrent.category_id,
    category.name AS category_name,
    torrent.title,
    torrent.subtitle,
    torrent.content_name,
    torrent.info_hash_v1,
    torrent.total_size_bytes,
    torrent.file_count,
    vote.expected_torrent_version AS version,
    torrent.submitted_at,
    round.opened_at AS review_requested_at,
    vote.id AS vote_id,
    vote.round_id,
    vote.decision,
    vote.reason_code,
    vote.reason,
    vote.occurred_at AS voted_at,
    round.approve_count,
    round.reject_count,
    (CASE
        WHEN round.status = 'escalated' THEN 'escalated'
        WHEN round.status = 'resolved' AND final_decision.resulting_state = 'published' THEN 'published'
        WHEN round.status = 'resolved' AND final_decision.resulting_state = 'rejected' THEN 'rejected'
        ELSE 'waiting'
    END)::text AS outcome,
    count(*) OVER ()::bigint AS total_count
FROM review.torrent_review_votes AS vote
JOIN review.torrent_review_rounds AS round ON round.id = vote.round_id
JOIN torrents.torrents AS torrent ON torrent.id = vote.torrent_id
JOIN identity.users AS uploader ON uploader.id = torrent.uploader_id
JOIN catalog.categories AS category ON category.id = torrent.category_id
LEFT JOIN review.torrent_decisions AS final_decision ON final_decision.id = round.final_decision_id
WHERE vote.voter_id = sqlc.arg(reviewer_id)::uuid
ORDER BY vote.occurred_at DESC, vote.id DESC
LIMIT sqlc.arg(result_limit)::integer;

-- name: GetTorrentReviewDecision :one
SELECT
    decision.id,
    decision.torrent_id,
    decision.reviewer_id,
    decision.decision,
    decision.reason_code,
    decision.reason,
    decision.expected_torrent_version,
    decision.resulting_torrent_version,
    decision.resulting_state,
    decision.resolution_source,
    decision.review_round_id,
    decision.membership_transition_id,
    decision.occurred_at
FROM review.torrent_decisions AS decision
WHERE decision.id = sqlc.arg(decision_id)::uuid;

-- name: GetPendingTorrentReviewForUpdate :one
SELECT
    torrent.id,
    torrent.uploader_id,
    uploader.display_name AS uploader_display_name,
    torrent.category_id,
    category.name AS category_name,
    category.enabled AS category_enabled,
    torrent.object_id,
    torrent.title,
    torrent.subtitle,
    torrent.content_name,
    torrent.info_hash_v1,
    torrent.total_size_bytes,
    torrent.file_count,
    torrent.state,
    torrent.version,
    torrent.submitted_at,
    torrent.state_changed_at,
    EXISTS (
        SELECT 1
        FROM torrents.torrent_object_locations AS location
        WHERE location.object_id = torrent.object_id
          AND location.state = 'verified'
          AND location.verified_at IS NOT NULL
    ) AS has_verified_location
FROM torrents.torrents AS torrent
JOIN identity.users AS uploader ON uploader.id = torrent.uploader_id
JOIN catalog.categories AS category ON category.id = torrent.category_id
WHERE torrent.id = sqlc.arg(torrent_id)::bigint
FOR UPDATE OF torrent;

-- name: PublishReviewedTorrent :one
UPDATE torrents.torrents
SET
    state = 'published',
    version = version + 1,
    published_at = sqlc.arg(occurred_at)::timestamptz,
    state_changed_at = sqlc.arg(occurred_at)::timestamptz,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE id = sqlc.arg(torrent_id)::bigint
  AND state = 'pending_review'
  AND version = sqlc.arg(expected_version)::bigint
RETURNING id, state, version, published_at, state_changed_at;

-- name: RejectReviewedTorrent :one
UPDATE torrents.torrents
SET
    state = 'rejected',
    version = version + 1,
    state_changed_at = sqlc.arg(occurred_at)::timestamptz,
    updated_at = sqlc.arg(occurred_at)::timestamptz
WHERE id = sqlc.arg(torrent_id)::bigint
  AND state = 'pending_review'
  AND version = sqlc.arg(expected_version)::bigint
RETURNING id, state, version, published_at, state_changed_at;

-- name: InsertTorrentReviewDecision :exec
INSERT INTO review.torrent_decisions (
    id,
    torrent_id,
    reviewer_id,
    decision,
    reason_code,
    reason,
    expected_torrent_version,
    resulting_torrent_version,
    resulting_state,
    authorization_decision_id,
    resolution_source,
    review_round_id,
    membership_transition_id,
    occurred_at
) VALUES (
    sqlc.arg(decision_id)::uuid,
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(reviewer_id)::uuid,
    sqlc.arg(review_decision)::text,
    sqlc.arg(reason_code)::text,
    sqlc.arg(reason)::text,
    sqlc.arg(expected_torrent_version)::bigint,
    sqlc.arg(resulting_torrent_version)::bigint,
    sqlc.arg(resulting_state)::text,
    sqlc.arg(authorization_decision_id)::uuid,
    sqlc.arg(resolution_source)::text,
    sqlc.narg(review_round_id)::uuid,
    sqlc.narg(membership_transition_id)::uuid,
    sqlc.arg(occurred_at)::timestamptz
);

-- name: InsertPublishedTorrentCatalogProjection :exec
INSERT INTO catalog.torrents (
    id,
    category_id,
    name,
    subtitle,
    size_bytes,
    promotion,
    published_at,
    created_at
) VALUES (
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(category_id)::text,
    sqlc.arg(title)::text,
    sqlc.arg(subtitle)::text,
    sqlc.arg(size_bytes)::bigint,
    'none',
    sqlc.arg(published_at)::timestamptz,
    sqlc.arg(published_at)::timestamptz
);
