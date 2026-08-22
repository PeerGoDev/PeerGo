-- +goose Up

-- Long-term accounting is a compact UTC-day projection. Detailed policy
-- slices remain available for 30 days, then this aggregate preserves the
-- actual credited/charged facts without one permanent row per announce.
CREATE TABLE ledger.traffic_daily_rollups (
    traffic_day date NOT NULL,
    user_id uuid NOT NULL,
    torrent_id bigint NOT NULL CHECK (torrent_id > 0),
    raw_uploaded numeric(30, 0) NOT NULL CHECK (raw_uploaded >= 0),
    raw_downloaded numeric(30, 0) NOT NULL CHECK (raw_downloaded >= 0),
    credited_uploaded numeric(30, 0) NOT NULL CHECK (credited_uploaded >= 0),
    charged_downloaded numeric(30, 0) NOT NULL CHECK (charged_downloaded >= 0),
    settlement_count bigint NOT NULL CHECK (settlement_count > 0),
    first_interval_at timestamptz NOT NULL,
    last_interval_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (traffic_day, user_id, torrent_id),
    CHECK (last_interval_at >= first_interval_at),
    CHECK (updated_at >= created_at)
);

CREATE INDEX traffic_daily_rollups_user_day_idx
    ON ledger.traffic_daily_rollups (user_id, traffic_day DESC, torrent_id);

-- Every maintenance predicate has a leading retention timestamp. These
-- indexes keep bounded deletion proportional to the batch size instead of
-- repeatedly scanning the live hot tables as they grow.
CREATE INDEX event_inbox_retention_idx
    ON settlement.event_inbox (processed_at, event_id)
    WHERE processed_at IS NOT NULL;
CREATE INDEX session_states_retention_idx
    ON settlement.session_states (updated_at, user_id, torrent_id, session_token);
CREATE INDEX policy_work_retention_idx
    ON settlement.policy_work (settled_at, interval_event_id)
    WHERE settled_at IS NOT NULL;
CREATE INDEX traffic_outbox_retention_idx
    ON settlement.traffic_outbox (published_at, event_id)
    WHERE published_at IS NOT NULL;
CREATE INDEX hnr_work_retention_idx
    ON settlement.hnr_work (processed_at, interval_event_id)
    WHERE processed_at IS NOT NULL;
CREATE INDEX hnr_outbox_retention_idx
    ON settlement.hnr_outbox (published_at, event_id)
    WHERE published_at IS NOT NULL;
CREATE INDEX seeding_evidence_outbox_retention_idx
    ON settlement.seeding_evidence_outbox (published_at, event_id)
    WHERE published_at IS NOT NULL;
CREATE INDEX seeding_swarm_snapshot_inbox_retention_idx
    ON settlement.seeding_swarm_snapshot_inbox (received_at, event_id);
CREATE INDEX seeding_swarm_snapshot_inbox_snapshot_retention_idx
    ON settlement.seeding_swarm_snapshot_inbox (snapshot_id, received_at, event_id);
CREATE INDEX traffic_settlements_retention_idx
    ON ledger.traffic_settlements (interval_ends_at, settlement_id);
CREATE INDEX raw_session_intervals_retention_idx
    ON ledger.raw_session_intervals (ends_at, event_id);
CREATE INDEX raw_session_intervals_previous_event_retention_idx
    ON ledger.raw_session_intervals (previous_event_id);
CREATE INDEX seeding_swarm_snapshots_retention_idx
    ON ledger.seeding_swarm_snapshots (status, observed_at, snapshot_id);
CREATE INDEX seeding_evidence_windows_selected_snapshot_retention_idx
    ON ledger.seeding_evidence_windows (selected_snapshot_id, window_end);
CREATE INDEX seeding_evidence_windows_fence_snapshot_retention_idx
    ON ledger.seeding_evidence_windows (snapshot_fence_id, window_end);
CREATE INDEX seeding_evidence_windows_end_retention_idx
    ON ledger.seeding_evidence_windows (window_end DESC);
CREATE INDEX speed_observations_retention_idx
    ON ledger.speed_observations (observed_at, interval_event_id);
CREATE INDEX seeding_evidence_anomalies_retention_idx
    ON ledger.seeding_evidence_anomalies (detected_at, id);
CREATE INDEX seeding_evidence_anomalies_interval_retention_idx
    ON ledger.seeding_evidence_anomalies (interval_event_id);
CREATE INDEX seeding_evidence_sources_interval_retention_idx
    ON ledger.seeding_evidence_sources (interval_event_id);

-- DELETE makes space reusable only after vacuum removes dead tuples. Tune the
-- high-churn tables to vacuum at a small absolute/relative threshold so their
-- physical files settle near the retention high-water mark instead of
-- accumulating avoidable bloat between default autovacuum cycles.
ALTER TABLE settlement.event_inbox SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE settlement.session_states SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE settlement.policy_work SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE settlement.traffic_outbox SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE settlement.hnr_work SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE settlement.hnr_outbox SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE settlement.seeding_evidence_outbox SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE settlement.seeding_swarm_snapshot_inbox SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE ledger.raw_session_intervals SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE ledger.traffic_settlements SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE ledger.traffic_settlement_segments SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE ledger.seeding_swarm_snapshot_entries SET (
    autovacuum_vacuum_scale_factor = 0.01, autovacuum_vacuum_threshold = 5000,
    autovacuum_analyze_scale_factor = 0.02, autovacuum_analyze_threshold = 5000
);
ALTER TABLE ledger.seeding_swarm_snapshot_chunks SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE ledger.seeding_swarm_snapshots SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE ledger.seeding_evidence_sources SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE ledger.seeding_evidence_anomalies SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);
ALTER TABLE ledger.speed_observations SET (
    autovacuum_vacuum_scale_factor = 0.02, autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05, autovacuum_analyze_threshold = 1000
);

