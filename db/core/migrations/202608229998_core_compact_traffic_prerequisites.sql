-- +goose NO TRANSACTION
-- +goose Up

-- Keep metadata-only changes on the existing hot tables outside the long
-- rollup backfill transaction.  Each statement commits before the backfill
-- starts, so a live projector cannot form a lock cycle with the migration.
-- Every operation is idempotent because a no-transaction migration may be
-- retried after a transient lock failure.
ALTER TABLE traffic.settlement_inbox
    ALTER COLUMN payload_json DROP NOT NULL,
    DROP CONSTRAINT IF EXISTS settlement_inbox_payload_json_check;

ALTER TABLE traffic.settlement_inbox SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05,
    autovacuum_analyze_threshold = 1000
);

ALTER TABLE traffic.user_traffic_entries SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05,
    autovacuum_analyze_threshold = 1000
);

ALTER TABLE traffic.user_traffic_entry_explanations SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05,
    autovacuum_analyze_threshold = 1000
);

ALTER TABLE traffic.user_traffic_entry_segments SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05,
    autovacuum_analyze_threshold = 1000
);

-- +goose Down

-- Once compact cleanup starts, restoring the payload constraint would require
-- reconstructing discarded event bodies.  Restore a pre-migration backup.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION '202608229998 is irreversible after compact traffic cleanup';
END;
$$;
-- +goose StatementEnd
