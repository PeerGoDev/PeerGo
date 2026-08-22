-- name: LockSeedingSnapshotTimeline :exec
SELECT pg_advisory_xact_lock(hashtextextended('peergo-settlement-seeding-snapshot-v1', 0));

-- name: InsertSeedingSnapshotInbox :one
INSERT INTO settlement.seeding_swarm_snapshot_inbox (
    event_id,
    snapshot_id,
    payload_sha256,
    source_stream,
    source_subject,
    source_sequence,
    delivery_count,
    observed_at,
    received_at
) VALUES (
    sqlc.arg(event_id)::uuid,
    sqlc.arg(snapshot_id)::uuid,
    sqlc.arg(payload_sha256)::bytea,
    sqlc.arg(source_stream)::text,
    sqlc.arg(source_subject)::text,
    sqlc.arg(source_sequence)::bigint,
    sqlc.arg(delivery_count)::bigint,
    sqlc.arg(observed_at)::timestamptz,
    sqlc.arg(received_at)::timestamptz
)
ON CONFLICT (event_id) DO NOTHING
RETURNING event_id;

-- name: GetSeedingSnapshotInbox :one
SELECT
    snapshot_id,
    payload_sha256,
    source_stream,
    source_subject,
    source_sequence,
    observed_at
FROM settlement.seeding_swarm_snapshot_inbox
WHERE event_id = sqlc.arg(event_id)::uuid;

-- name: GetLatestSeedingSnapshotForRoute :one
SELECT snapshot_id, snapshot_sequence, observed_at
FROM ledger.seeding_swarm_snapshots
WHERE source_id = sqlc.arg(source_id)::text
  AND routing_epoch = sqlc.arg(routing_epoch)::bigint
ORDER BY snapshot_sequence DESC
LIMIT 1;

-- name: InsertSeedingSnapshotRun :one
INSERT INTO ledger.seeding_swarm_snapshots (
    snapshot_id,
    source_id,
    routing_epoch,
    snapshot_sequence,
    observed_at,
    chunk_count,
    created_at
) VALUES (
    sqlc.arg(snapshot_id)::uuid,
    sqlc.arg(source_id)::text,
    sqlc.arg(routing_epoch)::bigint,
    sqlc.arg(snapshot_sequence)::bigint,
    sqlc.arg(observed_at)::timestamptz,
    sqlc.arg(chunk_count)::integer,
    sqlc.arg(created_at)::timestamptz
)
ON CONFLICT (snapshot_id) DO NOTHING
RETURNING snapshot_id;

-- name: GetSeedingSnapshotRunForUpdate :one
SELECT
    snapshot_id,
    source_id,
    routing_epoch,
    snapshot_sequence,
    observed_at,
    chunk_count,
    received_chunk_count,
    status,
    completed_at
FROM ledger.seeding_swarm_snapshots
WHERE snapshot_id = sqlc.arg(snapshot_id)::uuid
FOR UPDATE;

-- name: GetSeedingSnapshotRunBySequence :one
SELECT snapshot_id
FROM ledger.seeding_swarm_snapshots
WHERE source_id = sqlc.arg(source_id)::text
  AND routing_epoch = sqlc.arg(routing_epoch)::bigint
  AND snapshot_sequence = sqlc.arg(snapshot_sequence)::bigint;

-- name: InsertSeedingSnapshotChunk :one
INSERT INTO ledger.seeding_swarm_snapshot_chunks (
    snapshot_id,
    chunk_index,
    event_id,
    payload_sha256
) VALUES (
    sqlc.arg(snapshot_id)::uuid,
    sqlc.arg(chunk_index)::integer,
    sqlc.arg(event_id)::uuid,
    sqlc.arg(payload_sha256)::bytea
)
ON CONFLICT (snapshot_id, chunk_index) DO NOTHING
RETURNING event_id;

-- name: GetSeedingSnapshotChunk :one
SELECT event_id, payload_sha256
FROM ledger.seeding_swarm_snapshot_chunks
WHERE snapshot_id = sqlc.arg(snapshot_id)::uuid
  AND chunk_index = sqlc.arg(chunk_index)::integer;