INSERT INTO ledger.traffic_daily_rollups (
    traffic_day,
    user_id,
    torrent_id,
    raw_uploaded,
    raw_downloaded,
    credited_uploaded,
    charged_downloaded,
    settlement_count,
    first_interval_at,
    last_interval_at,
    created_at,
    updated_at
)
SELECT
    (interval_ends_at AT TIME ZONE 'UTC')::date,
    user_id,
    torrent_id,
    sum(raw_uploaded)::numeric(30, 0),
    sum(raw_downloaded)::numeric(30, 0),
    sum(credited_uploaded)::numeric(30, 0),
    sum(charged_downloaded)::numeric(30, 0),
    count(*)::bigint,
    min(interval_starts_at),
    max(interval_ends_at),
    min(created_at),
    max(created_at)
FROM ledger.traffic_settlements
GROUP BY (interval_ends_at AT TIME ZONE 'UTC')::date, user_id, torrent_id;

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_traffic_daily_rollup()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'traffic daily rollups cannot be deleted';
    END IF;
    IF OLD.traffic_day IS DISTINCT FROM NEW.traffic_day
        OR OLD.user_id IS DISTINCT FROM NEW.user_id
        OR OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR OLD.created_at IS DISTINCT FROM NEW.created_at
        OR NEW.raw_uploaded < OLD.raw_uploaded
        OR NEW.raw_downloaded < OLD.raw_downloaded
        OR NEW.credited_uploaded < OLD.credited_uploaded
        OR NEW.charged_downloaded < OLD.charged_downloaded
        OR NEW.settlement_count <= OLD.settlement_count
        OR NEW.first_interval_at > OLD.first_interval_at
        OR NEW.last_interval_at < OLD.last_interval_at
        OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'traffic daily rollup transition is not monotonic';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER traffic_daily_rollups_monotonic
BEFORE UPDATE OR DELETE ON ledger.traffic_daily_rollups
FOR EACH ROW EXECUTE FUNCTION ledger.protect_traffic_daily_rollup();

-- The database owns this projection so an old Settlement binary that is still
-- draining during a rolling deployment cannot insert unaggregated detail.
-- +goose StatementBegin
CREATE FUNCTION ledger.rollup_inserted_traffic_settlement()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO ledger.traffic_daily_rollups (
        traffic_day,
        user_id,
        torrent_id,
        raw_uploaded,
        raw_downloaded,
        credited_uploaded,
        charged_downloaded,
        settlement_count,
        first_interval_at,
        last_interval_at,
        created_at,
        updated_at
    ) VALUES (
        (NEW.interval_ends_at AT TIME ZONE 'UTC')::date,
        NEW.user_id,
        NEW.torrent_id,
        NEW.raw_uploaded,
        NEW.raw_downloaded,
        NEW.credited_uploaded,
        NEW.charged_downloaded,
        1,
        NEW.interval_starts_at,
        NEW.interval_ends_at,
        NEW.created_at,
        NEW.created_at
    )
    ON CONFLICT (traffic_day, user_id, torrent_id) DO UPDATE
    SET
        raw_uploaded = ledger.traffic_daily_rollups.raw_uploaded + EXCLUDED.raw_uploaded,
        raw_downloaded = ledger.traffic_daily_rollups.raw_downloaded + EXCLUDED.raw_downloaded,
        credited_uploaded = ledger.traffic_daily_rollups.credited_uploaded + EXCLUDED.credited_uploaded,
        charged_downloaded = ledger.traffic_daily_rollups.charged_downloaded + EXCLUDED.charged_downloaded,
        settlement_count = ledger.traffic_daily_rollups.settlement_count + 1,
        first_interval_at = least(ledger.traffic_daily_rollups.first_interval_at, EXCLUDED.first_interval_at),
        last_interval_at = greatest(ledger.traffic_daily_rollups.last_interval_at, EXCLUDED.last_interval_at),
        updated_at = greatest(ledger.traffic_daily_rollups.updated_at, EXCLUDED.updated_at);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER traffic_settlements_daily_rollup
AFTER INSERT ON ledger.traffic_settlements
FOR EACH ROW EXECUTE FUNCTION ledger.rollup_inserted_traffic_settlement();

-- Completion assessments already copy every durable H&R fact. Keeping their
-- UUID as a trace reference must not pin the much larger raw interval forever.
ALTER TABLE ledger.hnr_completion_assessments
    DROP CONSTRAINT hnr_completion_assessments_completion_event_id_fkey;

-- Snapshot entry payload is the dominant footprint, but a 30-second cadence
-- also makes immutable inbox/chunk/run metadata grow without bound. Evidence
-- windows already copy the selected and fence coordinates. Retain those
-- referenced headers and the latest header for every route epoch, while
-- allowing old transport detail and redundant headers to age out.
DROP TRIGGER seeding_swarm_snapshot_inbox_immutable
    ON settlement.seeding_swarm_snapshot_inbox;
DROP TRIGGER seeding_swarm_snapshot_chunks_immutable
    ON ledger.seeding_swarm_snapshot_chunks;

