-- name: GetSwarmSnapshotInbox :one
SELECT event_id, snapshot_id, payload_sha256
FROM catalog.swarm_snapshot_inbox
WHERE event_id = sqlc.arg(event_id)::uuid;

-- name: InsertSwarmSnapshotInbox :one
INSERT INTO catalog.swarm_snapshot_inbox (
    event_id,
    snapshot_id,
    payload_sha256,
    received_at
) VALUES (
    sqlc.arg(event_id)::uuid,
    sqlc.arg(snapshot_id)::uuid,
    sqlc.arg(payload_sha256)::bytea,
    sqlc.arg(received_at)::timestamptz
)
ON CONFLICT (event_id) DO NOTHING
RETURNING event_id;

-- name: GetSwarmProjectionStateForUpdate :one
SELECT source_id, routing_epoch, snapshot_sequence, snapshot_id, observed_at, applied_at
FROM catalog.swarm_snapshot_projection_state
WHERE singleton = true
FOR UPDATE;

-- name: InsertSwarmSnapshotRun :one
INSERT INTO catalog.swarm_snapshot_runs (
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

-- name: GetSwarmSnapshotRunForUpdate :one
SELECT
    snapshot_id,
    source_id,
    routing_epoch,
    snapshot_sequence,
    observed_at,
    chunk_count,
    received_chunk_count,
    status,
    applied_at
FROM catalog.swarm_snapshot_runs
WHERE snapshot_id = sqlc.arg(snapshot_id)::uuid
FOR UPDATE;

-- name: GetSwarmSnapshotRunBySequence :one
SELECT snapshot_id
FROM catalog.swarm_snapshot_runs
WHERE source_id = sqlc.arg(source_id)::text
  AND routing_epoch = sqlc.arg(routing_epoch)::bigint
  AND snapshot_sequence = sqlc.arg(snapshot_sequence)::bigint;

-- name: InsertSwarmSnapshotChunk :one
INSERT INTO catalog.swarm_snapshot_chunks (
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

-- name: GetSwarmSnapshotChunk :one
SELECT event_id, payload_sha256
FROM catalog.swarm_snapshot_chunks
WHERE snapshot_id = sqlc.arg(snapshot_id)::uuid
  AND chunk_index = sqlc.arg(chunk_index)::integer;

-- name: InsertSwarmSnapshotEntry :exec
INSERT INTO catalog.swarm_snapshot_entries (
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

-- name: IncrementSwarmSnapshotReceivedChunks :one
UPDATE catalog.swarm_snapshot_runs
SET received_chunk_count = received_chunk_count + 1
WHERE snapshot_id = sqlc.arg(snapshot_id)::uuid
  AND status = 'collecting'
  AND received_chunk_count < chunk_count
RETURNING received_chunk_count, chunk_count;

-- name: ApplyCompleteSwarmSnapshot :exec
INSERT INTO catalog.torrent_swarm_stats (
    torrent_id,
    seeders,
    leechers,
    completed,
    observed_at
)
SELECT
    public_torrent.id,
    coalesce(entry.seeders, 0)::integer,
    coalesce(entry.leechers, 0)::integer,
    coalesce(completion.completed, current_stats.completed, 0)::integer,
    sqlc.arg(observed_at)::timestamptz
FROM catalog.torrents AS public_torrent
LEFT JOIN torrents.torrents AS aggregate
  ON aggregate.id = public_torrent.id
 AND aggregate.state = 'published'
LEFT JOIN catalog.swarm_snapshot_entries AS entry
  ON entry.snapshot_id = sqlc.arg(snapshot_id)::uuid
 AND entry.info_hash_v1 = aggregate.info_hash_v1
LEFT JOIN catalog.torrent_completion_stats AS completion
  ON completion.torrent_id = public_torrent.id
LEFT JOIN catalog.torrent_swarm_stats AS current_stats
  ON current_stats.torrent_id = public_torrent.id
-- A catalog-only legacy/demo row has no authoritative info hash mapping. Leave
-- its existing fixture/import state untouched rather than falsely refreshing
-- it to zero; migration must first create the published aggregate mapping.
WHERE aggregate.id IS NOT NULL
ON CONFLICT (torrent_id) DO UPDATE SET
    seeders = EXCLUDED.seeders,
    leechers = EXCLUDED.leechers,
    completed = EXCLUDED.completed,
    observed_at = EXCLUDED.observed_at;

-- name: AdvanceSwarmProjectionState :execrows
UPDATE catalog.swarm_snapshot_projection_state
SET
    source_id = sqlc.arg(source_id)::text,
    routing_epoch = sqlc.arg(routing_epoch)::bigint,
    snapshot_sequence = sqlc.arg(snapshot_sequence)::bigint,
    snapshot_id = sqlc.arg(snapshot_id)::uuid,
    observed_at = sqlc.arg(observed_at)::timestamptz,
    applied_at = sqlc.arg(applied_at)::timestamptz
WHERE singleton = true;

-- name: MarkSwarmSnapshotApplied :execrows
UPDATE catalog.swarm_snapshot_runs
SET status = 'applied', applied_at = sqlc.arg(applied_at)::timestamptz
WHERE snapshot_id = sqlc.arg(snapshot_id)::uuid
  AND status = 'collecting'
  AND received_chunk_count = chunk_count;

-- name: SupersedeOlderSwarmSnapshotRuns :exec
UPDATE catalog.swarm_snapshot_runs
SET status = 'superseded'
WHERE status = 'collecting'
  AND (
    routing_epoch < sqlc.arg(routing_epoch)::bigint
    OR (
      routing_epoch = sqlc.arg(routing_epoch)::bigint
      AND source_id = sqlc.arg(source_id)::text
      AND snapshot_sequence < sqlc.arg(snapshot_sequence)::bigint
    )
  );

-- name: DeleteFinishedSwarmSnapshotEntries :exec
DELETE FROM catalog.swarm_snapshot_entries AS entry
USING catalog.swarm_snapshot_runs AS run
WHERE entry.snapshot_id = run.snapshot_id
  AND run.status IN ('applied', 'superseded');

-- name: DeleteFinishedSwarmSnapshotChunks :exec
DELETE FROM catalog.swarm_snapshot_chunks AS chunk
USING catalog.swarm_snapshot_runs AS run
WHERE chunk.snapshot_id = run.snapshot_id
  AND run.status IN ('applied', 'superseded');

-- name: GetSwarmCompletionByIdentity :one
SELECT event_id, torrent_id
FROM catalog.swarm_completion_inbox
WHERE completion_id = sqlc.arg(completion_id)::bytea;

-- name: GetSwarmCompletionByEvent :one
SELECT completion_id, payload_sha256, torrent_id
FROM catalog.swarm_completion_inbox
WHERE event_id = sqlc.arg(event_id)::uuid;

-- name: ResolvePublishedTorrentForCompletion :one
SELECT public_torrent.id
FROM torrents.torrents AS aggregate
JOIN catalog.torrents AS public_torrent
  ON public_torrent.id = aggregate.id
WHERE aggregate.id = sqlc.arg(torrent_id)::bigint
  AND aggregate.info_hash_v1 = sqlc.arg(info_hash_v1)::bytea
  AND aggregate.state = 'published';

-- name: InsertSwarmCompletionInbox :one
INSERT INTO catalog.swarm_completion_inbox (
    event_id,
    completion_id,
    payload_sha256,
    torrent_id,
    occurred_at,
    applied_at
) VALUES (
    sqlc.arg(event_id)::uuid,
    sqlc.arg(completion_id)::bytea,
    sqlc.arg(payload_sha256)::bytea,
    sqlc.arg(torrent_id)::bigint,
    sqlc.arg(occurred_at)::timestamptz,
    sqlc.arg(applied_at)::timestamptz
)
ON CONFLICT (event_id) DO NOTHING
RETURNING event_id;

-- name: IncrementTorrentCompletionStats :one
INSERT INTO catalog.torrent_completion_stats (
    torrent_id,
    completed,
    observed_at
) SELECT
    public_torrent.id,
    coalesce(current_stats.completed, 0) + 1,
    sqlc.arg(observed_at)::timestamptz
FROM catalog.torrents AS public_torrent
LEFT JOIN catalog.torrent_swarm_stats AS current_stats
  ON current_stats.torrent_id = public_torrent.id
WHERE public_torrent.id = sqlc.arg(torrent_id)::bigint
ON CONFLICT (torrent_id) DO UPDATE SET
    completed = catalog.torrent_completion_stats.completed + 1,
    observed_at = GREATEST(catalog.torrent_completion_stats.observed_at, EXCLUDED.observed_at)
RETURNING completed;

-- name: SyncTorrentSwarmCompleted :exec
UPDATE catalog.torrent_swarm_stats
SET completed = sqlc.arg(completed)::integer
WHERE torrent_id = sqlc.arg(torrent_id)::bigint;

-- name: AdvanceTrackerCompletionSequence :one
UPDATE tracker_control.projection_state
SET completion_sequence = completion_sequence + 1
WHERE singleton = true
RETURNING completion_sequence;
