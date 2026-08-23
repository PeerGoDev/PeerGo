-- +goose Up

-- PostgreSQL prepares OLD against the trigger relation's concrete row type.
-- Keep the source and anomaly guards separate so each function only references
-- columns that exist on its table.
DROP TRIGGER seeding_evidence_sources_retained ON ledger.seeding_evidence_sources;
DROP TRIGGER seeding_evidence_anomalies_retained ON ledger.seeding_evidence_anomalies;

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_bounded_seeding_source()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'DELETE' THEN
        RAISE EXCEPTION 'seeding evidence sources are immutable';
    END IF;
    IF OLD.window_start >= clock_timestamp() - interval '12 hours'
        OR EXISTS (
            SELECT 1
            FROM ledger.seeding_evidence_anomalies AS anomaly
            WHERE anomaly.window_start = OLD.window_start
        ) THEN
        RAISE EXCEPTION 'recent or anomalous seeding evidence sources cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_bounded_seeding_anomaly()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'DELETE' THEN
        RAISE EXCEPTION 'seeding evidence anomalies are immutable';
    END IF;
    IF OLD.detected_at >= clock_timestamp() - interval '30 days' THEN
        RAISE EXCEPTION 'recent seeding evidence anomalies cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER seeding_evidence_sources_retained
BEFORE UPDATE OR DELETE ON ledger.seeding_evidence_sources
FOR EACH ROW EXECUTE FUNCTION ledger.protect_bounded_seeding_source();

CREATE TRIGGER seeding_evidence_anomalies_retained
BEFORE UPDATE OR DELETE ON ledger.seeding_evidence_anomalies
FOR EACH ROW EXECUTE FUNCTION ledger.protect_bounded_seeding_anomaly();

DROP FUNCTION ledger.protect_bounded_seeding_detail();

-- +goose Down

-- Cleanup is intentionally irreversible once short-lived detail has aged out.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION '202608230002 is irreversible after bounded seeding cleanup';
END;
$$;
-- +goose StatementEnd
