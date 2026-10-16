-- +goose Up

-- The last dependency can disappear before a raw interval becomes eligible.
-- Record that signal in the dependency's transaction and retry durably.
CREATE TABLE settlement.raw_interval_gc (
    event_id uuid PRIMARY KEY,
    retry_after timestamptz NOT NULL,
    enqueued_at timestamptz NOT NULL DEFAULT statement_timestamp()
);
CREATE INDEX raw_interval_gc_ready_idx
    ON settlement.raw_interval_gc (retry_after, event_id);

CREATE TABLE settlement.raw_interval_gc_backfill (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    ends_at timestamptz NOT NULL DEFAULT '-infinity',
    event_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000'
);
INSERT INTO settlement.raw_interval_gc_backfill (singleton) VALUES (true);

CREATE TABLE settlement.traffic_detail_gc (
    settlement_id uuid PRIMARY KEY,
    retry_after timestamptz NOT NULL
);
CREATE INDEX traffic_detail_gc_ready_idx ON settlement.traffic_detail_gc (retry_after, settlement_id);
CREATE TABLE settlement.traffic_detail_gc_backfill (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    ends_at timestamptz NOT NULL DEFAULT '-infinity',
    settlement_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000'
);
INSERT INTO settlement.traffic_detail_gc_backfill (singleton) VALUES (true);

-- +goose StatementBegin
CREATE FUNCTION settlement.enqueue_traffic_detail_gc()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO settlement.traffic_detail_gc (settlement_id,retry_after)
    SELECT s.settlement_id, greatest(s.interval_ends_at + interval '12 hours',statement_timestamp())
    FROM deleted_traffic_outbox AS d
    INNER JOIN ledger.traffic_settlements AS s USING (settlement_id)
    ORDER BY s.settlement_id
    ON CONFLICT (settlement_id) DO NOTHING;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER traffic_outbox_detail_gc AFTER DELETE ON settlement.traffic_outbox
REFERENCING OLD TABLE AS deleted_traffic_outbox
FOR EACH STATEMENT EXECUTE FUNCTION settlement.enqueue_traffic_detail_gc();

-- +goose StatementBegin
CREATE FUNCTION settlement.enqueue_raw_interval_gc()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO settlement.raw_interval_gc (event_id, retry_after)
    SELECT raw.event_id,
           greatest(raw.ends_at + interval '12 hours', statement_timestamp())
    FROM (
        SELECT DISTINCT (to_jsonb(deleted) ->> TG_ARGV[0])::uuid AS event_id
        FROM deleted_gc_dependencies AS deleted
    ) AS candidate
    INNER JOIN ledger.raw_session_intervals AS raw USING (event_id)
    -- Unclosed seeding hours already have a durable range cursor below. Do
    -- not create another per-heartbeat backlog while coverage is missing;
    -- the bounded backfill reaches these intervals after the hour closes.
    WHERE raw.previous_left <> 0 OR raw.current_left <> 0
       OR raw.ends_at <= coalesce(
           (SELECT max(window_end) FROM ledger.seeding_evidence_windows), '-infinity'::timestamptz)
    ORDER BY raw.event_id
    ON CONFLICT (event_id) DO NOTHING;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER policy_work_raw_gc AFTER DELETE ON settlement.policy_work
REFERENCING OLD TABLE AS deleted_gc_dependencies
FOR EACH STATEMENT EXECUTE FUNCTION settlement.enqueue_raw_interval_gc('interval_event_id');
CREATE TRIGGER hnr_work_raw_gc AFTER DELETE ON settlement.hnr_work
REFERENCING OLD TABLE AS deleted_gc_dependencies
FOR EACH STATEMENT EXECUTE FUNCTION settlement.enqueue_raw_interval_gc('interval_event_id');
CREATE TRIGGER traffic_settlements_raw_gc AFTER DELETE ON ledger.traffic_settlements
REFERENCING OLD TABLE AS deleted_gc_dependencies
FOR EACH STATEMENT EXECUTE FUNCTION settlement.enqueue_raw_interval_gc('settlement_id');
CREATE TRIGGER speed_observations_raw_gc AFTER DELETE ON ledger.speed_observations
REFERENCING OLD TABLE AS deleted_gc_dependencies
FOR EACH STATEMENT EXECUTE FUNCTION settlement.enqueue_raw_interval_gc('interval_event_id');
CREATE TRIGGER seeding_sources_raw_gc AFTER DELETE ON ledger.seeding_evidence_sources
REFERENCING OLD TABLE AS deleted_gc_dependencies
FOR EACH STATEMENT EXECUTE FUNCTION settlement.enqueue_raw_interval_gc('interval_event_id');
CREATE TRIGGER seeding_anomalies_raw_gc AFTER DELETE ON ledger.seeding_evidence_anomalies
REFERENCING OLD TABLE AS deleted_gc_dependencies
FOR EACH STATEMENT EXECUTE FUNCTION settlement.enqueue_raw_interval_gc('interval_event_id');

-- Keep the exact first and last complete frame of each UTC hour and route.
-- Interior frames cannot be the boundary-before sample or boundary-after fence
-- of an hourly reward window, even if an older missing hour has not closed.
CREATE INDEX seeding_snapshot_route_hour_idx
    ON ledger.seeding_swarm_snapshots (source_id, routing_epoch, observed_at, snapshot_sequence)
    WHERE status = 'complete';