-- name: InsertSeedingSnapshotEntry :exec
INSERT INTO ledger.seeding_swarm_snapshot_entries (
    snapshot_id,
    info_hash_v1,
    seeders,
    leechers
) VALUES (
    sqlc.arg(snapshot_id)::uuid,
    sqlc.arg(info_hash_v1)::bytea,
    sqlc.arg(seeders)::integer,
    sqlc.arg(leechers)::integer
);

-- name: IncrementSeedingSnapshotReceivedChunks :one
UPDATE ledger.seeding_swarm_snapshots
SET received_chunk_count = received_chunk_count + 1
WHERE snapshot_id = sqlc.arg(snapshot_id)::uuid
  AND status = 'collecting'
  AND received_chunk_count < chunk_count
RETURNING received_chunk_count, chunk_count;

-- name: CompleteSeedingSnapshot :execrows
UPDATE ledger.seeding_swarm_snapshots
SET
    status = 'complete',
    completed_at = sqlc.arg(completed_at)::timestamptz
WHERE snapshot_id = sqlc.arg(snapshot_id)::uuid
  AND status = 'collecting'
  AND received_chunk_count = chunk_count;

-- name: LockSeedingEvidenceWindow :exec
SELECT pg_advisory_xact_lock(hashtextextended(
    'peergo-settlement-seeding-window-v1:' || extract(epoch FROM sqlc.arg(window_start)::timestamptz)::bigint::text,
    0
));

-- name: GetSeedingEvidenceWindow :one
SELECT
    window_start,
    window_end,
    schema_version,
    closure_delay_seconds,
    max_interval_credit_seconds,
    announce_fence_sequence,
    selected_snapshot_id,
    selected_snapshot_sequence,
    selected_snapshot_observed_at,
    snapshot_fence_id,
    snapshot_fence_sequence,
    snapshot_fence_observed_at,
    item_count,
    evidence_sha256,
    built_at
FROM ledger.seeding_evidence_windows
WHERE window_start = sqlc.arg(window_start)::timestamptz;

-- name: CountSeedingEvidenceAnomalies :one
SELECT count(*)
FROM ledger.seeding_evidence_anomalies
WHERE window_start = sqlc.arg(window_start)::timestamptz;

-- name: GetNextSeedingEvidenceWindowStart :one
SELECT coalesce(max(window_end), sqlc.arg(initial_window_start)::timestamptz)::timestamptz
FROM ledger.seeding_evidence_windows;

-- name: GetSeedingAnnounceFence :one
WITH terminal_head AS (
    SELECT source_sequence, received_at
    FROM settlement.event_inbox
    WHERE source_stream = sqlc.arg(source_stream)::text
      AND outcome <> 'processing'
    ORDER BY source_sequence DESC
    LIMIT 1
)
SELECT source_sequence, received_at
FROM terminal_head
WHERE received_at >= sqlc.arg(fence_not_before)::timestamptz;

-- name: GetSeedingSnapshotFence :one
SELECT
    snapshot.snapshot_id,
    snapshot.source_id,
    snapshot.routing_epoch,
    snapshot.snapshot_sequence,
    snapshot.observed_at
FROM ledger.seeding_swarm_snapshots AS snapshot
WHERE snapshot.status = 'complete'
  AND snapshot.observed_at >= sqlc.arg(window_end)::timestamptz
  AND EXISTS (
      SELECT 1
      FROM settlement.seeding_swarm_snapshot_inbox AS inbox
      WHERE inbox.snapshot_id = snapshot.snapshot_id
        AND inbox.source_stream = sqlc.arg(source_stream)::text
  )
ORDER BY snapshot.observed_at, snapshot.routing_epoch DESC, snapshot.snapshot_sequence
LIMIT 1;

-- name: GetSelectedSeedingSnapshot :one
SELECT snapshot_id, snapshot_sequence, observed_at
FROM ledger.seeding_swarm_snapshots
WHERE status = 'complete'
  AND source_id = sqlc.arg(source_id)::text
  AND routing_epoch = sqlc.arg(routing_epoch)::bigint
  AND snapshot_sequence <= sqlc.arg(max_snapshot_sequence)::bigint
  AND observed_at <= sqlc.arg(window_end)::timestamptz
ORDER BY snapshot_sequence DESC
LIMIT 1;

