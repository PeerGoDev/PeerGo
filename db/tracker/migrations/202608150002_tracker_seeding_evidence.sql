-- +goose Up

-- Settlement keeps its own immutable copy of complete Tracker swarm
-- snapshots. Core's swarm projection is intentionally lossy/current-state;
-- hourly reward evidence needs the exact historical snapshot that closed a
-- window and therefore cannot read Core's private database or Redis peers.
CREATE TABLE settlement.seeding_swarm_snapshot_inbox (
    event_id uuid PRIMARY KEY,
    snapshot_id uuid NOT NULL,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    source_stream text NOT NULL,
    source_subject text NOT NULL,
    source_sequence bigint NOT NULL CHECK (source_sequence > 0),
    delivery_count bigint NOT NULL CHECK (delivery_count > 0),
    observed_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL,
    UNIQUE (source_stream, source_sequence)
);

CREATE TABLE ledger.seeding_swarm_snapshots (
    snapshot_id uuid PRIMARY KEY,
    source_id text NOT NULL CHECK (source_id ~ '^[a-z][a-z0-9-]{0,62}$'),
    routing_epoch bigint NOT NULL CHECK (routing_epoch > 0),
    snapshot_sequence bigint NOT NULL CHECK (snapshot_sequence > 0),
    observed_at timestamptz NOT NULL,
    chunk_count integer NOT NULL CHECK (chunk_count BETWEEN 1 AND 10000),
    received_chunk_count integer NOT NULL DEFAULT 0
        CHECK (received_chunk_count BETWEEN 0 AND chunk_count),
    status text NOT NULL DEFAULT 'collecting'
        CHECK (status IN ('collecting', 'complete')),
    created_at timestamptz NOT NULL,
    completed_at timestamptz,
    UNIQUE (source_id, routing_epoch, snapshot_sequence),
    CHECK ((status = 'complete') = (completed_at IS NOT NULL)),
    CHECK (status <> 'complete' OR received_chunk_count = chunk_count)
);

CREATE INDEX seeding_swarm_snapshots_closure_idx
    ON ledger.seeding_swarm_snapshots (observed_at, routing_epoch, snapshot_sequence)
    WHERE status = 'complete';

CREATE TABLE ledger.seeding_swarm_snapshot_chunks (
    snapshot_id uuid NOT NULL
        REFERENCES ledger.seeding_swarm_snapshots (snapshot_id) ON DELETE RESTRICT,
    chunk_index integer NOT NULL CHECK (chunk_index >= 0),
    event_id uuid NOT NULL UNIQUE
        REFERENCES settlement.seeding_swarm_snapshot_inbox (event_id) ON DELETE RESTRICT,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    PRIMARY KEY (snapshot_id, chunk_index)
);

CREATE TABLE ledger.seeding_swarm_snapshot_entries (
    snapshot_id uuid NOT NULL
        REFERENCES ledger.seeding_swarm_snapshots (snapshot_id) ON DELETE RESTRICT,
    info_hash_v1 bytea NOT NULL CHECK (octet_length(info_hash_v1) = 20),
    seeders integer NOT NULL CHECK (seeders >= 0),
    leechers integer NOT NULL CHECK (leechers >= 0),
    PRIMARY KEY (snapshot_id, info_hash_v1)
);

-- One row means the UTC hour is closed. There is deliberately no mutable
-- "building" state: the builder computes under one transaction and publishes
-- the final window, items and sources atomically.
CREATE TABLE ledger.seeding_evidence_windows (
    window_start timestamptz PRIMARY KEY,
    window_end timestamptz NOT NULL,
    schema_version text NOT NULL CHECK (schema_version = 'seeding.evidence.v1'),
    announce_source_stream text NOT NULL,
    announce_fence_sequence bigint NOT NULL CHECK (announce_fence_sequence > 0),
    announce_fence_received_at timestamptz NOT NULL,
    selected_snapshot_id uuid NOT NULL
        REFERENCES ledger.seeding_swarm_snapshots (snapshot_id) ON DELETE RESTRICT,
    selected_snapshot_sequence bigint NOT NULL CHECK (selected_snapshot_sequence > 0),
    selected_snapshot_observed_at timestamptz NOT NULL,
    snapshot_fence_id uuid NOT NULL
        REFERENCES ledger.seeding_swarm_snapshots (snapshot_id) ON DELETE RESTRICT,
    snapshot_fence_sequence bigint NOT NULL CHECK (snapshot_fence_sequence > 0),
    snapshot_fence_observed_at timestamptz NOT NULL,
    item_count integer NOT NULL CHECK (item_count >= 0),
    evidence_sha256 bytea NOT NULL CHECK (octet_length(evidence_sha256) = 32),
    built_at timestamptz NOT NULL,
    CHECK (window_start = date_trunc('hour', window_start)),
    CHECK (window_end = window_start + interval '1 hour'),
    CHECK (selected_snapshot_observed_at <= window_end),
    CHECK (snapshot_fence_observed_at >= window_end),
    CHECK (snapshot_fence_sequence >= selected_snapshot_sequence),
    CHECK (announce_fence_received_at >= window_end),
    CHECK (built_at >= window_end)
);