-- +goose StatementBegin
CREATE FUNCTION ledger.is_redundant_hourly_snapshot(candidate_id uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
SELECT EXISTS (
    SELECT 1 FROM ledger.seeding_swarm_snapshots AS snapshot
    WHERE snapshot.snapshot_id = candidate_id
      AND snapshot.status = 'complete'
      AND snapshot.observed_at <> date_trunc('hour', snapshot.observed_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
      AND EXISTS (
          SELECT 1 FROM ledger.seeding_swarm_snapshots AS earlier
          WHERE earlier.source_id = snapshot.source_id
            AND earlier.routing_epoch = snapshot.routing_epoch
            AND earlier.status = 'complete'
            AND earlier.snapshot_sequence < snapshot.snapshot_sequence
            AND earlier.observed_at >= date_trunc('hour', snapshot.observed_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
            AND earlier.observed_at < (date_trunc('hour', snapshot.observed_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') + interval '1 hour'
      )
      AND EXISTS (
          SELECT 1 FROM ledger.seeding_swarm_snapshots AS later
          WHERE later.source_id = snapshot.source_id
            AND later.routing_epoch = snapshot.routing_epoch
            AND later.status = 'complete'
            AND later.snapshot_sequence > snapshot.snapshot_sequence
            AND later.observed_at >= date_trunc('hour', snapshot.observed_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
            AND later.observed_at < (date_trunc('hour', snapshot.observed_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') + interval '1 hour'
      )
      -- Fence selection is scoped to the consumer's snapshot stream. If a
      -- route moved between streams, retain the first complete frame seen by
      -- each stream as well as the route's first frame.
      AND NOT EXISTS (
          SELECT 1 FROM settlement.seeding_swarm_snapshot_inbox AS delivery
          WHERE delivery.snapshot_id = snapshot.snapshot_id
            AND NOT EXISTS (
                SELECT 1 FROM ledger.seeding_swarm_snapshots AS earlier
                INNER JOIN settlement.seeding_swarm_snapshot_inbox AS earlier_delivery
                    ON earlier_delivery.snapshot_id = earlier.snapshot_id
                WHERE earlier.source_id = snapshot.source_id
                  AND earlier.routing_epoch = snapshot.routing_epoch
                  AND earlier.status = 'complete'
                  AND earlier.snapshot_sequence < snapshot.snapshot_sequence
                  AND earlier_delivery.source_stream = delivery.source_stream
                  AND earlier.observed_at >= date_trunc('hour', snapshot.observed_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
                  AND earlier.observed_at < (date_trunc('hour', snapshot.observed_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') + interval '1 hour'
            )
      )
      AND NOT EXISTS (SELECT 1 FROM ledger.seeding_evidence_windows AS w WHERE w.selected_snapshot_id = snapshot.snapshot_id)
      AND NOT EXISTS (SELECT 1 FROM ledger.seeding_evidence_windows AS w WHERE w.snapshot_fence_id = snapshot.snapshot_id)
);
$$;
-- +goose StatementEnd

CREATE TABLE settlement.snapshot_entry_gc_progress (
    snapshot_id uuid PRIMARY KEY REFERENCES ledger.seeding_swarm_snapshots(snapshot_id) ON DELETE CASCADE,
    finished_at timestamptz NOT NULL DEFAULT statement_timestamp()
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_recent_seeding_snapshot_entries()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM (SELECT DISTINCT snapshot_id FROM deleted_snapshot_entries) AS deleted
        INNER JOIN ledger.seeding_swarm_snapshots AS snapshot USING (snapshot_id)
        WHERE snapshot.observed_at >= coalesce(
            (SELECT max(window_end) FROM ledger.seeding_evidence_windows), '-infinity'::timestamptz)
          AND (snapshot.observed_at >= statement_timestamp() - interval '12 hours'
               OR NOT ledger.is_redundant_hourly_snapshot(snapshot.snapshot_id))
    ) OR EXISTS (
        SELECT 1
        FROM (SELECT DISTINCT snapshot_id FROM deleted_snapshot_entries) AS deleted
        INNER JOIN ledger.seeding_evidence_windows AS w ON w.selected_snapshot_id = deleted.snapshot_id
        WHERE w.window_end >= statement_timestamp() - interval '12 hours'
           OR EXISTS (SELECT 1 FROM ledger.seeding_evidence_anomalies AS a WHERE a.window_start = w.window_start)
    ) THEN
        RAISE EXCEPTION 'recent, boundary or anomalous selected seeding snapshot entries are retained';
    END IF;
    INSERT INTO settlement.snapshot_entry_gc_progress (snapshot_id)
    SELECT DISTINCT deleted.snapshot_id FROM deleted_snapshot_entries AS deleted
    WHERE NOT EXISTS (
        SELECT 1 FROM ledger.seeding_swarm_snapshot_entries AS remaining
        WHERE remaining.snapshot_id = deleted.snapshot_id LIMIT 1 OFFSET 0)
    ON CONFLICT (snapshot_id) DO NOTHING;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION '202610160001 is irreversible after redundant snapshot cleanup; restore application binaries forward';
END $$;
-- +goose StatementEnd
