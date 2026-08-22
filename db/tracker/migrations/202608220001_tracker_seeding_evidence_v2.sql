-- +goose Up

-- v1 closed each UTC hour as soon as it observed any announce at the hour
-- boundary. That is not a sufficient event-time watermark when the ordered
-- Settlement consumer is catching up: later stream sequences can still carry
-- a credible interval that overlaps the hour. v2 records both timing limits
-- on every immutable window so an auditor can reproduce the decision without
-- consulting today's process configuration.
ALTER TABLE ledger.seeding_evidence_windows
    DROP CONSTRAINT seeding_evidence_windows_schema_version_check;

ALTER TABLE ledger.seeding_evidence_windows
    ADD COLUMN closure_delay_seconds integer,
    ADD COLUMN max_interval_credit_seconds integer;

ALTER TABLE ledger.seeding_evidence_windows
    ADD CONSTRAINT seeding_evidence_windows_schema_policy_check CHECK (
        (
            schema_version = 'seeding.evidence.v1'
            AND closure_delay_seconds IS NULL
            AND max_interval_credit_seconds IS NULL
        )
        OR
        (
            schema_version = 'seeding.evidence.v2'
            AND closure_delay_seconds BETWEEN 60 AND 3600
            AND max_interval_credit_seconds BETWEEN 60 AND 3600
            AND closure_delay_seconds >= max_interval_credit_seconds
            AND announce_fence_received_at >= window_end
                + (closure_delay_seconds * interval '1 second')
            AND built_at >= window_end
                + (closure_delay_seconds * interval '1 second')
        )
    );

-- A stale multi-hour gap is not proof that the client seeded continuously.
-- UNIT3D applies the same conservative principle by granting no seed time when
-- adjacent updates exceed its credible interval. Keep v1 rows reproducible,
-- but stop new stale intervals from creating meaningless late anomalies by
-- using PeerGo's historical 35-minute peer lifetime as their fallback limit.
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

-- +goose Down

-- Downgrading after a v2 window has closed would discard policy evidence and
-- is intentionally refused. A fresh development database can still roll back.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM ledger.seeding_evidence_windows
        WHERE schema_version = 'seeding.evidence.v2'
    ) THEN
        RAISE EXCEPTION 'cannot downgrade after seeding.evidence.v2 windows exist';
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
    WHERE inbox.event_id = NEW.event_id
    ON CONFLICT (window_start, interval_event_id) DO NOTHING;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER TABLE ledger.seeding_evidence_windows
    DROP CONSTRAINT seeding_evidence_windows_schema_policy_check;

ALTER TABLE ledger.seeding_evidence_windows
    DROP COLUMN max_interval_credit_seconds,
    DROP COLUMN closure_delay_seconds;

ALTER TABLE ledger.seeding_evidence_windows
    ADD CONSTRAINT seeding_evidence_windows_schema_version_check
    CHECK (schema_version = 'seeding.evidence.v1');