-- +goose StatementBegin
CREATE FUNCTION settlement.protect_bounded_seeding_snapshot_inbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    snapshot_observed_at timestamptz;
    snapshot_status text;
    evidence_closed_through timestamptz;
BEGIN
    IF TG_OP <> 'DELETE' THEN
        RAISE EXCEPTION 'seeding swarm snapshot inbox evidence is immutable';
    END IF;
    SELECT snapshot.observed_at, snapshot.status
    INTO snapshot_observed_at, snapshot_status
    FROM ledger.seeding_swarm_snapshots AS snapshot
    WHERE snapshot.snapshot_id = OLD.snapshot_id;
    SELECT max(evidence_window.window_end)
    INTO evidence_closed_through
    FROM ledger.seeding_evidence_windows AS evidence_window;
    IF snapshot_status IS NULL
        OR snapshot_status NOT IN ('collecting', 'complete')
        OR snapshot_observed_at IS NULL
        OR evidence_closed_through IS NULL
        OR snapshot_observed_at >= evidence_closed_through
        OR OLD.received_at >= clock_timestamp() - interval '30 days' THEN
        RAISE EXCEPTION 'active, recent or unclosed seeding snapshot inbox cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_bounded_seeding_snapshot_chunks()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    snapshot_observed_at timestamptz;
    snapshot_status text;
    evidence_closed_through timestamptz;
BEGIN
    IF TG_OP <> 'DELETE' THEN
        RAISE EXCEPTION 'seeding swarm snapshot chunk evidence is immutable';
    END IF;
    SELECT snapshot.observed_at, snapshot.status
    INTO snapshot_observed_at, snapshot_status
    FROM ledger.seeding_swarm_snapshots AS snapshot
    WHERE snapshot.snapshot_id = OLD.snapshot_id;
    SELECT max(evidence_window.window_end)
    INTO evidence_closed_through
    FROM ledger.seeding_evidence_windows AS evidence_window;
    IF snapshot_status IS NULL
        OR snapshot_status NOT IN ('collecting', 'complete')
        OR snapshot_observed_at IS NULL
        OR evidence_closed_through IS NULL
        OR snapshot_observed_at >= evidence_closed_through
        OR snapshot_observed_at >= clock_timestamp() - interval '30 days' THEN
        RAISE EXCEPTION 'active, recent or unclosed seeding snapshot chunks cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER seeding_swarm_snapshot_inbox_retained
BEFORE UPDATE OR DELETE ON settlement.seeding_swarm_snapshot_inbox
FOR EACH ROW EXECUTE FUNCTION settlement.protect_bounded_seeding_snapshot_inbox();
CREATE TRIGGER seeding_swarm_snapshot_chunks_retained
BEFORE UPDATE OR DELETE ON ledger.seeding_swarm_snapshot_chunks
FOR EACH ROW EXECUTE FUNCTION ledger.protect_bounded_seeding_snapshot_chunks();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_seeding_snapshot_run()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.observed_at >= clock_timestamp() - interval '30 days'
            OR OLD.observed_at >= coalesce(
                (SELECT max(evidence_window.window_end) FROM ledger.seeding_evidence_windows AS evidence_window),
                '-infinity'::timestamptz
            )
            OR EXISTS (
                SELECT 1 FROM settlement.seeding_swarm_snapshot_inbox AS inbox
                WHERE inbox.snapshot_id = OLD.snapshot_id
            )
            OR EXISTS (
                SELECT 1 FROM ledger.seeding_swarm_snapshot_chunks AS chunk
                WHERE chunk.snapshot_id = OLD.snapshot_id
            )
            OR EXISTS (
                SELECT 1 FROM ledger.seeding_swarm_snapshot_entries AS entry
                WHERE entry.snapshot_id = OLD.snapshot_id
            )
            OR EXISTS (
                SELECT 1 FROM ledger.seeding_evidence_windows AS evidence_window
                WHERE evidence_window.selected_snapshot_id = OLD.snapshot_id
                   OR evidence_window.snapshot_fence_id = OLD.snapshot_id
            )
            OR NOT EXISTS (
                SELECT 1 FROM ledger.seeding_swarm_snapshots AS newer
                WHERE newer.source_id = OLD.source_id
                  AND newer.routing_epoch = OLD.routing_epoch
                  AND newer.snapshot_sequence > OLD.snapshot_sequence
            ) THEN
            RAISE EXCEPTION 'active, recent, referenced or route-head seeding snapshot cannot be deleted';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.status = 'complete' THEN
        RAISE EXCEPTION 'complete seeding swarm snapshot evidence is immutable';
    END IF;
    IF OLD.snapshot_id IS DISTINCT FROM NEW.snapshot_id
        OR OLD.source_id IS DISTINCT FROM NEW.source_id
        OR OLD.routing_epoch IS DISTINCT FROM NEW.routing_epoch
        OR OLD.snapshot_sequence IS DISTINCT FROM NEW.snapshot_sequence
        OR OLD.observed_at IS DISTINCT FROM NEW.observed_at
        OR OLD.chunk_count IS DISTINCT FROM NEW.chunk_count
        OR OLD.created_at IS DISTINCT FROM NEW.created_at
        OR NEW.received_chunk_count < OLD.received_chunk_count THEN
        RAISE EXCEPTION 'seeding swarm snapshot identity is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_recent_seeding_snapshot_entries()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'DELETE' THEN
        RAISE EXCEPTION 'seeding swarm snapshot entries are immutable';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM ledger.seeding_swarm_snapshots AS snapshot
        WHERE snapshot.snapshot_id = OLD.snapshot_id
          AND snapshot.observed_at >= coalesce(
              (SELECT max(evidence_window.window_end) FROM ledger.seeding_evidence_windows AS evidence_window),
              '-infinity'::timestamptz
          )
    ) OR EXISTS (
        SELECT 1
        FROM ledger.seeding_evidence_windows AS evidence_window
        WHERE evidence_window.selected_snapshot_id = OLD.snapshot_id
          AND (
              evidence_window.window_end >= clock_timestamp() - interval '30 days'
              OR EXISTS (
                  SELECT 1
                  FROM ledger.seeding_evidence_anomalies AS anomaly
                  WHERE anomaly.window_start = evidence_window.window_start
              )
          )
    ) THEN
        RAISE EXCEPTION 'recent or anomalous selected seeding snapshot entries are retained';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- Legacy v1 payload rows are finite after the v2 producer cursor cutover, but