-- name: ListSeedingIntervalsForWindow :many
SELECT
    raw.event_id,
    raw.user_id,
    raw.torrent_id,
    raw.info_hash_v1,
    greatest(raw.starts_at, sqlc.arg(window_start)::timestamptz)::timestamptz AS clipped_starts_at,
    least(raw.ends_at, sqlc.arg(window_end)::timestamptz)::timestamptz AS clipped_ends_at,
    raw.raw_uploaded,
    inbox.source_sequence
FROM ledger.raw_session_intervals AS raw
INNER JOIN settlement.event_inbox AS inbox ON inbox.event_id = raw.event_id
WHERE inbox.source_stream = sqlc.arg(source_stream)::text
  AND inbox.source_sequence <= sqlc.arg(announce_fence_sequence)::bigint
  AND raw.starts_at < sqlc.arg(window_end)::timestamptz
  AND raw.ends_at > sqlc.arg(window_start)::timestamptz
  AND raw.previous_left = 0
  AND raw.current_left = 0
  -- An interval proves activity only while adjacent Tracker updates remain
  -- within the configured credible gap. Do not cap a stale multi-hour gap:
  -- excluding it entirely matches UNIT3D's conservative seed-time rule.
  AND raw.ends_at <= raw.starts_at
      + (sqlc.arg(max_interval_credit_seconds)::bigint * interval '1 second')
ORDER BY raw.user_id, raw.torrent_id, clipped_starts_at, clipped_ends_at, raw.event_id;

-- name: ListSeedingSnapshotEntries :many
SELECT info_hash_v1, seeders, leechers
FROM ledger.seeding_swarm_snapshot_entries
WHERE snapshot_id = sqlc.arg(snapshot_id)::uuid
ORDER BY info_hash_v1;

-- name: InsertSeedingEvidenceWindow :exec
INSERT INTO ledger.seeding_evidence_windows (
    window_start,
    window_end,
    schema_version,
    closure_delay_seconds,
    max_interval_credit_seconds,
    announce_source_stream,
    announce_fence_sequence,
    announce_fence_received_at,
    selected_snapshot_id,
    selected_snapshot_sequence,
    selected_snapshot_observed_at,
    snapshot_fence_id,
    snapshot_fence_sequence,
    snapshot_fence_observed_at,
    item_count,
    evidence_sha256,
    built_at
) VALUES (
    sqlc.arg(window_start)::timestamptz,
    sqlc.arg(window_end)::timestamptz,
    'seeding.evidence.v2',
    sqlc.arg(closure_delay_seconds)::integer,
    sqlc.arg(max_interval_credit_seconds)::integer,
    sqlc.arg(announce_source_stream)::text,
    sqlc.arg(announce_fence_sequence)::bigint,
    sqlc.arg(announce_fence_received_at)::timestamptz,
    sqlc.arg(selected_snapshot_id)::uuid,
    sqlc.arg(selected_snapshot_sequence)::bigint,
    sqlc.arg(selected_snapshot_observed_at)::timestamptz,
    sqlc.arg(snapshot_fence_id)::uuid,
    sqlc.arg(snapshot_fence_sequence)::bigint,
    sqlc.arg(snapshot_fence_observed_at)::timestamptz,
    sqlc.arg(item_count)::integer,
    sqlc.arg(evidence_sha256)::bytea,
    sqlc.arg(built_at)::timestamptz
);

-- name: InsertSeedingEvidenceItem :exec
INSERT INTO ledger.seeding_evidence_items (
    window_start,
    user_id,
    torrent_id,
    info_hash_v1,
    active_seconds,
    raw_uploaded,
    source_interval_count,
    first_active_at,
    last_active_at,
    snapshot_seeders,
    snapshot_leechers,
    evidence_sha256
) VALUES (
    sqlc.arg(window_start)::timestamptz,
    sqlc.arg(user_id)::uuid,
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(info_hash_v1)::bytea,
    sqlc.arg(active_seconds)::bigint,
    sqlc.arg(raw_uploaded)::bigint,
    sqlc.arg(source_interval_count)::integer,
    sqlc.arg(first_active_at)::timestamptz,
    sqlc.arg(last_active_at)::timestamptz,
    sqlc.arg(snapshot_seeders)::integer,
    sqlc.arg(snapshot_leechers)::integer,
    sqlc.arg(evidence_sha256)::bytea
);

