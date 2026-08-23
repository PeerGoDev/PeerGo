-- +goose Up

-- Snapshot-entry cleanup removes thousands of rows belonging to the same
-- snapshot. Preserve the retention boundary while validating each affected
-- snapshot once per statement instead of repeating ledger probes per row.
DROP TRIGGER seeding_swarm_snapshot_entries_retained
    ON ledger.seeding_swarm_snapshot_entries;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_recent_seeding_snapshot_entries()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM (
            SELECT DISTINCT deleted.snapshot_id
            FROM deleted_snapshot_entries AS deleted
        ) AS deleted_snapshot
        INNER JOIN ledger.seeding_swarm_snapshots AS snapshot
            ON snapshot.snapshot_id = deleted_snapshot.snapshot_id
        WHERE snapshot.observed_at >= coalesce(
            (SELECT max(evidence_window.window_end)
             FROM ledger.seeding_evidence_windows AS evidence_window),
            '-infinity'::timestamptz
        )
    ) OR EXISTS (
        SELECT 1
        FROM (
            SELECT DISTINCT deleted.snapshot_id
            FROM deleted_snapshot_entries AS deleted
        ) AS deleted_snapshot
        INNER JOIN ledger.seeding_evidence_windows AS evidence_window
            ON evidence_window.selected_snapshot_id = deleted_snapshot.snapshot_id
        WHERE evidence_window.window_end >= clock_timestamp() - interval '12 hours'
           OR EXISTS (
               SELECT 1
               FROM ledger.seeding_evidence_anomalies AS anomaly
               WHERE anomaly.window_start = evidence_window.window_start
           )
    ) THEN
        RAISE EXCEPTION 'recent or anomalous selected seeding snapshot entries are retained';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER seeding_swarm_snapshot_entries_update_immutable
BEFORE UPDATE ON ledger.seeding_swarm_snapshot_entries
FOR EACH STATEMENT EXECUTE FUNCTION ledger.reject_bounded_seeding_evidence_update();

CREATE TRIGGER seeding_swarm_snapshot_entries_retained
AFTER DELETE ON ledger.seeding_swarm_snapshot_entries
REFERENCING OLD TABLE AS deleted_snapshot_entries
FOR EACH STATEMENT EXECUTE FUNCTION ledger.protect_recent_seeding_snapshot_entries();

-- +goose Down

-- Cleanup is intentionally irreversible once short-lived detail has aged out.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION '202608230004 is irreversible after batched snapshot cleanup';
END;
$$;
-- +goose StatementEnd
