-- name: StorageCleanupDeleteTrafficOutbox :execrows
WITH candidate AS (
    SELECT outbox.event_id
    FROM settlement.traffic_outbox AS outbox
    WHERE outbox.published_at < sqlc.arg(terminal_before)::timestamptz
    ORDER BY outbox.published_at, outbox.event_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM settlement.traffic_outbox AS outbox
USING candidate
WHERE outbox.event_id = candidate.event_id;

-- name: StorageCleanupDeleteHNROutbox :execrows
WITH candidate AS (
    SELECT outbox.event_id
    FROM settlement.hnr_outbox AS outbox
    WHERE outbox.published_at < sqlc.arg(terminal_before)::timestamptz
    ORDER BY outbox.published_at, outbox.event_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM settlement.hnr_outbox AS outbox
USING candidate
WHERE outbox.event_id = candidate.event_id;

-- name: StorageCleanupDeleteSeedingEvidenceOutbox :execrows
WITH candidate AS (
    SELECT outbox.event_id
    FROM settlement.seeding_evidence_outbox AS outbox
    WHERE outbox.published_at < sqlc.arg(terminal_before)::timestamptz
    ORDER BY outbox.published_at, outbox.event_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM settlement.seeding_evidence_outbox AS outbox
USING candidate
WHERE outbox.event_id = candidate.event_id;

-- name: StorageCleanupDeletePolicyWork :execrows
WITH candidate AS (
    SELECT work.interval_event_id
    FROM settlement.policy_work AS work
    WHERE work.settled_at < sqlc.arg(terminal_before)::timestamptz
    ORDER BY work.settled_at, work.interval_event_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM settlement.policy_work AS work
USING candidate
WHERE work.interval_event_id = candidate.interval_event_id;

-- name: StorageCleanupDeleteHNRWork :execrows
WITH candidate AS (
    SELECT work.interval_event_id
    FROM settlement.hnr_work AS work
    WHERE work.processed_at < sqlc.arg(terminal_before)::timestamptz
    ORDER BY work.processed_at, work.interval_event_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM settlement.hnr_work AS work
USING candidate
WHERE work.interval_event_id = candidate.interval_event_id;

-- name: StorageCleanupListTrafficSettlements :many
SELECT settlement.settlement_id
FROM ledger.traffic_settlements AS settlement
WHERE settlement.interval_ends_at < sqlc.arg(detail_before)::timestamptz
  AND NOT EXISTS (
      SELECT 1
      FROM settlement.traffic_outbox AS outbox
      WHERE outbox.settlement_id = settlement.settlement_id
  )
ORDER BY settlement.interval_ends_at, settlement.settlement_id
LIMIT sqlc.arg(batch_size)::integer
FOR UPDATE SKIP LOCKED;

-- name: StorageCleanupDeleteTrafficSettlementSegments :execrows
DELETE FROM ledger.traffic_settlement_segments
WHERE settlement_id = ANY(sqlc.arg(settlement_ids)::uuid[]);

-- name: StorageCleanupDeleteTrafficSettlements :execrows
DELETE FROM ledger.traffic_settlements
WHERE settlement_id = ANY(sqlc.arg(settlement_ids)::uuid[]);

-- name: StorageCleanupDeleteSeedingSources :execrows
WITH candidate AS (
    SELECT source.window_start, source.user_id, source.torrent_id, source.interval_event_id
    FROM ledger.seeding_evidence_sources AS source
    WHERE source.window_start < sqlc.arg(detail_before)::timestamptz
      AND NOT EXISTS (
          SELECT 1
          FROM ledger.seeding_evidence_anomalies AS anomaly
          WHERE anomaly.window_start = source.window_start
      )
    ORDER BY source.window_start, source.user_id, source.torrent_id, source.interval_event_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM ledger.seeding_evidence_sources AS source
USING candidate
WHERE source.window_start = candidate.window_start
  AND source.user_id = candidate.user_id
  AND source.torrent_id = candidate.torrent_id
  AND source.interval_event_id = candidate.interval_event_id;

-- name: StorageCleanupDeleteSpeedObservations :execrows
WITH candidate AS (
    SELECT observation.interval_event_id
    FROM ledger.speed_observations AS observation
    WHERE observation.observed_at < sqlc.arg(anomaly_before)::timestamptz
    ORDER BY observation.observed_at, observation.interval_event_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM ledger.speed_observations AS observation
USING candidate
WHERE observation.interval_event_id = candidate.interval_event_id;

-- name: StorageCleanupDeleteSeedingAnomalies :execrows
WITH candidate AS (
    SELECT anomaly.id
    FROM ledger.seeding_evidence_anomalies AS anomaly
    WHERE anomaly.detected_at < sqlc.arg(anomaly_before)::timestamptz
    ORDER BY anomaly.detected_at, anomaly.id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM ledger.seeding_evidence_anomalies AS anomaly
USING candidate
WHERE anomaly.id = candidate.id;

-- name: StorageCleanupDeleteSnapshotEntries :execrows
WITH candidate AS (
    SELECT entry.snapshot_id, entry.info_hash_v1
    FROM ledger.seeding_swarm_snapshot_entries AS entry
    INNER JOIN ledger.seeding_swarm_snapshots AS snapshot
        ON snapshot.snapshot_id = entry.snapshot_id
    WHERE snapshot.status IN ('collecting', 'complete')
      AND (
        (
            snapshot.observed_at < coalesce(
                (SELECT max(evidence_window.window_end) FROM ledger.seeding_evidence_windows AS evidence_window),
                '-infinity'::timestamptz
            )
            AND NOT EXISTS (
                SELECT 1
                FROM ledger.seeding_evidence_windows AS evidence_window
                WHERE evidence_window.selected_snapshot_id = snapshot.snapshot_id
            )
        )
        OR
        (
            snapshot.observed_at < sqlc.arg(detail_before)::timestamptz
            AND EXISTS (
                SELECT 1
                FROM ledger.seeding_evidence_windows AS retained_window
                WHERE retained_window.selected_snapshot_id = snapshot.snapshot_id
                  AND retained_window.window_end < sqlc.arg(detail_before)::timestamptz
            )
            AND NOT EXISTS (
                SELECT 1
                FROM ledger.seeding_evidence_windows AS anomalous_window
                INNER JOIN ledger.seeding_evidence_anomalies AS anomaly
                    ON anomaly.window_start = anomalous_window.window_start
                WHERE anomalous_window.selected_snapshot_id = snapshot.snapshot_id
            )
        )
    )
    ORDER BY snapshot.observed_at, entry.snapshot_id, entry.info_hash_v1
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE OF entry SKIP LOCKED
)
DELETE FROM ledger.seeding_swarm_snapshot_entries AS entry
USING candidate
WHERE entry.snapshot_id = candidate.snapshot_id
  AND entry.info_hash_v1 = candidate.info_hash_v1;

-- name: StorageCleanupDeleteSnapshotChunks :execrows
WITH candidate AS (
    SELECT chunk.snapshot_id, chunk.chunk_index
    FROM ledger.seeding_swarm_snapshot_chunks AS chunk
    INNER JOIN ledger.seeding_swarm_snapshots AS snapshot
        ON snapshot.snapshot_id = chunk.snapshot_id
    WHERE snapshot.status IN ('collecting', 'complete')
      AND snapshot.observed_at < sqlc.arg(detail_before)::timestamptz
      AND snapshot.observed_at < coalesce(
          (SELECT max(evidence_window.window_end) FROM ledger.seeding_evidence_windows AS evidence_window),
          '-infinity'::timestamptz
      )
    ORDER BY snapshot.observed_at, chunk.snapshot_id, chunk.chunk_index
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE OF chunk SKIP LOCKED
)
DELETE FROM ledger.seeding_swarm_snapshot_chunks AS chunk
USING candidate
WHERE chunk.snapshot_id = candidate.snapshot_id
  AND chunk.chunk_index = candidate.chunk_index;

-- name: StorageCleanupDeleteSnapshotInbox :execrows
WITH candidate AS (
    SELECT inbox.event_id
    FROM settlement.seeding_swarm_snapshot_inbox AS inbox
    INNER JOIN ledger.seeding_swarm_snapshots AS snapshot
        ON snapshot.snapshot_id = inbox.snapshot_id
    WHERE inbox.received_at < sqlc.arg(detail_before)::timestamptz
      AND snapshot.status IN ('collecting', 'complete')
      AND snapshot.observed_at < coalesce(
          (SELECT max(evidence_window.window_end) FROM ledger.seeding_evidence_windows AS evidence_window),
          '-infinity'::timestamptz
      )
      AND NOT EXISTS (
          SELECT 1
          FROM ledger.seeding_swarm_snapshot_chunks AS chunk
          WHERE chunk.event_id = inbox.event_id
      )
    ORDER BY inbox.received_at, inbox.event_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM settlement.seeding_swarm_snapshot_inbox AS inbox
USING candidate
WHERE inbox.event_id = candidate.event_id;

-- name: StorageCleanupDeleteSnapshotRuns :execrows
WITH candidate AS (
    SELECT snapshot.snapshot_id
    FROM ledger.seeding_swarm_snapshots AS snapshot
    WHERE snapshot.status IN ('collecting', 'complete')
      AND snapshot.observed_at < sqlc.arg(detail_before)::timestamptz
      AND snapshot.observed_at < coalesce(
          (SELECT max(evidence_window.window_end) FROM ledger.seeding_evidence_windows AS evidence_window),
          '-infinity'::timestamptz
      )
      AND NOT EXISTS (
          SELECT 1 FROM settlement.seeding_swarm_snapshot_inbox AS inbox
          WHERE inbox.snapshot_id = snapshot.snapshot_id
      )
      AND NOT EXISTS (
          SELECT 1 FROM ledger.seeding_swarm_snapshot_chunks AS chunk
          WHERE chunk.snapshot_id = snapshot.snapshot_id
      )
      AND NOT EXISTS (
          SELECT 1 FROM ledger.seeding_swarm_snapshot_entries AS entry
          WHERE entry.snapshot_id = snapshot.snapshot_id
      )
      AND NOT EXISTS (
          SELECT 1 FROM ledger.seeding_evidence_windows AS evidence_window
          WHERE evidence_window.selected_snapshot_id = snapshot.snapshot_id
             OR evidence_window.snapshot_fence_id = snapshot.snapshot_id
      )
      AND EXISTS (
          SELECT 1 FROM ledger.seeding_swarm_snapshots AS newer
          WHERE newer.source_id = snapshot.source_id
            AND newer.routing_epoch = snapshot.routing_epoch
            AND newer.snapshot_sequence > snapshot.snapshot_sequence
      )
    ORDER BY snapshot.observed_at, snapshot.snapshot_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE OF snapshot SKIP LOCKED
)
DELETE FROM ledger.seeding_swarm_snapshots AS snapshot
USING candidate
WHERE snapshot.snapshot_id = candidate.snapshot_id;

-- name: StorageCleanupDeleteRawIntervals :execrows
WITH evidence_head AS (
    SELECT max(window_end) AS closed_through
    FROM ledger.seeding_evidence_windows
), candidate AS (
    SELECT raw.event_id
    FROM ledger.raw_session_intervals AS raw
    CROSS JOIN evidence_head
    WHERE raw.ends_at < sqlc.arg(detail_before)::timestamptz
      AND (
          raw.previous_left <> 0
          OR raw.current_left <> 0
          OR raw.ends_at <= coalesce(evidence_head.closed_through, '-infinity'::timestamptz)
      )
      AND NOT EXISTS (
          SELECT 1 FROM settlement.policy_work AS work
          WHERE work.interval_event_id = raw.event_id
      )
      AND NOT EXISTS (
          SELECT 1 FROM ledger.traffic_settlements AS traffic
          WHERE traffic.settlement_id = raw.event_id
      )
      AND NOT EXISTS (
          SELECT 1 FROM settlement.hnr_work AS work
          WHERE work.interval_event_id = raw.event_id
      )
      AND NOT EXISTS (
          SELECT 1 FROM ledger.speed_observations AS observation
          WHERE observation.interval_event_id = raw.event_id
      )
      AND NOT EXISTS (
          SELECT 1 FROM ledger.seeding_evidence_sources AS source
          WHERE source.interval_event_id = raw.event_id
      )
      AND NOT EXISTS (
          SELECT 1 FROM ledger.seeding_evidence_anomalies AS anomaly
          WHERE anomaly.interval_event_id = raw.event_id
      )
      AND NOT EXISTS (
          SELECT 1
          FROM ledger.hnr_obligations AS obligation
          INNER JOIN ledger.hnr_completion_assessments AS assessment
              ON assessment.id = obligation.assessment_id
          WHERE obligation.state = 'tracking'
            AND assessment.user_id = raw.user_id
            AND assessment.torrent_id = raw.torrent_id
            AND raw.ends_at > assessment.completed_at
      )
    ORDER BY raw.ends_at, raw.event_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM ledger.raw_session_intervals AS raw
USING candidate
WHERE raw.event_id = candidate.event_id;

-- name: StorageCleanupDeleteSessions :execrows
WITH candidate AS (
    SELECT state.user_id, state.torrent_id, state.session_token
    FROM settlement.session_states AS state
    WHERE state.updated_at < sqlc.arg(session_before)::timestamptz
    ORDER BY state.updated_at, state.user_id, state.torrent_id, state.session_token
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM settlement.session_states AS state
USING candidate
WHERE state.user_id = candidate.user_id
  AND state.torrent_id = candidate.torrent_id
  AND state.session_token = candidate.session_token;

-- name: StorageCleanupDeleteLegacyInbox :execrows
WITH candidate AS (
    SELECT inbox.event_id
    FROM settlement.event_inbox AS inbox
    WHERE inbox.processed_at < sqlc.arg(detail_before)::timestamptz
      AND inbox.outcome <> 'processing'
      AND NOT EXISTS (
          SELECT 1 FROM settlement.ingest_stream_cursors AS cursor
          WHERE cursor.last_event_id = inbox.event_id
      )
      AND NOT EXISTS (
          SELECT 1 FROM settlement.session_states AS state
          WHERE state.last_event_id = inbox.event_id
      )
      AND NOT EXISTS (
          SELECT 1 FROM ledger.raw_session_intervals AS raw
          WHERE raw.event_id = inbox.event_id OR raw.previous_event_id = inbox.event_id
      )
    ORDER BY inbox.processed_at, inbox.event_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM settlement.event_inbox AS inbox
USING candidate
WHERE inbox.event_id = candidate.event_id;
