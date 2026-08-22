-- +goose Up

-- Full swarm snapshots arrive every few seconds or minutes, but hourly reward
-- evidence only needs the selected boundary snapshot. Keep the immutable
-- window/items forever while allowing redundant entry payloads to be pruned.
-- Selected detail stays available for 30 days, well beyond the seven-day NATS
-- replay window; after that the materialized evidence item is authoritative.
DROP TRIGGER seeding_swarm_snapshot_entries_immutable
    ON ledger.seeding_swarm_snapshot_entries;

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_recent_seeding_snapshot_entries()
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

CREATE TRIGGER seeding_swarm_snapshot_entries_retained
BEFORE UPDATE OR DELETE ON ledger.seeding_swarm_snapshot_entries
FOR EACH ROW EXECUTE FUNCTION ledger.protect_recent_seeding_snapshot_entries();

-- +goose Down

DROP TRIGGER seeding_swarm_snapshot_entries_retained
    ON ledger.seeding_swarm_snapshot_entries;
DROP FUNCTION ledger.protect_recent_seeding_snapshot_entries();

CREATE TRIGGER seeding_swarm_snapshot_entries_immutable
BEFORE UPDATE OR DELETE ON ledger.seeding_swarm_snapshot_entries
FOR EACH ROW EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();
