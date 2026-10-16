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

-- name: StorageCleanupDeletePolicyWork :many
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
WHERE work.interval_event_id = candidate.interval_event_id
RETURNING work.interval_event_id;

-- name: StorageCleanupDeleteHNRWork :many
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
WHERE work.interval_event_id = candidate.interval_event_id
RETURNING work.interval_event_id;

-- name: StorageCleanupListTrafficSettlements :many
SELECT settlement.settlement_id
FROM ledger.traffic_settlements AS settlement
LEFT JOIN LATERAL (
    -- OFFSET 0 deliberately keeps this as a parameterized primary-key probe.
    -- The equivalent NOT EXISTS is estimated as nearly empty and PostgreSQL
    -- otherwise builds a hash over both multi-million-row ledgers before it
    -- can return the first cleanup batch.
    SELECT outbox.settlement_id
    FROM settlement.traffic_outbox AS outbox
    WHERE outbox.settlement_id = settlement.settlement_id
    OFFSET 0
) AS retained_outbox ON true
WHERE settlement.settlement_id = ANY(sqlc.arg(settlement_ids)::uuid[])
  AND settlement.interval_ends_at < sqlc.arg(detail_before)::timestamptz
  AND retained_outbox.settlement_id IS NULL
ORDER BY settlement.interval_ends_at, settlement.settlement_id
LIMIT sqlc.arg(batch_size)::integer
FOR UPDATE OF settlement SKIP LOCKED;

-- name: StorageCleanupListDueTrafficDetails :many
SELECT settlement_id FROM settlement.traffic_detail_gc
WHERE retry_after <= statement_timestamp()
ORDER BY retry_after,settlement_id
LIMIT sqlc.arg(batch_size)::integer
FOR UPDATE SKIP LOCKED;

-- name: StorageCleanupCompleteTrafficDetailJobs :exec
DELETE FROM settlement.traffic_detail_gc AS job
WHERE job.settlement_id = ANY(sqlc.arg(settlement_ids)::uuid[])
  AND (SELECT true FROM ledger.traffic_settlements AS s
       WHERE s.settlement_id=job.settlement_id LIMIT 1 OFFSET 0) IS NULL;

-- name: StorageCleanupDeferTrafficDetailJobs :exec
UPDATE settlement.traffic_detail_gc
SET retry_after=statement_timestamp()+interval '5 minutes'
WHERE settlement_id=ANY(sqlc.arg(settlement_ids)::uuid[]);

-- name: StorageCleanupBackfillTrafficDetailJobs :one
WITH cursor AS MATERIALIZED (
    SELECT ends_at,settlement_id FROM settlement.traffic_detail_gc_backfill
    WHERE singleton FOR UPDATE
), candidate AS MATERIALIZED (
    SELECT s.settlement_id,s.interval_ends_at
    FROM ledger.traffic_settlements AS s,cursor
    WHERE (s.interval_ends_at,s.settlement_id)>(cursor.ends_at,cursor.settlement_id)
      AND s.interval_ends_at<sqlc.arg(detail_before)::timestamptz
    ORDER BY s.interval_ends_at,s.settlement_id
    LIMIT sqlc.arg(batch_size)::integer
), queued AS (
    INSERT INTO settlement.traffic_detail_gc (settlement_id,retry_after)
    SELECT c.settlement_id,statement_timestamp() FROM candidate AS c
    WHERE (SELECT true FROM settlement.traffic_outbox AS o
           WHERE o.settlement_id=c.settlement_id LIMIT 1 OFFSET 0) IS NULL
    ORDER BY c.settlement_id
    ON CONFLICT (settlement_id) DO NOTHING RETURNING settlement_id
), advanced AS (
    UPDATE settlement.traffic_detail_gc_backfill AS target
    SET ends_at=last.interval_ends_at,settlement_id=last.settlement_id
    FROM (SELECT interval_ends_at,settlement_id FROM candidate
          ORDER BY interval_ends_at DESC,settlement_id DESC LIMIT 1) AS last
    WHERE target.singleton
)
SELECT count(*) FROM queued;

-- name: StorageCleanupDeleteTrafficSettlementSegments :execrows
DELETE FROM ledger.traffic_settlement_segments
WHERE settlement_id = ANY(sqlc.arg(settlement_ids)::uuid[]);

-- name: StorageCleanupDeleteTrafficSettlements :execrows
DELETE FROM ledger.traffic_settlements
WHERE settlement_id = ANY(sqlc.arg(settlement_ids)::uuid[]);

