-- +goose Up

-- The stream cursor proves that Settlement has committed one contiguous
-- transport prefix. It replaces count(*) over an ever-growing inbox as the
-- announce watermark used by evidence and repair tooling.
CREATE TABLE settlement.ingest_stream_cursors (
    source_stream text PRIMARY KEY CHECK (
        char_length(source_stream) BETWEEN 1 AND 255
        AND source_stream !~ '[.*/\\[:space:]]'
    ),
    source_subject text NOT NULL CHECK (
        char_length(source_subject) BETWEEN 1 AND 255
        AND source_subject !~ '[*>[:space:]]'
        AND source_subject !~ '(^\\.|\\.$|\\.\\.)'
    ),
    last_source_sequence bigint NOT NULL CHECK (last_source_sequence > 0),
    last_event_id uuid NOT NULL,
    last_payload_sha256 bytea NOT NULL CHECK (octet_length(last_payload_sha256) = 32),
    last_outcome text NOT NULL CHECK (last_outcome IN (
        'baseline',
        'interval',
        'counter_reset',
        'out_of_order',
        'reopened_baseline',
        'duplicate'
    )),
    last_received_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

-- Existing v1 deployments already have one immutable row per stream
-- sequence. Refuse to manufacture a high-water mark when that history is not
-- a gap-free prefix; an operator must reconcile the source first.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM settlement.event_inbox
        GROUP BY source_stream
        HAVING min(source_sequence) <> 1
            OR count(*) <> max(source_sequence)
            OR count(*) FILTER (WHERE outcome = 'processing') <> 0
    ) THEN
        RAISE EXCEPTION 'cannot bootstrap bounded ingest from a non-terminal or gapped v1 inbox';
    END IF;
END;
$$;
-- +goose StatementEnd

INSERT INTO settlement.ingest_stream_cursors (
    source_stream,
    source_subject,
    last_source_sequence,
    last_event_id,
    last_payload_sha256,
    last_outcome,
    last_received_at,
    updated_at
)
SELECT DISTINCT ON (source_stream)
    source_stream,
    source_subject,
    source_sequence,
    event_id,
    payload_sha256,
    outcome,
    received_at,
    coalesce(processed_at, ingested_at)
FROM settlement.event_inbox
ORDER BY source_stream, source_sequence DESC;

-- One small row per Tracker process epoch is the durable business-level
-- idempotency fence. Announce volume no longer requires retaining full payload
-- JSON forever. Old epochs are deliberately cheap permanent rows: removing an
-- epoch is only safe after every producer WAL and stream replay window is gone.
CREATE TABLE settlement.ingest_producer_cursors (
    producer_id text NOT NULL CHECK (producer_id ~ '^[a-z][a-z0-9-]{0,62}$'),
    producer_epoch uuid NOT NULL,
    last_producer_sequence bigint NOT NULL CHECK (last_producer_sequence > 0),
    last_event_id uuid NOT NULL,
    last_payload_sha256 bytea NOT NULL CHECK (octet_length(last_payload_sha256) = 32),
    last_outcome text NOT NULL CHECK (last_outcome IN (
        'baseline',
        'interval',
        'counter_reset',
        'out_of_order',
        'reopened_baseline'
    )),
    last_session_epoch bigint NOT NULL CHECK (last_session_epoch > 0),
    last_source_stream text NOT NULL,
    last_source_sequence bigint NOT NULL CHECK (last_source_sequence > 0),
    last_received_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (producer_id, producer_epoch)
);

-- Source coordinates live on the interval itself so evidence construction no
-- longer needs event_inbox as a permanent foreign-key root. Columns remain
-- nullable only for already-written v1 history; every v2 insert supplies all
-- five values and the all-or-none check prevents partial provenance.
ALTER TABLE ledger.raw_session_intervals
    ADD COLUMN source_stream text,
    ADD COLUMN source_sequence bigint,
    ADD COLUMN producer_id text,
    ADD COLUMN producer_epoch uuid,
    ADD COLUMN producer_sequence bigint;

ALTER TABLE ledger.raw_session_intervals
    ADD CONSTRAINT raw_interval_source_provenance CHECK (
        (
            source_stream IS NULL
            AND source_sequence IS NULL
            AND producer_id IS NULL
            AND producer_epoch IS NULL
            AND producer_sequence IS NULL
        )
        OR
        (
            char_length(source_stream) BETWEEN 1 AND 255
            AND source_sequence > 0
            AND producer_id ~ '^[a-z][a-z0-9-]{0,62}$'
            AND producer_epoch IS NOT NULL
            AND producer_sequence > 0
        )
    );

