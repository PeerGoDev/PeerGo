-- +goose Up

-- Source cleanup operates in batches. Validate the set of affected evidence
-- windows once per statement instead of probing the anomaly index once for
-- every deleted source row.
DROP TRIGGER seeding_evidence_sources_retained ON ledger.seeding_evidence_sources;
DROP TRIGGER seeding_evidence_anomalies_retained ON ledger.seeding_evidence_anomalies;

-- +goose StatementBegin
CREATE FUNCTION ledger.reject_bounded_seeding_evidence_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'seeding evidence detail is immutable';
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_bounded_seeding_source()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM (
            SELECT DISTINCT deleted.window_start
            FROM deleted_seeding_sources AS deleted
        ) AS deleted_window
        WHERE deleted_window.window_start >= clock_timestamp() - interval '12 hours'
           OR EXISTS (
               SELECT 1
               FROM ledger.seeding_evidence_anomalies AS anomaly
               WHERE anomaly.window_start = deleted_window.window_start
           )
    ) THEN
        RAISE EXCEPTION 'recent or anomalous seeding evidence sources cannot be deleted';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_bounded_seeding_anomaly()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM deleted_seeding_anomalies AS deleted
        WHERE deleted.detected_at >= clock_timestamp() - interval '30 days'
    ) THEN
        RAISE EXCEPTION 'recent seeding evidence anomalies cannot be deleted';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER seeding_evidence_sources_update_immutable
BEFORE UPDATE ON ledger.seeding_evidence_sources
FOR EACH STATEMENT EXECUTE FUNCTION ledger.reject_bounded_seeding_evidence_update();

CREATE TRIGGER seeding_evidence_sources_retained
AFTER DELETE ON ledger.seeding_evidence_sources
REFERENCING OLD TABLE AS deleted_seeding_sources
FOR EACH STATEMENT EXECUTE FUNCTION ledger.protect_bounded_seeding_source();

CREATE TRIGGER seeding_evidence_anomalies_update_immutable
BEFORE UPDATE ON ledger.seeding_evidence_anomalies
FOR EACH STATEMENT EXECUTE FUNCTION ledger.reject_bounded_seeding_evidence_update();

CREATE TRIGGER seeding_evidence_anomalies_retained
AFTER DELETE ON ledger.seeding_evidence_anomalies
REFERENCING OLD TABLE AS deleted_seeding_anomalies
FOR EACH STATEMENT EXECUTE FUNCTION ledger.protect_bounded_seeding_anomaly();

-- +goose Down

-- Cleanup is intentionally irreversible once short-lived detail has aged out.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION '202608230003 is irreversible after batched seeding cleanup';
END;
$$;
-- +goose StatementEnd