-- name: StorageCleanupDeleteSeedingSources :many
WITH eligible_window AS MATERIALIZED (
    -- Evidence windows are one row per hour. Select from that compact ledger
    -- first so retained anomalous hours do not force every source row in those
    -- hours to be scanned again on each cleanup pass. Processing one eligible
    -- hour at a time also forces a narrow primary-key prefix scan.
    SELECT evidence_window.window_start
    FROM ledger.seeding_evidence_windows AS evidence_window
    WHERE evidence_window.window_start < sqlc.arg(detail_before)::timestamptz
      AND NOT EXISTS(SELECT 1 FROM settlement.seeding_reconciliation_work r
                     WHERE r.window_start=evidence_window.window_start AND r.resolved_at IS NULL)
      AND EXISTS (
          SELECT 1
          FROM ledger.seeding_evidence_sources AS existing_source
          WHERE existing_source.window_start = evidence_window.window_start
      )
      AND NOT EXISTS (
          SELECT 1
          FROM ledger.seeding_evidence_anomalies AS anomaly
          WHERE anomaly.window_start = evidence_window.window_start
      )
    ORDER BY evidence_window.window_start
    LIMIT 1
), candidate AS (
    SELECT source.window_start, source.user_id, source.torrent_id, source.interval_event_id
    FROM eligible_window
    CROSS JOIN LATERAL (
        SELECT source.window_start, source.user_id, source.torrent_id, source.interval_event_id
        FROM ledger.seeding_evidence_sources AS source
        WHERE source.window_start = eligible_window.window_start
        ORDER BY source.user_id, source.torrent_id, source.interval_event_id
        LIMIT sqlc.arg(batch_size)::integer
        FOR UPDATE OF source SKIP LOCKED
    ) AS source
)
DELETE FROM ledger.seeding_evidence_sources AS source
USING candidate
WHERE source.window_start = candidate.window_start
  AND source.user_id = candidate.user_id
  AND source.torrent_id = candidate.torrent_id
  AND source.interval_event_id = candidate.interval_event_id
RETURNING source.interval_event_id;

