-- +goose Up

-- A late leeching-only interval cannot change hourly seeding evidence and must
-- not block an otherwise valid reward window. Only intervals whose complete
-- range was observed with left=0 can alter the unioned seeding duration.
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

-- +goose Down

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.record_late_seeding_interval()
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