-- an upgraded installation may already contain millions of them. They may be
-- removed only after 30 days; the cleanup query additionally proves that no
-- live session or retained raw interval still names the event.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_terminal_inbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.outcome = 'processing'
            OR OLD.processed_at IS NULL
            OR OLD.processed_at >= clock_timestamp() - interval '30 days'
            OR EXISTS (
                SELECT 1 FROM settlement.ingest_stream_cursors AS cursor
                WHERE cursor.last_event_id = OLD.event_id
            )
            OR EXISTS (
                SELECT 1 FROM settlement.session_states AS state
                WHERE state.last_event_id = OLD.event_id
            )
            OR EXISTS (
                SELECT 1 FROM ledger.raw_session_intervals AS raw
                WHERE raw.event_id = OLD.event_id OR raw.previous_event_id = OLD.event_id
            ) THEN
            RAISE EXCEPTION 'active, recent or referenced Settlement inbox evidence cannot be deleted';
        END IF;
        RETURN OLD;
    END IF;

    IF OLD.outcome <> 'processing' THEN
        RAISE EXCEPTION 'terminal Settlement inbox evidence is immutable';
    END IF;
    IF OLD.event_id IS DISTINCT FROM NEW.event_id
        OR OLD.payload_sha256 IS DISTINCT FROM NEW.payload_sha256
        OR OLD.payload_json IS DISTINCT FROM NEW.payload_json
        OR OLD.source_stream IS DISTINCT FROM NEW.source_stream
        OR OLD.source_subject IS DISTINCT FROM NEW.source_subject
        OR OLD.source_sequence IS DISTINCT FROM NEW.source_sequence
        OR OLD.delivery_count IS DISTINCT FROM NEW.delivery_count
        OR OLD.received_at IS DISTINCT FROM NEW.received_at
        OR OLD.ingested_at IS DISTINCT FROM NEW.ingested_at THEN
        RAISE EXCEPTION 'Settlement inbox event evidence is immutable';
    END IF;
    IF NEW.outcome = 'processing' THEN
        RAISE EXCEPTION 'Settlement inbox finalization must be terminal';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Queue/outbox rows are transport evidence, not the accounting ledger. Permit