-- name: InsertSeedingEvidenceSource :exec
INSERT INTO ledger.seeding_evidence_sources (
    window_start,
    user_id,
    torrent_id,
    interval_event_id,
    source_sequence,
    clipped_starts_at,
    clipped_ends_at
) VALUES (
    sqlc.arg(window_start)::timestamptz,
    sqlc.arg(user_id)::uuid,
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(interval_event_id)::uuid,
    sqlc.arg(source_sequence)::bigint,
    sqlc.arg(clipped_starts_at)::timestamptz,
    sqlc.arg(clipped_ends_at)::timestamptz
);

-- name: InsertSeedingEvidenceOutboxEvent :exec
INSERT INTO settlement.seeding_evidence_outbox (
    event_id,
    window_start,
    chunk_index,
    event_type,
    schema_version,
    occurred_at,
    payload_json,
    payload_sha256,
    available_at,
    created_at
) VALUES (
    sqlc.arg(event_id)::uuid,
    sqlc.arg(window_start)::timestamptz,
    sqlc.arg(chunk_index)::integer,
    'settlement.seeding.evidence.closed',
    'settlement.seeding.evidence.v1',
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(payload_json)::text,
    sqlc.arg(payload_sha256)::bytea,
    sqlc.arg(available_at)::timestamptz,
    sqlc.arg(created_at)::timestamptz
);

-- name: CountSeedingEvidenceOutboxEvents :one
SELECT count(*)
FROM settlement.seeding_evidence_outbox
WHERE window_start = sqlc.arg(window_start)::timestamptz;

-- name: ListSeedingEvidenceItemsForTransport :many
SELECT
    user_id,
    torrent_id,
    active_seconds,
    raw_uploaded,
    snapshot_seeders,
    snapshot_leechers,
    evidence_sha256
FROM ledger.seeding_evidence_items
WHERE window_start = sqlc.arg(window_start)::timestamptz
ORDER BY user_id, torrent_id;

-- name: ClaimNextSeedingEvidenceOutboxEvent :one
WITH candidate AS (
    SELECT outbox.event_id
    FROM settlement.seeding_evidence_outbox AS outbox
    WHERE outbox.published_at IS NULL
      AND outbox.available_at <= sqlc.arg(claimed_at)::timestamptz
      AND (outbox.lease_until IS NULL OR outbox.lease_until <= sqlc.arg(claimed_at)::timestamptz)
    ORDER BY outbox.available_at, outbox.event_id
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE settlement.seeding_evidence_outbox AS outbox
SET
    lease_token = sqlc.arg(lease_token)::uuid,
    lease_until = sqlc.arg(lease_until)::timestamptz,
    attempts = outbox.attempts + 1,
    last_error_code = NULL
FROM candidate
WHERE outbox.event_id = candidate.event_id
RETURNING
    outbox.event_id,
    outbox.window_start,
    outbox.chunk_index,
    outbox.event_type,
    outbox.schema_version,
    outbox.occurred_at,
    outbox.payload_json,
    outbox.payload_sha256,
    outbox.lease_token,
    outbox.attempts;

-- name: MarkSeedingEvidenceOutboxEventPublished :execrows
UPDATE settlement.seeding_evidence_outbox
SET
    published_at = sqlc.arg(published_at)::timestamptz,
    lease_token = NULL,
    lease_until = NULL,
    last_error_code = NULL
WHERE event_id = sqlc.arg(event_id)::uuid
  AND lease_token = sqlc.arg(lease_token)::uuid
  AND published_at IS NULL;

-- name: ReleaseSeedingEvidenceOutboxEvent :execrows
UPDATE settlement.seeding_evidence_outbox
SET
    available_at = sqlc.arg(available_at)::timestamptz,
    lease_token = NULL,
    lease_until = NULL,
    last_error_code = sqlc.arg(last_error_code)::text
WHERE event_id = sqlc.arg(event_id)::uuid
  AND lease_token = sqlc.arg(lease_token)::uuid
  AND published_at IS NULL;
