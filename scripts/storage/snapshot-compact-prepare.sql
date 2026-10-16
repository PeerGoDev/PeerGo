-- One-time physical rewrite of the obsolete row representation.
-- Pause the worker first. New writers must use compressed snapshot payloads.
-- Only old interior frames accepted by the existing guard are retired.
BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='15min';
SET LOCAL max_parallel_workers_per_gather=0;
SELECT pg_advisory_xact_lock(20261016, 4001);
LOCK TABLE ledger.seeding_swarm_snapshots, settlement.seeding_swarm_snapshot_inbox,
 ledger.seeding_evidence_windows, ledger.seeding_evidence_anomalies,
 settlement.seeding_reconciliation_work IN SHARE MODE;
LOCK TABLE ledger.seeding_swarm_snapshot_entries IN SHARE MODE;
DO $$
BEGIN
 IF (SELECT version_id FROM goose_db_version WHERE is_applied ORDER BY id DESC LIMIT 1) <> 202610160003 THEN
  RAISE EXCEPTION 'unexpected Tracker migration';
 END IF;
 IF EXISTS (SELECT 1 FROM pg_constraint WHERE confrelid='ledger.seeding_swarm_snapshot_entries'::regclass) THEN
  RAISE EXCEPTION 'incoming foreign key prevents an in-place rewrite';
 END IF;
 IF EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='ledger.seeding_swarm_snapshot_entries'::regclass AND NOT convalidated) THEN
  RAISE EXCEPTION 'unvalidated source constraint';
 END IF;
END $$;
CREATE SCHEMA storage_snapshot_rewrite;
REVOKE ALL ON SCHEMA storage_snapshot_rewrite FROM PUBLIC;
CREATE TABLE storage_snapshot_rewrite.control AS
SELECT clock_timestamp() prepared_at, statement_timestamp()-interval '12 hours' cutoff,
 'ledger.seeding_swarm_snapshot_entries'::regclass::oid original_oid,
 pg_total_relation_size('ledger.seeding_swarm_snapshot_entries') original_bytes,
 false backup_verified, NULL::timestamptz applied_at;
CREATE TABLE storage_snapshot_rewrite.plan (
 snapshot_id uuid PRIMARY KEY, expires boolean NOT NULL
);
INSERT INTO storage_snapshot_rewrite.plan
SELECT snapshot_id, status='complete'
 AND observed_at<(SELECT cutoff FROM storage_snapshot_rewrite.control)
 AND ledger.is_redundant_hourly_snapshot(snapshot_id)
FROM ledger.seeding_swarm_snapshots;
CREATE TABLE storage_snapshot_rewrite.retained
 (LIKE ledger.seeding_swarm_snapshot_entries INCLUDING ALL);
INSERT INTO storage_snapshot_rewrite.retained
SELECT entry.*
FROM (SELECT snapshot_id FROM storage_snapshot_rewrite.plan WHERE NOT expires
      ORDER BY snapshot_id OFFSET 0) plan
CROSS JOIN LATERAL (
 SELECT entry.* FROM ledger.seeding_swarm_snapshot_entries entry
 WHERE entry.snapshot_id=plan.snapshot_id ORDER BY info_hash_v1 OFFSET 0
) entry;
CREATE TABLE storage_snapshot_rewrite.manifest (
 snapshot_id uuid PRIMARY KEY, row_count bigint NOT NULL, sha256 bytea NOT NULL
);
INSERT INTO storage_snapshot_rewrite.manifest
SELECT plan.snapshot_id, digest.row_count, digest.sha256
FROM storage_snapshot_rewrite.plan plan
CROSS JOIN LATERAL (
 SELECT count(*) row_count,
 sha256(convert_to(coalesce(string_agg(encode(info_hash_v1,'hex')||':'||seeders::text||':'||leechers::text,
 ',' ORDER BY info_hash_v1),''),'UTF8')) sha256
 FROM ledger.seeding_swarm_snapshot_entries entry
 WHERE entry.snapshot_id=plan.snapshot_id OFFSET 0
) digest WHERE NOT plan.expires;
DO $$ BEGIN
 IF EXISTS (
  SELECT 1 FROM storage_snapshot_rewrite.manifest m
  CROSS JOIN LATERAL (
   SELECT count(*) row_count,
    sha256(convert_to(coalesce(string_agg(encode(info_hash_v1,'hex')||':'||seeders::text||':'||leechers::text,
    ',' ORDER BY info_hash_v1),''),'UTF8')) sha256
   FROM storage_snapshot_rewrite.retained e WHERE e.snapshot_id=m.snapshot_id OFFSET 0
  ) d WHERE (m.row_count,m.sha256) IS DISTINCT FROM (d.row_count,d.sha256)
 ) THEN RAISE EXCEPTION 'retained copy does not match original per-frame manifests'; END IF;
END $$;
COMMIT;
SELECT json_build_object('phase','prepared','original_bytes',original_bytes,
 'snapshot_count',(SELECT count(*) FROM storage_snapshot_rewrite.plan),
 'retired_frames',(SELECT count(*) FROM storage_snapshot_rewrite.plan WHERE expires),
 'retained_rows',(SELECT coalesce(sum(row_count),0) FROM storage_snapshot_rewrite.manifest),
 'retained_bytes',pg_total_relation_size('storage_snapshot_rewrite.retained'))
FROM storage_snapshot_rewrite.control;