-- deletion only after a 72-hour replay window and only in terminal state.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_policy_work()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.settled_at IS NULL
            OR OLD.settled_at >= clock_timestamp() - interval '72 hours' THEN
            RAISE EXCEPTION 'active or recent Settlement policy work cannot be deleted';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.interval_event_id IS DISTINCT FROM NEW.interval_event_id
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'Settlement policy work identity is immutable';
    END IF;
    IF OLD.settled_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'settled policy work is terminal';
    END IF;
    IF NEW.attempts < OLD.attempts THEN
        RAISE EXCEPTION 'Settlement policy work attempts cannot regress';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_traffic_outbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.published_at IS NULL
            OR OLD.published_at >= clock_timestamp() - interval '72 hours' THEN
            RAISE EXCEPTION 'unpublished or recent Settlement traffic outbox cannot be deleted';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.event_id IS DISTINCT FROM NEW.event_id
        OR OLD.settlement_id IS DISTINCT FROM NEW.settlement_id
        OR OLD.event_type IS DISTINCT FROM NEW.event_type
        OR OLD.schema_version IS DISTINCT FROM NEW.schema_version
        OR OLD.occurred_at IS DISTINCT FROM NEW.occurred_at
        OR OLD.payload_json IS DISTINCT FROM NEW.payload_json
        OR OLD.payload_sha256 IS DISTINCT FROM NEW.payload_sha256
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'Settlement traffic outbox event evidence is immutable';
    END IF;
    IF OLD.published_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'published Settlement traffic outbox event is terminal';
    END IF;
    IF NEW.attempts < OLD.attempts THEN
        RAISE EXCEPTION 'Settlement traffic outbox attempts cannot regress';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_hnr_work()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.processed_at IS NULL
            OR OLD.processed_at >= clock_timestamp() - interval '72 hours' THEN
            RAISE EXCEPTION 'active or recent H&R work cannot be deleted';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.interval_event_id IS DISTINCT FROM NEW.interval_event_id
        OR OLD.created_at IS DISTINCT FROM NEW.created_at
        OR NEW.attempts < OLD.attempts THEN
        RAISE EXCEPTION 'H&R work identity is immutable';
    END IF;
    IF OLD.processed_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'processed H&R work is terminal';
    END IF;
    IF NEW.processed_at IS NULL AND NEW.processing_disposition IS NOT NULL THEN
        RAISE EXCEPTION 'H&R work terminal disposition is incomplete';
    END IF;
    IF OLD.processed_at IS NULL
        AND NEW.processed_at IS NOT NULL
        AND NEW.processing_disposition IS NULL THEN
        RAISE EXCEPTION 'H&R work terminal disposition is incomplete';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_hnr_outbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.published_at IS NULL
            OR OLD.published_at >= clock_timestamp() - interval '72 hours' THEN
            RAISE EXCEPTION 'unpublished or recent H&R outbox cannot be deleted';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.event_id IS DISTINCT FROM NEW.event_id
        OR OLD.obligation_id IS DISTINCT FROM NEW.obligation_id
        OR OLD.obligation_version IS DISTINCT FROM NEW.obligation_version
        OR OLD.event_type IS DISTINCT FROM NEW.event_type
        OR OLD.schema_version IS DISTINCT FROM NEW.schema_version
        OR OLD.occurred_at IS DISTINCT FROM NEW.occurred_at
        OR OLD.payload_json IS DISTINCT FROM NEW.payload_json
        OR OLD.payload_sha256 IS DISTINCT FROM NEW.payload_sha256
        OR OLD.created_at IS DISTINCT FROM NEW.created_at
        OR NEW.attempts < OLD.attempts THEN
        RAISE EXCEPTION 'H&R outbox evidence is immutable';
    END IF;
    IF OLD.published_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'published H&R outbox event is terminal';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_seeding_evidence_outbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.published_at IS NULL
            OR OLD.published_at >= clock_timestamp() - interval '72 hours' THEN
            RAISE EXCEPTION 'unpublished or recent seeding evidence outbox cannot be deleted';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.event_id IS DISTINCT FROM NEW.event_id
        OR OLD.window_start IS DISTINCT FROM NEW.window_start
        OR OLD.chunk_index IS DISTINCT FROM NEW.chunk_index
        OR OLD.event_type IS DISTINCT FROM NEW.event_type
        OR OLD.schema_version IS DISTINCT FROM NEW.schema_version
        OR OLD.occurred_at IS DISTINCT FROM NEW.occurred_at
        OR OLD.payload_json IS DISTINCT FROM NEW.payload_json
        OR OLD.payload_sha256 IS DISTINCT FROM NEW.payload_sha256
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'Settlement seeding evidence outbox payload is immutable';
    END IF;
    IF OLD.published_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'published Settlement seeding evidence outbox event is terminal';
    END IF;
    IF NEW.attempts < OLD.attempts THEN
        RAISE EXCEPTION 'Settlement seeding evidence outbox attempts cannot regress';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Detailed settlement rows are removable only after 30 days and only after
-- their transport outbox has already been pruned. The daily rollup is written
-- in the same settlement transaction for all new traffic and backfilled above.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.reject_traffic_settlement_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    interval_end timestamptz;
BEGIN
    IF TG_OP <> 'DELETE' THEN
        RAISE EXCEPTION 'final traffic settlements are immutable';
    END IF;
    IF TG_TABLE_NAME = 'traffic_settlements' THEN
        interval_end := OLD.interval_ends_at;
        IF EXISTS (
            SELECT 1 FROM settlement.traffic_outbox
            WHERE settlement_id = OLD.settlement_id
        ) THEN
            RAISE EXCEPTION 'traffic settlement transport evidence still exists';
        END IF;
    ELSE
        SELECT settlement.interval_ends_at INTO interval_end
        FROM ledger.traffic_settlements AS settlement
        WHERE settlement.settlement_id = OLD.settlement_id;
    END IF;
    IF interval_end IS NULL
        OR interval_end >= clock_timestamp() - interval '30 days' THEN
        RAISE EXCEPTION 'recent traffic settlement detail cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- Bounded evidence detail. Aggregate window/items, daily traffic totals and
-- H&R assessments remain; only reconstructable source links and observations
-- age out.
DROP TRIGGER seeding_evidence_sources_immutable ON ledger.seeding_evidence_sources;
DROP TRIGGER seeding_evidence_anomalies_immutable ON ledger.seeding_evidence_anomalies;

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_bounded_seeding_detail()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'DELETE' THEN
        RAISE EXCEPTION 'seeding evidence detail is immutable';
    END IF;
    IF TG_TABLE_NAME = 'seeding_evidence_sources'
        AND (
            OLD.window_start >= clock_timestamp() - interval '30 days'
            OR EXISTS (
                SELECT 1
                FROM ledger.seeding_evidence_anomalies AS anomaly
                WHERE anomaly.window_start = OLD.window_start
            )
        ) THEN
        RAISE EXCEPTION 'recent or anomalous seeding evidence sources cannot be deleted';
    END IF;
    IF TG_TABLE_NAME = 'seeding_evidence_anomalies'
        AND OLD.detected_at >= clock_timestamp() - interval '180 days' THEN
        RAISE EXCEPTION 'recent seeding evidence anomalies cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER seeding_evidence_sources_retained
BEFORE UPDATE OR DELETE ON ledger.seeding_evidence_sources
FOR EACH ROW EXECUTE FUNCTION ledger.protect_bounded_seeding_detail();

CREATE TRIGGER seeding_evidence_anomalies_retained
BEFORE UPDATE OR DELETE ON ledger.seeding_evidence_anomalies
FOR EACH ROW EXECUTE FUNCTION ledger.protect_bounded_seeding_detail();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.reject_speed_observation_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'DELETE'
        OR OLD.observed_at >= clock_timestamp() - interval '180 days' THEN
        RAISE EXCEPTION 'recent speed observations are immutable';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- Raw intervals may leave only after all foreign-key detail has been pruned.
