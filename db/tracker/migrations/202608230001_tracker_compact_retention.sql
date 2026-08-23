-- +goose Up

-- Tracker hot/replay state is bounded independently from compact accounting
-- facts. Totals, daily rollups, evidence windows, H&R assessments and user
-- obligations remain durable; only reconstructable transport/detail ages out.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_terminal_inbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.outcome = 'processing'
            OR OLD.processed_at IS NULL
            OR OLD.processed_at >= clock_timestamp() - interval '12 hours'
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_policy_work()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.settled_at IS NULL
            OR OLD.settled_at >= clock_timestamp() - interval '3 hours' THEN
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
            OR OLD.published_at >= clock_timestamp() - interval '3 hours' THEN
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
            OR OLD.processed_at >= clock_timestamp() - interval '3 hours' THEN
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
            OR OLD.published_at >= clock_timestamp() - interval '3 hours' THEN
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
            OR OLD.published_at >= clock_timestamp() - interval '3 hours' THEN
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
        OR interval_end >= clock_timestamp() - interval '12 hours' THEN
        RAISE EXCEPTION 'recent traffic settlement detail cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_bounded_seeding_detail()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'DELETE' THEN
        RAISE EXCEPTION 'seeding evidence detail is immutable';
    END IF;
    IF TG_TABLE_NAME = 'seeding_evidence_sources'
        AND (
            OLD.window_start >= clock_timestamp() - interval '12 hours'
            OR EXISTS (
                SELECT 1
                FROM ledger.seeding_evidence_anomalies AS anomaly
                WHERE anomaly.window_start = OLD.window_start
            )
        ) THEN
        RAISE EXCEPTION 'recent or anomalous seeding evidence sources cannot be deleted';
    END IF;
    IF TG_TABLE_NAME = 'seeding_evidence_anomalies'
        AND OLD.detected_at >= clock_timestamp() - interval '30 days' THEN
        RAISE EXCEPTION 'recent seeding evidence anomalies cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.reject_speed_observation_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'DELETE'
        OR (
            OLD.outcome = 'exceeded'
            AND OLD.observed_at >= clock_timestamp() - interval '30 days'
        )
        OR (
            OLD.outcome <> 'exceeded'
            AND OLD.observed_at >= clock_timestamp() - interval '12 hours'
        ) THEN
        RAISE EXCEPTION 'recent speed observations are immutable';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.reject_raw_interval_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'DELETE'
        OR OLD.ends_at >= clock_timestamp() - interval '12 hours'
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_session_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.updated_at >= clock_timestamp() - interval '12 hours' THEN
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_bounded_seeding_snapshot_inbox()
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
        OR OLD.received_at >= clock_timestamp() - interval '12 hours' THEN
        RAISE EXCEPTION 'active, recent or unclosed seeding snapshot inbox cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_bounded_seeding_snapshot_chunks()
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
        OR snapshot_observed_at >= clock_timestamp() - interval '12 hours' THEN
        RAISE EXCEPTION 'active, recent or unclosed seeding snapshot chunks cannot be deleted';
    END IF;
    RETURN OLD;
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
              evidence_window.window_end >= clock_timestamp() - interval '12 hours'
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_seeding_snapshot_run()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.observed_at >= clock_timestamp() - interval '12 hours'
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

-- +goose Down

-- Cleanup is intentionally irreversible once short-lived detail has aged out.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION '202608230001 is irreversible after compact Tracker cleanup';
END;
$$;
-- +goose StatementEnd
