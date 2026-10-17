-- Operator maintenance after migration 202610170001, before large catch-up.
-- Ordinary VACUUM removes already-dead versions and index entries. It never
-- deletes live evidence and does not rewrite the live heap or pause services.
-- Run with the database owner/admin using psql, not inside a transaction.
\set ON_ERROR_STOP on
\connect peergo_core
SET lock_timeout='3s';
SET statement_timeout='15min';
SET maintenance_work_mem='256MB';
SET vacuum_cost_delay='2ms';
SET vacuum_cost_limit=400;
VACUUM (ANALYZE, INDEX_CLEANUP ON, TRUNCATE FALSE, PARALLEL 2) economy.seeding_reward_evidence_items;
ALTER TABLE economy.seeding_reward_evidence_items SET (
 autovacuum_vacuum_scale_factor=0.01,
 autovacuum_vacuum_threshold=50000,
 autovacuum_analyze_scale_factor=0.01,
 autovacuum_analyze_threshold=50000
);
SELECT json_build_object('table',s.relid::regclass,'estimated_live',s.n_live_tup,
 'estimated_dead',s.n_dead_tup,'last_vacuum',s.last_vacuum,'options',c.reloptions)
FROM pg_stat_user_tables s JOIN pg_class c ON c.oid=s.relid
WHERE s.relid='economy.seeding_reward_evidence_items'::regclass;
\connect peergo_tracker
SET lock_timeout='3s';
SET statement_timeout='15min';
SET maintenance_work_mem='256MB';
SET vacuum_cost_delay='2ms';
SET vacuum_cost_limit=400;
VACUUM (ANALYZE, INDEX_CLEANUP ON, TRUNCATE FALSE, PARALLEL 2) ledger.seeding_evidence_items;
ALTER TABLE ledger.seeding_evidence_items SET (
 autovacuum_vacuum_scale_factor=0.01,
 autovacuum_vacuum_threshold=50000,
 autovacuum_analyze_scale_factor=0.01,
 autovacuum_analyze_threshold=50000
);
SELECT json_build_object('table',s.relid::regclass,'estimated_live',s.n_live_tup,
 'estimated_dead',s.n_dead_tup,'last_vacuum',s.last_vacuum,'options',c.reloptions)
FROM pg_stat_user_tables s JOIN pg_class c ON c.oid=s.relid
WHERE s.relid='ledger.seeding_evidence_items'::regclass;