-- Active H&R obligations additionally pin their post-completion intervals so
-- incremental evidence can never disappear mid-assessment.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.reject_raw_interval_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'DELETE'
        OR OLD.ends_at >= clock_timestamp() - interval '30 days'
        OR EXISTS (
            SELECT 1
            FROM ledger.hnr_obligations AS obligation
            INNER JOIN ledger.hnr_completion_assessments AS assessment
                ON assessment.id = obligation.assessment_id
            WHERE obligation.state = 'tracking'
              AND assessment.user_id = OLD.user_id
              AND assessment.torrent_id = OLD.torrent_id
              AND OLD.ends_at > assessment.completed_at
        ) THEN
        RAISE EXCEPTION 'raw Tracker interval is immutable or still required';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- Session baselines are ephemeral peer state. A client returning after two
-- days safely creates a new baseline instead of pinning an event identity.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_session_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.updated_at >= clock_timestamp() - interval '48 hours' THEN
            RAISE EXCEPTION 'recent Settlement session state cannot be deleted';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.user_id IS DISTINCT FROM NEW.user_id
        OR OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR OLD.session_token IS DISTINCT FROM NEW.session_token
        OR OLD.info_hash_v1 IS DISTINCT FROM NEW.info_hash_v1
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'Settlement session identity is immutable';
    END IF;
    IF NEW.version <> OLD.version + 1
        OR NEW.last_received_at <= OLD.last_received_at
        OR NEW.updated_at < OLD.updated_at
        OR NEW.session_epoch NOT IN (OLD.session_epoch, OLD.session_epoch + 1) THEN
        RAISE EXCEPTION 'Settlement session transition is not monotonic';
    END IF;
    IF NEW.session_epoch = OLD.session_epoch
        AND (NEW.last_uploaded < OLD.last_uploaded OR NEW.last_downloaded < OLD.last_downloaded) THEN
        RAISE EXCEPTION 'Settlement counters can regress only in a new epoch';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down

-- A downgrade cannot safely restore permanent detail after cleanup has run.
-- Permit it only while every detailed settlement is still present.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM ledger.traffic_daily_rollups AS rollup
        WHERE rollup.settlement_count <> (
            SELECT count(*)
            FROM ledger.traffic_settlements AS settlement
            WHERE settlement.user_id = rollup.user_id
              AND settlement.torrent_id = rollup.torrent_id
              AND (settlement.interval_ends_at AT TIME ZONE 'UTC')::date = rollup.traffic_day
        )
    ) THEN
        RAISE EXCEPTION 'cannot downgrade after bounded storage cleanup';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM ledger.hnr_completion_assessments AS assessment
        LEFT JOIN ledger.raw_session_intervals AS raw
            ON raw.event_id = assessment.completion_event_id
        WHERE raw.event_id IS NULL
    ) THEN
        RAISE EXCEPTION 'cannot downgrade after H&R source interval cleanup';
    END IF;
END;
$$;
-- +goose StatementEnd

-- Restore the strict legacy mutation guards. Development rollbacks use empty
-- or unpruned schemas, so no retained fact is fabricated here.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_recent_seeding_snapshot_entries()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'DELETE' THEN
        RAISE EXCEPTION 'seeding swarm snapshot entries are immutable';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM ledger.seeding_evidence_windows AS evidence_window
        WHERE evidence_window.selected_snapshot_id = OLD.snapshot_id
          AND evidence_window.window_end >= clock_timestamp() - interval '30 days'
    ) THEN
        RAISE EXCEPTION 'recent selected seeding snapshot entries are retained';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_terminal_inbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'Settlement inbox evidence cannot be deleted';
    END IF;
    IF OLD.outcome <> 'processing' THEN
        RAISE EXCEPTION 'terminal Settlement inbox evidence is immutable';
    END IF;
    IF OLD.event_id IS DISTINCT FROM NEW.event_id
        OR OLD.payload_sha256 IS DISTINCT FROM NEW.payload_sha256
        OR OLD.payload_json IS DISTINCT FROM NEW.payload_json
        OR OLD.source_stream IS DISTINCT FROM NEW.source_stream
        OR OLD.source_subject IS DISTINCT FROM NEW.source_subject
        OR OLD.source_sequence IS DISTINCT FROM NEW.source_sequence
        OR OLD.delivery_count IS DISTINCT FROM NEW.delivery_count
        OR OLD.received_at IS DISTINCT FROM NEW.received_at
        OR OLD.ingested_at IS DISTINCT FROM NEW.ingested_at THEN
        RAISE EXCEPTION 'Settlement inbox event evidence is immutable';
    END IF;
    IF NEW.outcome = 'processing' THEN
        RAISE EXCEPTION 'Settlement inbox finalization must be terminal';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_session_transition()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'Settlement session state cannot be deleted'; END IF;
    IF OLD.user_id IS DISTINCT FROM NEW.user_id OR OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR OLD.session_token IS DISTINCT FROM NEW.session_token OR OLD.info_hash_v1 IS DISTINCT FROM NEW.info_hash_v1
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN RAISE EXCEPTION 'Settlement session identity is immutable'; END IF;
    IF NEW.version <> OLD.version + 1 OR NEW.last_received_at <= OLD.last_received_at OR NEW.updated_at < OLD.updated_at
        OR NEW.session_epoch NOT IN (OLD.session_epoch, OLD.session_epoch + 1) THEN RAISE EXCEPTION 'Settlement session transition is not monotonic'; END IF;
    IF NEW.session_epoch = OLD.session_epoch AND (NEW.last_uploaded < OLD.last_uploaded OR NEW.last_downloaded < OLD.last_downloaded)
        THEN RAISE EXCEPTION 'Settlement counters can regress only in a new epoch'; END IF;
    RETURN NEW;
