-- Requires the retained schema archive to have been restored and verified
-- on an isolated PostgreSQL instance. Preserve the original table OID, grants,
-- constraints, indexes and guards; TRUNCATE/restore is one atomic transaction.
BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='15min';
SET LOCAL max_parallel_workers_per_gather=0;
SELECT pg_advisory_xact_lock(20261016, 4001);
LOCK TABLE ledger.seeding_swarm_snapshots, settlement.seeding_swarm_snapshot_inbox,
 ledger.seeding_evidence_windows, ledger.seeding_evidence_anomalies,
 settlement.seeding_reconciliation_work IN SHARE MODE;
LOCK TABLE ledger.seeding_swarm_snapshot_entries IN ACCESS EXCLUSIVE MODE;
LOCK TABLE storage_snapshot_rewrite.control, storage_snapshot_rewrite.plan,
 storage_snapshot_rewrite.retained, storage_snapshot_rewrite.manifest IN SHARE ROW EXCLUSIVE MODE;
DO $$ BEGIN
 IF (SELECT count(*) FROM storage_snapshot_rewrite.control)<>1
    OR NOT (SELECT backup_verified AND applied_at IS NULL FROM storage_snapshot_rewrite.control) THEN
  RAISE EXCEPTION 'verified restore required, or rewrite already applied';
 END IF;
 IF (SELECT original_oid FROM storage_snapshot_rewrite.control)<>'ledger.seeding_swarm_snapshot_entries'::regclass::oid THEN
  RAISE EXCEPTION 'source table identity changed';
 END IF;
 IF EXISTS (SELECT 1 FROM pg_constraint WHERE confrelid='ledger.seeding_swarm_snapshot_entries'::regclass) THEN
  RAISE EXCEPTION 'new incoming foreign key';
 END IF;
 IF EXISTS (
  SELECT 1 FROM storage_snapshot_rewrite.plan p
  LEFT JOIN ledger.seeding_swarm_snapshots s USING(snapshot_id)
  WHERE p.expires AND (s.snapshot_id IS NULL OR s.status<>'complete'
   OR s.observed_at>=(SELECT cutoff FROM storage_snapshot_rewrite.control)
   OR NOT ledger.is_redundant_hourly_snapshot(p.snapshot_id))
 ) THEN RAISE EXCEPTION 'retired frame is no longer eligible'; END IF;
 IF EXISTS (
  SELECT 1 FROM ledger.seeding_swarm_snapshots s
  WHERE NOT EXISTS(SELECT 1 FROM storage_snapshot_rewrite.plan p WHERE p.snapshot_id=s.snapshot_id)
   AND EXISTS(SELECT 1 FROM ledger.seeding_swarm_snapshot_entries e WHERE e.snapshot_id=s.snapshot_id LIMIT 1 OFFSET 0)
 ) THEN RAISE EXCEPTION 'new legacy entries appeared after prepare'; END IF;
 IF EXISTS (SELECT 1 FROM storage_snapshot_rewrite.plan p WHERE NOT p.expires
  AND NOT EXISTS(SELECT 1 FROM storage_snapshot_rewrite.manifest m WHERE m.snapshot_id=p.snapshot_id))
 OR EXISTS(SELECT 1 FROM storage_snapshot_rewrite.manifest m WHERE NOT EXISTS(
  SELECT 1 FROM storage_snapshot_rewrite.plan p WHERE p.snapshot_id=m.snapshot_id AND NOT p.expires))
 THEN RAISE EXCEPTION 'plan and retained manifest differ'; END IF;
 IF EXISTS (
  SELECT 1 FROM storage_snapshot_rewrite.manifest m
  CROSS JOIN LATERAL (
   SELECT count(*) row_count,
    sha256(convert_to(coalesce(string_agg(encode(info_hash_v1,'hex')||':'||seeders::text||':'||leechers::text,
    ',' ORDER BY info_hash_v1),''),'UTF8')) sha256
   FROM ledger.seeding_swarm_snapshot_entries e WHERE e.snapshot_id=m.snapshot_id OFFSET 0
  ) d WHERE (m.row_count,m.sha256) IS DISTINCT FROM (d.row_count,d.sha256)
 ) THEN RAISE EXCEPTION 'retained original changed after prepare'; END IF;
 IF EXISTS (
  SELECT 1 FROM storage_snapshot_rewrite.manifest m
  CROSS JOIN LATERAL (
   SELECT count(*) row_count,
    sha256(convert_to(coalesce(string_agg(encode(info_hash_v1,'hex')||':'||seeders::text||':'||leechers::text,
    ',' ORDER BY info_hash_v1),''),'UTF8')) sha256
   FROM storage_snapshot_rewrite.retained e WHERE e.snapshot_id=m.snapshot_id OFFSET 0
  ) d WHERE (m.row_count,m.sha256) IS DISTINCT FROM (d.row_count,d.sha256)
 ) OR (SELECT count(*) FROM storage_snapshot_rewrite.retained)<>
      (SELECT coalesce(sum(row_count),0) FROM storage_snapshot_rewrite.manifest)
 THEN RAISE EXCEPTION 'retained copy no longer matches manifest'; END IF;
END $$;
TRUNCATE TABLE ledger.seeding_swarm_snapshot_entries;
INSERT INTO ledger.seeding_swarm_snapshot_entries(snapshot_id,info_hash_v1,seeders,leechers)
SELECT snapshot_id,info_hash_v1,seeders,leechers FROM storage_snapshot_rewrite.retained
ORDER BY snapshot_id,info_hash_v1;
DO $$ BEGIN
 IF (SELECT count(*) FROM ledger.seeding_swarm_snapshot_entries)<>
    (SELECT coalesce(sum(row_count),0) FROM storage_snapshot_rewrite.manifest)
 OR EXISTS (
  SELECT 1 FROM storage_snapshot_rewrite.manifest m
  CROSS JOIN LATERAL (
   SELECT count(*) row_count,
    sha256(convert_to(coalesce(string_agg(encode(info_hash_v1,'hex')||':'||seeders::text||':'||leechers::text,
    ',' ORDER BY info_hash_v1),''),'UTF8')) sha256
   FROM ledger.seeding_swarm_snapshot_entries e WHERE e.snapshot_id=m.snapshot_id OFFSET 0
  ) d WHERE (m.row_count,m.sha256) IS DISTINCT FROM (d.row_count,d.sha256)
 ) THEN RAISE EXCEPTION 'rewritten rows do not match retained manifests'; END IF;
END $$;
INSERT INTO settlement.snapshot_entry_gc_progress(snapshot_id)
SELECT snapshot_id FROM storage_snapshot_rewrite.plan WHERE expires
ON CONFLICT(snapshot_id) DO NOTHING;
UPDATE storage_snapshot_rewrite.control SET applied_at=clock_timestamp();
COMMIT;
ANALYZE ledger.seeding_swarm_snapshot_entries;
SELECT json_build_object('phase','applied','original_bytes',original_bytes,
 'current_bytes',pg_total_relation_size('ledger.seeding_swarm_snapshot_entries'),
 'retained_rows',(SELECT count(*) FROM ledger.seeding_swarm_snapshot_entries),
 'original_oid_preserved',original_oid='ledger.seeding_swarm_snapshot_entries'::regclass::oid)
FROM storage_snapshot_rewrite.control;
