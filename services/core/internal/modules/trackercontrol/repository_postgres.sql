-- name: AppendTrackerControlEvent :exec
INSERT INTO tracker_control.outbox (
    event_id,
    event_type,
    schema_version,
    aggregate_id,
    aggregate_version,
    occurred_at,
    payload_json,
    payload_sha256,
    available_at
) VALUES (
    sqlc.arg(event_id)::uuid,
    sqlc.arg(event_type)::text,
    sqlc.arg(schema_version)::text,
    sqlc.arg(aggregate_id)::bigint,
    sqlc.arg(aggregate_version)::bigint,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(payload_json)::text,
    sqlc.arg(payload_sha256)::bytea,
    sqlc.arg(available_at)::timestamptz
);

-- name: ClaimNextTrackerControlEvent :one
WITH candidate AS (
    SELECT pending.sequence
    FROM tracker_control.outbox AS pending
    WHERE pending.projected_at IS NULL
    ORDER BY pending.sequence
    LIMIT 1
    FOR UPDATE
)
UPDATE tracker_control.outbox AS event
SET
    lease_token = sqlc.arg(lease_token)::uuid,
    lease_until = sqlc.arg(lease_until)::timestamptz,
    attempts = event.attempts + 1,
    last_error_code = NULL
FROM candidate
WHERE event.sequence = candidate.sequence
  AND event.available_at <= sqlc.arg(claimed_at)::timestamptz
  AND (event.lease_until IS NULL OR event.lease_until <= sqlc.arg(claimed_at)::timestamptz)
RETURNING
    event.sequence,
    event.event_id,
    event.event_type,
    event.schema_version,
    event.aggregate_id,
    event.aggregate_version,
    event.occurred_at,
    event.payload_json,
    event.payload_sha256,
    event.lease_token,
    event.attempts;

-- name: GetClaimedTrackerControlEventForUpdate :one
SELECT
    event.sequence,
    event.event_id,
    event.event_type,
    event.schema_version,
    event.aggregate_id,
    event.aggregate_version,
    event.occurred_at,
    event.payload_json,
    event.payload_sha256,
    event.attempts
FROM tracker_control.outbox AS event
WHERE event.sequence = sqlc.arg(control_sequence)::bigint
  AND event.lease_token = sqlc.arg(lease_token)::uuid
  AND event.projected_at IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM tracker_control.outbox AS earlier
      WHERE earlier.projected_at IS NULL
        AND earlier.sequence < event.sequence
  )
FOR UPDATE;

-- name: GetTrackerProjectionStateForUpdate :one
SELECT last_sequence, updated_at
FROM tracker_control.projection_state
WHERE singleton = true
FOR UPDATE;

-- name: UpsertTorrentAllowlistProjection :execrows
INSERT INTO tracker_control.torrent_allowlist_projection (
    torrent_id,
    info_hash_v1,
    total_size_bytes,
    enabled,
    torrent_version,
    control_sequence,
    updated_at
) VALUES (
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(info_hash_v1)::bytea,
    sqlc.arg(total_size_bytes)::bigint,
    sqlc.arg(enabled)::boolean,
    sqlc.arg(torrent_version)::bigint,
    sqlc.arg(control_sequence)::bigint,
    sqlc.arg(updated_at)::timestamptz
)
ON CONFLICT (torrent_id) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    torrent_version = EXCLUDED.torrent_version,
    control_sequence = EXCLUDED.control_sequence,
    updated_at = EXCLUDED.updated_at
WHERE tracker_control.torrent_allowlist_projection.info_hash_v1 = EXCLUDED.info_hash_v1
  AND tracker_control.torrent_allowlist_projection.total_size_bytes = EXCLUDED.total_size_bytes
  AND tracker_control.torrent_allowlist_projection.torrent_version < EXCLUDED.torrent_version;

-- name: AdvanceTrackerProjectionState :execrows
UPDATE tracker_control.projection_state
SET
    last_sequence = sqlc.arg(control_sequence)::bigint,
    updated_at = sqlc.arg(projected_at)::timestamptz
WHERE singleton = true
  AND last_sequence < sqlc.arg(control_sequence)::bigint;

-- name: MarkTrackerControlEventProjected :execrows
UPDATE tracker_control.outbox
SET
    projected_at = sqlc.arg(projected_at)::timestamptz,
    lease_token = NULL,
    lease_until = NULL,
    last_error_code = NULL
WHERE sequence = sqlc.arg(control_sequence)::bigint
  AND lease_token = sqlc.arg(lease_token)::uuid
  AND projected_at IS NULL;

-- name: ReleaseTrackerControlEvent :execrows
UPDATE tracker_control.outbox
SET
    available_at = sqlc.arg(available_at)::timestamptz,
    lease_token = NULL,
    lease_until = NULL,
    last_error_code = sqlc.arg(last_error_code)::text
WHERE sequence = sqlc.arg(control_sequence)::bigint
  AND lease_token = sqlc.arg(lease_token)::uuid
  AND projected_at IS NULL;

-- name: GetTrackerProjectionStatus :one
SELECT
    state.last_sequence,
    state.updated_at,
    count(event.sequence) FILTER (WHERE event.projected_at IS NULL)::bigint AS pending_events
FROM tracker_control.projection_state AS state
LEFT JOIN tracker_control.outbox AS event ON true
WHERE state.singleton = true
GROUP BY state.last_sequence, state.updated_at;

-- name: GetTrackerSnapshotProjectionState :one
SELECT
    state.last_sequence,
    state.updated_at,
    count(event.sequence) FILTER (WHERE event.projected_at IS NULL)::bigint AS pending_events
FROM tracker_control.projection_state AS state
LEFT JOIN tracker_control.outbox AS event ON true
WHERE state.singleton = true
GROUP BY state.last_sequence, state.updated_at;

-- name: ListEnabledTorrentAllowlist :many
SELECT
	allowlist.torrent_id,
	allowlist.info_hash_v1,
	allowlist.total_size_bytes,
	coalesce(completion.completed, swarm.completed, 0)::bigint AS completed_downloads,
	allowlist.torrent_version,
	allowlist.control_sequence,
	allowlist.updated_at
FROM tracker_control.torrent_allowlist_projection AS allowlist
LEFT JOIN catalog.torrent_completion_stats AS completion
  ON completion.torrent_id = allowlist.torrent_id
LEFT JOIN catalog.torrent_swarm_stats AS swarm
  ON swarm.torrent_id = allowlist.torrent_id
WHERE allowlist.enabled = true
ORDER BY allowlist.info_hash_v1;

-- name: ReserveTrackerSubjectSnapshotSequence :one
UPDATE tracker_control.subject_snapshot_state
SET
    last_sequence = last_sequence + 1,
    updated_at = sqlc.arg(as_of)::timestamptz
WHERE singleton = true
RETURNING last_sequence, updated_at;

-- name: ListTrackerSubjectSnapshotEntries :many
SELECT
    passkey.user_id,
    users.numeric_id AS numeric_user_id,
    passkey.lookup_hmac,
    passkey.vault_version,
    identity.is_download_restricted(users.id) AS download_restricted
FROM identity.tracker_passkey_hmac AS passkey
INNER JOIN identity.users AS users ON users.id = passkey.user_id
WHERE users.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= sqlc.arg(as_of)::timestamptz
        AND restriction.expires_at > sqlc.arg(as_of)::timestamptz
  )
ORDER BY passkey.lookup_hmac;