CREATE TABLE ledger.seeding_evidence_items (
    window_start timestamptz NOT NULL
        REFERENCES ledger.seeding_evidence_windows (window_start) ON DELETE RESTRICT,
    user_id uuid NOT NULL,
    torrent_id bigint NOT NULL CHECK (torrent_id > 0),
    info_hash_v1 bytea NOT NULL CHECK (octet_length(info_hash_v1) = 20),
    active_seconds bigint NOT NULL CHECK (active_seconds BETWEEN 0 AND 3600),
    raw_uploaded bigint NOT NULL CHECK (raw_uploaded >= 0),
    source_interval_count integer NOT NULL CHECK (source_interval_count > 0),
    first_active_at timestamptz NOT NULL,
    last_active_at timestamptz NOT NULL,
    snapshot_seeders integer NOT NULL CHECK (snapshot_seeders >= 0),
    snapshot_leechers integer NOT NULL CHECK (snapshot_leechers >= 0),
    evidence_sha256 bytea NOT NULL CHECK (octet_length(evidence_sha256) = 32),
    PRIMARY KEY (window_start, user_id, torrent_id),
    CHECK (last_active_at > first_active_at)
);

CREATE INDEX seeding_evidence_items_user_time_idx
    ON ledger.seeding_evidence_items (user_id, window_start DESC, torrent_id);

-- Source links retain every clipped seeding interval. active_seconds on the
-- item is the union of these ranges, so simultaneous clients cannot multiply
-- one user's hourly reward by double-counting overlapping time.
CREATE TABLE ledger.seeding_evidence_sources (
    window_start timestamptz NOT NULL,
    user_id uuid NOT NULL,
    torrent_id bigint NOT NULL,
    interval_event_id uuid NOT NULL
        REFERENCES ledger.raw_session_intervals (event_id) ON DELETE RESTRICT,
    source_sequence bigint NOT NULL CHECK (source_sequence > 0),
    clipped_starts_at timestamptz NOT NULL,
    clipped_ends_at timestamptz NOT NULL,
    PRIMARY KEY (window_start, user_id, torrent_id, interval_event_id),
    FOREIGN KEY (window_start, user_id, torrent_id)
        REFERENCES ledger.seeding_evidence_items (window_start, user_id, torrent_id)
        ON DELETE RESTRICT,
    CHECK (clipped_ends_at > clipped_starts_at)
);

-- Evidence never mutates after closure. A delayed announce is retained as an
-- explicit anomaly and blocks the later reward stage instead of silently
-- rewriting or double-paying an already closed hour.
CREATE TABLE ledger.seeding_evidence_anomalies (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    window_start timestamptz NOT NULL
        REFERENCES ledger.seeding_evidence_windows (window_start) ON DELETE RESTRICT,
    reason_code text NOT NULL CHECK (reason_code IN ('late_announce_interval')),
    interval_event_id uuid NOT NULL
        REFERENCES ledger.raw_session_intervals (event_id) ON DELETE RESTRICT,
    source_sequence bigint NOT NULL CHECK (source_sequence > 0),
    detected_at timestamptz NOT NULL,
    UNIQUE (window_start, interval_event_id)
);

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_seeding_snapshot_run()
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

CREATE TRIGGER seeding_swarm_snapshot_run_guard
BEFORE UPDATE OR DELETE ON ledger.seeding_swarm_snapshots
FOR EACH ROW EXECUTE FUNCTION ledger.protect_seeding_snapshot_run();