END; $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_seeding_snapshot_run()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' OR OLD.status = 'complete' THEN
        RAISE EXCEPTION 'complete seeding swarm snapshot evidence is immutable';
    END IF;
    IF OLD.snapshot_id IS DISTINCT FROM NEW.snapshot_id
        OR OLD.source_id IS DISTINCT FROM NEW.source_id
        OR OLD.routing_epoch IS DISTINCT FROM NEW.routing_epoch
        OR OLD.snapshot_sequence IS DISTINCT FROM NEW.snapshot_sequence
        OR OLD.observed_at IS DISTINCT FROM NEW.observed_at
        OR OLD.chunk_count IS DISTINCT FROM NEW.chunk_count
        OR OLD.created_at IS DISTINCT FROM NEW.created_at
        OR NEW.received_chunk_count < OLD.received_chunk_count THEN
        RAISE EXCEPTION 'seeding swarm snapshot identity is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER seeding_swarm_snapshot_chunks_retained
    ON ledger.seeding_swarm_snapshot_chunks;
DROP TRIGGER seeding_swarm_snapshot_inbox_retained
    ON settlement.seeding_swarm_snapshot_inbox;
DROP FUNCTION ledger.protect_bounded_seeding_snapshot_chunks();
DROP FUNCTION settlement.protect_bounded_seeding_snapshot_inbox();
CREATE TRIGGER seeding_swarm_snapshot_inbox_immutable
BEFORE UPDATE OR DELETE ON settlement.seeding_swarm_snapshot_inbox
FOR EACH ROW EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();
CREATE TRIGGER seeding_swarm_snapshot_chunks_immutable
BEFORE UPDATE OR DELETE ON ledger.seeding_swarm_snapshot_chunks
FOR EACH ROW EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.reject_raw_interval_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'raw Tracker ledger intervals are immutable'; END; $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.reject_speed_observation_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'speed observations are immutable'; END; $$;
-- +goose StatementEnd

DROP TRIGGER seeding_evidence_anomalies_retained ON ledger.seeding_evidence_anomalies;
DROP TRIGGER seeding_evidence_sources_retained ON ledger.seeding_evidence_sources;
DROP FUNCTION ledger.protect_bounded_seeding_detail();
CREATE TRIGGER seeding_evidence_sources_immutable BEFORE UPDATE OR DELETE ON ledger.seeding_evidence_sources
FOR EACH ROW EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();
CREATE TRIGGER seeding_evidence_anomalies_immutable BEFORE UPDATE OR DELETE ON ledger.seeding_evidence_anomalies
FOR EACH ROW EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.reject_traffic_settlement_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'final traffic settlements are immutable'; END; $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_policy_work()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'Settlement policy work evidence cannot be deleted'; END IF;
    IF OLD.interval_event_id IS DISTINCT FROM NEW.interval_event_id OR OLD.created_at IS DISTINCT FROM NEW.created_at
        THEN RAISE EXCEPTION 'Settlement policy work identity is immutable'; END IF;
    IF OLD.settled_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN RAISE EXCEPTION 'settled policy work is terminal'; END IF;
    IF NEW.attempts < OLD.attempts THEN RAISE EXCEPTION 'Settlement policy work attempts cannot regress'; END IF;
    RETURN NEW;
END; $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_traffic_outbox()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'Settlement traffic outbox evidence cannot be deleted'; END IF;
    IF OLD.event_id IS DISTINCT FROM NEW.event_id OR OLD.settlement_id IS DISTINCT FROM NEW.settlement_id
        OR OLD.event_type IS DISTINCT FROM NEW.event_type OR OLD.schema_version IS DISTINCT FROM NEW.schema_version
        OR OLD.occurred_at IS DISTINCT FROM NEW.occurred_at OR OLD.payload_json IS DISTINCT FROM NEW.payload_json
        OR OLD.payload_sha256 IS DISTINCT FROM NEW.payload_sha256 OR OLD.created_at IS DISTINCT FROM NEW.created_at
        THEN RAISE EXCEPTION 'Settlement traffic outbox event evidence is immutable'; END IF;
    IF OLD.published_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN RAISE EXCEPTION 'published Settlement traffic outbox event is terminal'; END IF;
    IF NEW.attempts < OLD.attempts THEN RAISE EXCEPTION 'Settlement traffic outbox attempts cannot regress'; END IF;
    RETURN NEW;
END; $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_hnr_work()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'H&R work evidence cannot be deleted'; END IF;
    IF OLD.interval_event_id IS DISTINCT FROM NEW.interval_event_id OR OLD.created_at IS DISTINCT FROM NEW.created_at
        OR NEW.attempts < OLD.attempts THEN RAISE EXCEPTION 'H&R work identity is immutable'; END IF;
    IF OLD.processed_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN RAISE EXCEPTION 'processed H&R work is terminal'; END IF;
    IF NEW.processed_at IS NULL AND NEW.processing_disposition IS NOT NULL THEN RAISE EXCEPTION 'H&R work terminal disposition is incomplete'; END IF;
    IF OLD.processed_at IS NULL AND NEW.processed_at IS NOT NULL AND NEW.processing_disposition IS NULL
        THEN RAISE EXCEPTION 'H&R work terminal disposition is incomplete'; END IF;
    RETURN NEW;