CREATE UNIQUE INDEX raw_session_intervals_producer_sequence_idx
    ON ledger.raw_session_intervals (producer_id, producer_epoch, producer_sequence)
    WHERE producer_id IS NOT NULL;

CREATE INDEX raw_session_intervals_source_sequence_idx
    ON ledger.raw_session_intervals (source_stream, source_sequence)
    WHERE source_stream IS NOT NULL;

-- Event UUIDs remain immutable trace identities, but they no longer force a
-- permanent copy of every raw message. Existing rows and legacy v1 replay are
-- untouched; only the unnecessary cross-table retention locks are removed.
ALTER TABLE settlement.event_inbox
    DROP CONSTRAINT event_inbox_ledger_event_fk;

ALTER TABLE settlement.session_states
    DROP CONSTRAINT session_states_last_event_id_fkey;

ALTER TABLE ledger.raw_session_intervals
    DROP CONSTRAINT raw_session_intervals_event_id_fkey,
    DROP CONSTRAINT raw_session_intervals_previous_event_id_fkey;

-- Late-evidence detection reads v2 provenance directly and falls back to the
-- legacy inbox only for pre-cutover intervals.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.record_late_seeding_interval()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.previous_left <> 0 OR NEW.current_left <> 0 THEN
        RETURN NEW;
    END IF;

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
        coalesce(NEW.source_sequence, inbox.source_sequence),
        clock_timestamp()
    FROM ledger.seeding_evidence_windows AS evidence_window
    LEFT JOIN settlement.event_inbox AS inbox
      ON inbox.event_id = NEW.event_id
    WHERE NEW.starts_at < evidence_window.window_end
      AND NEW.ends_at > evidence_window.window_start
      AND coalesce(NEW.source_stream, inbox.source_stream) = evidence_window.announce_source_stream
      AND coalesce(NEW.source_sequence, inbox.source_sequence) > evidence_window.announce_fence_sequence
      AND NEW.ends_at <= NEW.starts_at + (
          coalesce(evidence_window.max_interval_credit_seconds, 2100)
          * interval '1 second'
      )
    ON CONFLICT (window_start, interval_event_id) DO NOTHING;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down

-- Downgrading after v2 ingestion would remove its only durable idempotency
-- fence and provenance. Empty development databases can still roll back.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM settlement.ingest_producer_cursors) THEN
        RAISE EXCEPTION 'cannot downgrade after tracker.announce.v2 ingestion';
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.record_late_seeding_interval()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.previous_left <> 0 OR NEW.current_left <> 0 THEN
        RETURN NEW;
    END IF;

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
     AND NEW.ends_at <= NEW.starts_at + (
         coalesce(evidence_window.max_interval_credit_seconds, 2100)
         * interval '1 second'
     )
    WHERE inbox.event_id = NEW.event_id
    ON CONFLICT (window_start, interval_event_id) DO NOTHING;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER TABLE ledger.raw_session_intervals
    ADD CONSTRAINT raw_session_intervals_previous_event_id_fkey
        FOREIGN KEY (previous_event_id) REFERENCES settlement.event_inbox (event_id) ON DELETE RESTRICT,
    ADD CONSTRAINT raw_session_intervals_event_id_fkey
        FOREIGN KEY (event_id) REFERENCES settlement.event_inbox (event_id) ON DELETE RESTRICT;

ALTER TABLE settlement.session_states
    ADD CONSTRAINT session_states_last_event_id_fkey
        FOREIGN KEY (last_event_id) REFERENCES settlement.event_inbox (event_id) ON DELETE RESTRICT;

ALTER TABLE settlement.event_inbox
    ADD CONSTRAINT event_inbox_ledger_event_fk
        FOREIGN KEY (ledger_event_id) REFERENCES ledger.raw_session_intervals (event_id) ON DELETE RESTRICT;

DROP INDEX ledger.raw_session_intervals_source_sequence_idx;
DROP INDEX ledger.raw_session_intervals_producer_sequence_idx;

ALTER TABLE ledger.raw_session_intervals
    DROP CONSTRAINT raw_interval_source_provenance,
    DROP COLUMN producer_sequence,
    DROP COLUMN producer_epoch,
    DROP COLUMN producer_id,
    DROP COLUMN source_sequence,
    DROP COLUMN source_stream;

DROP TABLE settlement.ingest_producer_cursors;
DROP TABLE settlement.ingest_stream_cursors;