-- +goose StatementBegin
CREATE FUNCTION ledger.reject_seeding_evidence_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'closed seeding evidence is immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER seeding_swarm_snapshot_inbox_immutable
BEFORE UPDATE OR DELETE ON settlement.seeding_swarm_snapshot_inbox
FOR EACH ROW EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();
CREATE TRIGGER seeding_swarm_snapshot_chunks_immutable
BEFORE UPDATE OR DELETE ON ledger.seeding_swarm_snapshot_chunks
FOR EACH ROW EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();
CREATE TRIGGER seeding_swarm_snapshot_entries_immutable
BEFORE UPDATE OR DELETE ON ledger.seeding_swarm_snapshot_entries
FOR EACH ROW EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();
CREATE TRIGGER seeding_evidence_windows_immutable
BEFORE UPDATE OR DELETE ON ledger.seeding_evidence_windows
FOR EACH ROW EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();
CREATE TRIGGER seeding_evidence_items_immutable
BEFORE UPDATE OR DELETE ON ledger.seeding_evidence_items
FOR EACH ROW EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();
CREATE TRIGGER seeding_evidence_sources_immutable
BEFORE UPDATE OR DELETE ON ledger.seeding_evidence_sources
FOR EACH ROW EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();
CREATE TRIGGER seeding_evidence_anomalies_immutable
BEFORE UPDATE OR DELETE ON ledger.seeding_evidence_anomalies
FOR EACH ROW EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();

-- +goose StatementBegin
CREATE FUNCTION ledger.record_late_seeding_interval()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO ledger.seeding_evidence_anomalies (
        window_start,
        reason_code,
        interval_event_id,
        source_sequence,
        detected_at
    )
    SELECT
        evidence_window.window_start,
        'late_announce_interval',
        NEW.event_id,
        inbox.source_sequence,
        clock_timestamp()
    FROM settlement.event_inbox AS inbox
    INNER JOIN ledger.seeding_evidence_windows AS evidence_window
      ON NEW.starts_at < evidence_window.window_end
     AND NEW.ends_at > evidence_window.window_start
     AND inbox.source_stream = evidence_window.announce_source_stream
     AND inbox.source_sequence > evidence_window.announce_fence_sequence
    WHERE inbox.event_id = NEW.event_id
    ON CONFLICT (window_start, interval_event_id) DO NOTHING;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER raw_session_interval_seeding_evidence_anomaly
AFTER INSERT ON ledger.raw_session_intervals
FOR EACH ROW EXECUTE FUNCTION ledger.record_late_seeding_interval();

-- +goose Down

DROP TRIGGER raw_session_interval_seeding_evidence_anomaly ON ledger.raw_session_intervals;
DROP FUNCTION ledger.record_late_seeding_interval();
DROP TRIGGER seeding_evidence_anomalies_immutable ON ledger.seeding_evidence_anomalies;
DROP TRIGGER seeding_evidence_sources_immutable ON ledger.seeding_evidence_sources;
DROP TRIGGER seeding_evidence_items_immutable ON ledger.seeding_evidence_items;
DROP TRIGGER seeding_evidence_windows_immutable ON ledger.seeding_evidence_windows;
DROP TRIGGER seeding_swarm_snapshot_entries_immutable ON ledger.seeding_swarm_snapshot_entries;
DROP TRIGGER seeding_swarm_snapshot_chunks_immutable ON ledger.seeding_swarm_snapshot_chunks;
DROP TRIGGER seeding_swarm_snapshot_inbox_immutable ON settlement.seeding_swarm_snapshot_inbox;
DROP FUNCTION ledger.reject_seeding_evidence_mutation();
DROP TRIGGER seeding_swarm_snapshot_run_guard ON ledger.seeding_swarm_snapshots;
DROP FUNCTION ledger.protect_seeding_snapshot_run();
DROP TABLE ledger.seeding_evidence_anomalies;
DROP TABLE ledger.seeding_evidence_sources;
DROP INDEX ledger.seeding_evidence_items_user_time_idx;
DROP TABLE ledger.seeding_evidence_items;
DROP TABLE ledger.seeding_evidence_windows;
DROP TABLE ledger.seeding_swarm_snapshot_entries;
DROP TABLE ledger.seeding_swarm_snapshot_chunks;
DROP INDEX ledger.seeding_swarm_snapshots_closure_idx;
DROP TABLE ledger.seeding_swarm_snapshots;
DROP TABLE settlement.seeding_swarm_snapshot_inbox;
