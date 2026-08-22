-- +goose Up

-- H&R is evaluated from immutable completion evidence, but ordinary announce
-- intervals only matter while an obligation for the same user/torrent is
-- tracking.  The v1 trigger queued every interval even when H&R was disabled;
-- on a busy Tracker that duplicated the entire announce workload into hnr_work.
-- Keep terminal work rows for audit and record whether a row was evaluated or
-- was proven irrelevant without inventing an obligation.
ALTER TABLE settlement.hnr_work
    ADD COLUMN processing_disposition text CHECK (
        processing_disposition IN ('evaluated', 'irrelevant_no_obligation')
    );

ALTER TABLE settlement.hnr_work
    ADD CONSTRAINT hnr_work_disposition_state CHECK (
        processed_at IS NOT NULL OR processing_disposition IS NULL
    ) NOT VALID;

ALTER TABLE settlement.hnr_work
    VALIDATE CONSTRAINT hnr_work_disposition_state;

CREATE INDEX raw_session_intervals_hnr_completion_aggregate_idx
    ON ledger.raw_session_intervals (user_id, torrent_id, event_id)
    WHERE completed_transition;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.enqueue_hnr_work()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    -- A completion always receives one immutable policy assessment, including
    -- a disabled assessment.  Later intervals are routed only when there is a
    -- live obligation or when a preceding completion is still waiting to be
    -- assessed.  The pending-completion branch closes the small race between
    -- completion ingestion and obligation creation.
    IF NEW.completed_transition
        OR EXISTS (
            SELECT 1
            FROM ledger.hnr_obligations AS obligation
            INNER JOIN ledger.hnr_completion_assessments AS assessment
                ON assessment.id = obligation.assessment_id
            WHERE obligation.state = 'tracking'
              AND assessment.user_id = NEW.user_id
              AND assessment.torrent_id = NEW.torrent_id
        )
        OR EXISTS (
            SELECT 1
            FROM settlement.hnr_work AS completion_work
            INNER JOIN ledger.raw_session_intervals AS completion
                ON completion.event_id = completion_work.interval_event_id
            WHERE completion_work.processed_at IS NULL
              AND completion.completed_transition
              AND completion.user_id = NEW.user_id
              AND completion.torrent_id = NEW.torrent_id
        ) THEN
        INSERT INTO settlement.hnr_work (interval_event_id, available_at, created_at)
        VALUES (NEW.event_id, NEW.created_at, NEW.created_at)
        ON CONFLICT (interval_event_id) DO NOTHING;
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
        RAISE EXCEPTION 'H&R work evidence cannot be deleted';
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
    -- Rows that were already terminal before this migration retain a NULL
    -- disposition rather than forcing a high-WAL rewrite of historical work.
    -- Every new terminal transition must identify how it was resolved.
    IF OLD.processed_at IS NULL
        AND NEW.processed_at IS NOT NULL
        AND NEW.processing_disposition IS NULL THEN
        RAISE EXCEPTION 'H&R work terminal disposition is incomplete';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_hnr_work()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'H&R work evidence cannot be deleted';
    END IF;
    IF OLD.interval_event_id IS DISTINCT FROM NEW.interval_event_id
        OR OLD.created_at IS DISTINCT FROM NEW.created_at
        OR NEW.attempts < OLD.attempts THEN
        RAISE EXCEPTION 'H&R work identity is immutable';
    END IF;
    IF OLD.processed_at IS NOT NULL AND OLD IS DISTINCT FROM NEW THEN
        RAISE EXCEPTION 'processed H&R work is terminal';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.enqueue_hnr_work()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO settlement.hnr_work (interval_event_id, available_at, created_at)
    VALUES (NEW.event_id, NEW.created_at, NEW.created_at)
    ON CONFLICT (interval_event_id) DO NOTHING;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP INDEX IF EXISTS ledger.raw_session_intervals_hnr_completion_aggregate_idx;
ALTER TABLE settlement.hnr_work
    DROP CONSTRAINT IF EXISTS hnr_work_disposition_state,
    DROP COLUMN IF EXISTS processing_disposition;