END; $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_hnr_outbox()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'H&R outbox evidence cannot be deleted'; END IF;
    IF OLD.event_id IS DISTINCT FROM NEW.event_id OR OLD.obligation_id IS DISTINCT FROM NEW.obligation_id
        OR OLD.obligation_version IS DISTINCT FROM NEW.obligation_version OR OLD.event_type IS DISTINCT FROM NEW.event_type
        OR OLD.schema_version IS DISTINCT FROM NEW.schema_version OR OLD.occurred_at IS DISTINCT FROM NEW.occurred_at
        OR OLD.payload_json IS DISTINCT FROM NEW.payload_json OR OLD.payload_sha256 IS DISTINCT FROM NEW.payload_sha256
        OR OLD.created_at IS DISTINCT FROM NEW.created_at OR NEW.attempts < OLD.attempts
        THEN RAISE EXCEPTION 'H&R outbox evidence is immutable'; END IF;
    IF OLD.published_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN RAISE EXCEPTION 'published H&R outbox event is terminal'; END IF;
    RETURN NEW;
END; $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_seeding_evidence_outbox()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'Settlement seeding evidence outbox cannot be deleted'; END IF;
    IF OLD.event_id IS DISTINCT FROM NEW.event_id OR OLD.window_start IS DISTINCT FROM NEW.window_start
        OR OLD.chunk_index IS DISTINCT FROM NEW.chunk_index OR OLD.event_type IS DISTINCT FROM NEW.event_type
        OR OLD.schema_version IS DISTINCT FROM NEW.schema_version OR OLD.occurred_at IS DISTINCT FROM NEW.occurred_at
        OR OLD.payload_json IS DISTINCT FROM NEW.payload_json OR OLD.payload_sha256 IS DISTINCT FROM NEW.payload_sha256
        OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN RAISE EXCEPTION 'Settlement seeding evidence outbox payload is immutable'; END IF;
    IF OLD.published_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN RAISE EXCEPTION 'published Settlement seeding evidence outbox event is terminal'; END IF;
    IF NEW.attempts < OLD.attempts THEN RAISE EXCEPTION 'Settlement seeding evidence outbox attempts cannot regress'; END IF;
    RETURN NEW;
END; $$;
-- +goose StatementEnd

ALTER TABLE ledger.hnr_completion_assessments
    ADD CONSTRAINT hnr_completion_assessments_completion_event_id_fkey
        FOREIGN KEY (completion_event_id) REFERENCES ledger.raw_session_intervals (event_id) ON DELETE RESTRICT;

ALTER TABLE ledger.speed_observations RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE ledger.seeding_evidence_anomalies RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE ledger.seeding_evidence_sources RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE ledger.seeding_swarm_snapshot_entries RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE ledger.seeding_swarm_snapshot_chunks RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE ledger.seeding_swarm_snapshots RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE ledger.traffic_settlement_segments RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE ledger.traffic_settlements RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE ledger.raw_session_intervals RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE settlement.seeding_evidence_outbox RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE settlement.seeding_swarm_snapshot_inbox RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE settlement.hnr_outbox RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE settlement.hnr_work RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE settlement.traffic_outbox RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE settlement.policy_work RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE settlement.session_states RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);
ALTER TABLE settlement.event_inbox RESET (
    autovacuum_vacuum_scale_factor, autovacuum_vacuum_threshold,
    autovacuum_analyze_scale_factor, autovacuum_analyze_threshold
);

DROP INDEX ledger.seeding_evidence_anomalies_retention_idx;
DROP INDEX ledger.seeding_evidence_anomalies_interval_retention_idx;
DROP INDEX ledger.seeding_evidence_sources_interval_retention_idx;
DROP INDEX ledger.speed_observations_retention_idx;
DROP INDEX ledger.seeding_evidence_windows_end_retention_idx;
DROP INDEX ledger.seeding_evidence_windows_fence_snapshot_retention_idx;
DROP INDEX ledger.seeding_evidence_windows_selected_snapshot_retention_idx;
DROP INDEX ledger.seeding_swarm_snapshots_retention_idx;
DROP INDEX ledger.raw_session_intervals_retention_idx;
DROP INDEX ledger.raw_session_intervals_previous_event_retention_idx;
DROP INDEX ledger.traffic_settlements_retention_idx;
DROP INDEX settlement.seeding_swarm_snapshot_inbox_retention_idx;
DROP INDEX settlement.seeding_swarm_snapshot_inbox_snapshot_retention_idx;
DROP INDEX settlement.seeding_evidence_outbox_retention_idx;
DROP INDEX settlement.hnr_outbox_retention_idx;
DROP INDEX settlement.hnr_work_retention_idx;
DROP INDEX settlement.traffic_outbox_retention_idx;
DROP INDEX settlement.policy_work_retention_idx;
DROP INDEX settlement.session_states_retention_idx;
DROP INDEX settlement.event_inbox_retention_idx;

DROP TRIGGER traffic_daily_rollups_monotonic ON ledger.traffic_daily_rollups;
DROP TRIGGER traffic_settlements_daily_rollup ON ledger.traffic_settlements;
DROP FUNCTION ledger.rollup_inserted_traffic_settlement();
DROP FUNCTION ledger.protect_traffic_daily_rollup();
DROP INDEX ledger.traffic_daily_rollups_user_day_idx;
DROP TABLE ledger.traffic_daily_rollups;
