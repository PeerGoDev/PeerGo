-- Operator performance settings for existing Core queue tables.
-- Run as their owner, with psql -v ON_ERROR_STOP=1. No business rows change.
BEGIN;
SET LOCAL lock_timeout='2s';
SET LOCAL statement_timeout='10s';
ALTER TABLE economy.seeding_reward_work_items SET (
 autovacuum_vacuum_scale_factor=0.02,autovacuum_vacuum_threshold=1000,
 autovacuum_analyze_scale_factor=0.01,autovacuum_analyze_threshold=1000
);
ALTER TABLE audit.outbox SET (
 autovacuum_vacuum_scale_factor=0.02,autovacuum_vacuum_threshold=1000,
 autovacuum_analyze_scale_factor=0.01,autovacuum_analyze_threshold=1000
);
COMMIT;
-- After a one-off historical backfill, also run outside a transaction:
-- VACUUM (ANALYZE,TRUNCATE FALSE,SKIP_LOCKED TRUE,PARALLEL 0) <queue_table>;
-- Rollback: ALTER TABLE <queue_table> RESET (autovacuum_vacuum_scale_factor,
-- autovacuum_vacuum_threshold,autovacuum_analyze_scale_factor,autovacuum_analyze_threshold);