-- name: StorageCleanupDeleteSpeedObservations :many
WITH candidate AS (
    SELECT observation.interval_event_id
    FROM ledger.speed_observations AS observation
    WHERE (
        observation.outcome = 'exceeded'
        AND observation.observed_at < sqlc.arg(anomaly_before)::timestamptz
    ) OR (
        observation.outcome <> 'exceeded'
        AND observation.observed_at < sqlc.arg(detail_before)::timestamptz
    )
    ORDER BY observation.observed_at, observation.interval_event_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM ledger.speed_observations AS observation
USING candidate
WHERE observation.interval_event_id = candidate.interval_event_id
RETURNING observation.interval_event_id;

-- name: StorageCleanupDeleteSeedingAnomalies :many
WITH candidate AS (
    SELECT anomaly.id
    FROM ledger.seeding_evidence_anomalies AS anomaly
    WHERE anomaly.detected_at < sqlc.arg(anomaly_before)::timestamptz
      AND NOT EXISTS(SELECT 1 FROM settlement.seeding_reconciliation_work r
                     WHERE r.window_start=anomaly.window_start AND r.resolved_at IS NULL)
    ORDER BY anomaly.detected_at, anomaly.id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM ledger.seeding_evidence_anomalies AS anomaly
USING candidate
WHERE anomaly.id = candidate.id
RETURNING anomaly.interval_event_id;

-- name: StorageCleanupDeleteSnapshotEntries :execrows
WITH eligible_snapshot AS MATERIALIZED (
    -- Snapshot entries are keyed by snapshot_id. Choose one eligible snapshot
    -- from the compact snapshot ledger first, then walk that primary-key prefix
    -- instead of sorting every retained entry on each cleanup pass.
    SELECT snapshot.snapshot_id
    -- Keep ordering before the expensive evidence and entry probes. Without
    -- this fence PostgreSQL can test every retained snapshot, then sort all
    -- qualifying rows merely to return the first cleanup candidate.
    FROM (
        SELECT snapshot_id, observed_at, status
        FROM ledger.seeding_swarm_snapshots
        ORDER BY observed_at, snapshot_id
        OFFSET 0
    ) AS snapshot
    WHERE (
        SELECT true FROM settlement.snapshot_entry_gc_progress AS gc
        WHERE gc.snapshot_id = snapshot.snapshot_id LIMIT 1 OFFSET 0
    ) IS NULL
      AND snapshot.status IN ('collecting', 'complete')
      AND NOT EXISTS (SELECT 1 FROM ledger.seeding_evidence_windows w JOIN settlement.seeding_reconciliation_work r USING(window_start)
                      WHERE r.resolved_at IS NULL AND (w.selected_snapshot_id=snapshot.snapshot_id OR w.snapshot_fence_id=snapshot.snapshot_id))
      AND (
        (
            snapshot.observed_at < coalesce(
                (SELECT ledger.seeding_closed_through()),
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
            AND ledger.is_redundant_hourly_snapshot(snapshot.snapshot_id)
        )
        OR
        (
            snapshot.observed_at < sqlc.arg(detail_before)::timestamptz
            -- Legacy entry guards retain selected frames after a coverage
            -- hole. Match that guard instead of repeatedly failing the pass.
            AND snapshot.observed_at < coalesce(
                (SELECT ledger.seeding_closed_through()),
                '-infinity'::timestamptz
            )
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
      AND (
          SELECT true
          FROM ledger.seeding_swarm_snapshot_entries AS existing_entry
          WHERE existing_entry.snapshot_id = snapshot.snapshot_id
          LIMIT 1 OFFSET 0
      ) IS TRUE
    ORDER BY snapshot.observed_at, snapshot.snapshot_id
    LIMIT 1
), candidate AS (
    SELECT entry.snapshot_id, entry.info_hash_v1
    FROM eligible_snapshot
    CROSS JOIN LATERAL (
        SELECT entry.snapshot_id, entry.info_hash_v1
        FROM ledger.seeding_swarm_snapshot_entries AS entry
        WHERE entry.snapshot_id = eligible_snapshot.snapshot_id
        ORDER BY entry.info_hash_v1
        LIMIT sqlc.arg(batch_size)::integer
        FOR UPDATE OF entry SKIP LOCKED
    ) AS entry
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
    WHERE NOT EXISTS(SELECT 1 FROM ledger.seeding_snapshot_payloads p
                     WHERE p.snapshot_id=chunk.snapshot_id AND p.chunk_index=chunk.chunk_index)
      AND snapshot.status IN ('collecting', 'complete')
      AND snapshot.observed_at < sqlc.arg(detail_before)::timestamptz
      AND snapshot.observed_at < coalesce(
          (SELECT ledger.seeding_closed_through()),
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
          (SELECT ledger.seeding_closed_through()),
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
          (SELECT ledger.seeding_closed_through()),
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
WITH candidate AS (
    SELECT raw.event_id
    FROM ledger.raw_session_intervals AS raw
    WHERE raw.event_id = ANY(sqlc.arg(candidate_event_ids)::uuid[])
      AND raw.ends_at < sqlc.arg(detail_before)::timestamptz
      AND (
          raw.previous_left <> 0
          OR raw.current_left <> 0
          OR ledger.seeding_interval_is_closed(raw.starts_at,raw.ends_at)
          OR ledger.raw_seeding_is_compacted(raw)
      )
      AND (
          SELECT true FROM settlement.policy_work AS work
          WHERE work.interval_event_id = raw.event_id
          LIMIT 1 OFFSET 0
      ) IS NULL
      AND (
          SELECT true FROM ledger.traffic_settlements AS traffic
          WHERE traffic.settlement_id = raw.event_id
          LIMIT 1 OFFSET 0
      ) IS NULL
      AND (
          SELECT true FROM settlement.hnr_work AS work
          WHERE work.interval_event_id = raw.event_id
          LIMIT 1 OFFSET 0
      ) IS NULL
      AND (
          SELECT true FROM ledger.speed_observations AS observation
          WHERE observation.interval_event_id = raw.event_id
          LIMIT 1 OFFSET 0
      ) IS NULL
      AND (
          SELECT true FROM ledger.seeding_evidence_sources AS source
          WHERE source.interval_event_id = raw.event_id
          LIMIT 1 OFFSET 0
      ) IS NULL
      AND (
          SELECT true FROM ledger.seeding_evidence_anomalies AS anomaly
          WHERE anomaly.interval_event_id = raw.event_id
          LIMIT 1 OFFSET 0
      ) IS NULL
      AND NOT ledger.raw_hnr_is_required(raw)
    ORDER BY raw.ends_at, raw.event_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM ledger.raw_session_intervals AS raw
USING candidate
WHERE raw.event_id = candidate.event_id;

-- name: StorageCleanupListDueRawIntervals :many
SELECT event_id FROM settlement.raw_interval_gc
WHERE retry_after <= statement_timestamp()
ORDER BY retry_after, event_id
LIMIT sqlc.arg(batch_size)::integer
FOR UPDATE SKIP LOCKED;

-- name: StorageCleanupCompleteRawIntervalJobs :exec
DELETE FROM settlement.raw_interval_gc AS job
WHERE job.event_id = ANY(sqlc.arg(event_ids)::uuid[])
  AND (SELECT true FROM ledger.raw_session_intervals AS raw
       WHERE raw.event_id = job.event_id LIMIT 1 OFFSET 0) IS NULL;

-- name: StorageCleanupDeferRawIntervalJobs :exec
UPDATE settlement.raw_interval_gc
SET retry_after = statement_timestamp() + interval '5 minutes'
WHERE event_id = ANY(sqlc.arg(event_ids)::uuid[]);

-- name: StorageCleanupBackfillRawIntervalJobs :one
WITH cursor AS MATERIALIZED (
    SELECT ends_at, event_id FROM settlement.raw_interval_gc_backfill
    WHERE singleton FOR UPDATE
), candidate AS MATERIALIZED (
    SELECT raw.event_id, raw.ends_at
    FROM ledger.raw_session_intervals AS raw, cursor
    WHERE (raw.ends_at, raw.event_id) > (cursor.ends_at, cursor.event_id)
      AND raw.ends_at < sqlc.arg(detail_before)::timestamptz
    ORDER BY raw.ends_at, raw.event_id
    LIMIT sqlc.arg(batch_size)::integer
), queued AS (
    INSERT INTO settlement.raw_interval_gc (event_id, retry_after)
    SELECT c.event_id,statement_timestamp() FROM candidate c
    JOIN ledger.raw_session_intervals raw USING(event_id)
    WHERE raw.previous_left<>0 OR raw.current_left<>0
       OR ledger.seeding_interval_is_closed(raw.starts_at,raw.ends_at)
       OR ledger.raw_seeding_is_compacted(raw)
    ORDER BY c.event_id
    ON CONFLICT (event_id) DO NOTHING RETURNING event_id
), advanced AS (
    UPDATE settlement.raw_interval_gc_backfill AS target
    SET ends_at = last.ends_at, event_id = last.event_id
    FROM (SELECT ends_at,event_id FROM candidate ORDER BY ends_at DESC,event_id DESC LIMIT 1) AS last
    WHERE target.singleton
)
SELECT count(*) FROM queued;

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
      AND (
          SELECT true FROM settlement.ingest_stream_cursors AS cursor
          WHERE cursor.last_event_id = inbox.event_id
          LIMIT 1 OFFSET 0
      ) IS NULL
      AND (
          SELECT true FROM settlement.session_states AS state
          WHERE state.last_event_id = inbox.event_id
          LIMIT 1 OFFSET 0
      ) IS NULL
      -- Keep these as two independent probes. Combining the predicates with
      -- OR makes PostgreSQL hash the complete raw interval ledger instead of
      -- using its primary-key and previous-event indexes.
      AND (
          SELECT true FROM ledger.raw_session_intervals AS raw
          WHERE raw.event_id = inbox.event_id
          LIMIT 1 OFFSET 0
      ) IS NULL
      AND (
          SELECT true FROM ledger.raw_session_intervals AS raw
          WHERE raw.previous_event_id = inbox.event_id
          LIMIT 1 OFFSET 0
      ) IS NULL
    ORDER BY inbox.processed_at, inbox.event_id
    LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE SKIP LOCKED
)
DELETE FROM settlement.event_inbox AS inbox
USING candidate
WHERE inbox.event_id = candidate.event_id;

-- name: StorageCleanupBackfillRawRangeJobs :one
WITH chosen AS MATERIALIZED (
    SELECT r.window_start,r.cursor_ends_at,r.cursor_event_id
    FROM settlement.raw_interval_gc_ranges r
    WHERE r.retry_after<=statement_timestamp()
      AND (EXISTS(SELECT 1 FROM ledger.seeding_evidence_windows w WHERE w.window_start=r.window_start)
           OR EXISTS(SELECT 1 FROM settlement.activity_projection_profiles p
                     WHERE p.activates_at<=r.window_start AND p.shadow_verified_windows>=p.required_shadow_windows))
    ORDER BY r.retry_after,r.window_start LIMIT 1 FOR UPDATE SKIP LOCKED
), candidate AS MATERIALIZED (
    SELECT raw.event_id,raw.ends_at
    FROM chosen,ledger.raw_session_intervals raw
    WHERE (raw.ends_at,raw.event_id)>(chosen.cursor_ends_at,chosen.cursor_event_id)
      AND raw.ends_at<=chosen.window_start+interval '2 hours'
      AND raw.ends_at<sqlc.arg(detail_before)::timestamptz
    ORDER BY raw.ends_at,raw.event_id LIMIT sqlc.arg(batch_size)::integer
), queued AS (
    INSERT INTO settlement.raw_interval_gc(event_id,retry_after)
    SELECT event_id,statement_timestamp() FROM candidate ORDER BY event_id
    ON CONFLICT(event_id) DO NOTHING RETURNING event_id
), advanced AS (
    UPDATE settlement.raw_interval_gc_ranges r
    SET cursor_ends_at=last.ends_at,cursor_event_id=last.event_id,retry_after=statement_timestamp()
    FROM chosen,(SELECT ends_at,event_id FROM candidate ORDER BY ends_at DESC,event_id DESC LIMIT 1) last
    WHERE r.window_start=chosen.window_start
), deferred AS (
    UPDATE settlement.raw_interval_gc_ranges r
    SET retry_after=r.window_start+interval '50 hours'
    FROM chosen WHERE r.window_start=chosen.window_start AND NOT EXISTS(SELECT 1 FROM candidate)
      AND r.window_start+interval '50 hours'>=statement_timestamp()
), finished AS (
    DELETE FROM settlement.raw_interval_gc_ranges r USING chosen
    WHERE r.window_start=chosen.window_start AND NOT EXISTS(SELECT 1 FROM candidate)
      AND r.window_start+interval '50 hours'<statement_timestamp()
)
SELECT count(*) FROM queued;

-- name: StorageCleanupDeleteSnapshotPayloads :execrows
WITH candidate AS (
    SELECT payload.snapshot_id,payload.chunk_index
    FROM (
        SELECT snapshot_id,observed_at FROM ledger.seeding_swarm_snapshots
        WHERE status='complete'
        ORDER BY observed_at,snapshot_id OFFSET 0
    ) snapshot
    JOIN LATERAL (
        SELECT p.snapshot_id,p.chunk_index FROM ledger.seeding_snapshot_payloads p
        WHERE p.snapshot_id=snapshot.snapshot_id
          AND ledger.snapshot_payload_can_expire(snapshot.snapshot_id)
        ORDER BY p.chunk_index LIMIT 128 FOR UPDATE SKIP LOCKED
    ) payload ON true
    ORDER BY snapshot.observed_at,snapshot.snapshot_id,payload.chunk_index LIMIT 128
)
DELETE FROM ledger.seeding_snapshot_payloads p USING candidate c
WHERE p.snapshot_id=c.snapshot_id AND p.chunk_index=c.chunk_index;

-- name: StorageCleanupDeleteActivityHours :execrows
WITH candidate AS (
    SELECT a.source_stream,a.window_start,a.user_id,a.torrent_id
    FROM settlement.seeding_activity_hours a
    JOIN ledger.seeding_evidence_windows w USING(window_start)
    WHERE w.schema_version='seeding.evidence.v3'
      AND NOT EXISTS(SELECT 1 FROM settlement.seeding_reconciliation_work r
                     WHERE r.window_start=w.window_start AND r.resolved_at IS NULL)
      AND w.window_end<sqlc.arg(detail_before)::timestamptz
      AND NOT EXISTS(SELECT 1 FROM ledger.seeding_evidence_anomalies e WHERE e.window_start=a.window_start)
    ORDER BY a.window_start,a.user_id,a.torrent_id LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE OF a SKIP LOCKED
)
DELETE FROM settlement.seeding_activity_hours a USING candidate c
WHERE (a.source_stream,a.window_start,a.user_id,a.torrent_id)=(c.source_stream,c.window_start,c.user_id,c.torrent_id);

-- name: StorageCleanupDeleteHNRCheckpoints :execrows
WITH candidate AS (
    SELECT p.obligation_id FROM settlement.hnr_progress_accumulators p
    JOIN ledger.hnr_obligations o ON o.id=p.obligation_id
    WHERE o.state<>'tracking' AND o.updated_at<sqlc.arg(detail_before)::timestamptz
    ORDER BY o.updated_at,p.obligation_id LIMIT sqlc.arg(batch_size)::integer
    FOR UPDATE OF p SKIP LOCKED
)
DELETE FROM settlement.hnr_progress_accumulators p USING candidate c WHERE p.obligation_id=c.obligation_id;
