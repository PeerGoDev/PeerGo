#!/usr/bin/env python3
"""Exercise the maintenance SQL on an isolated, fully migrated PostgreSQL DB.
The caller must provide a container and a disposable template database. No
production database URL or production fixture is accepted by this test runner.
"""
import argparse
import json
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parent
PREPARE = (ROOT / "snapshot-compact-prepare.sql").read_text()
APPLY = (ROOT / "snapshot-compact-apply.sql").read_text()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--container", required=True)
    parser.add_argument("--template", required=True)
    args = parser.parse_args()
    if not args.container.startswith("peergo-storage-rewrite-test") or not args.template.startswith("rewrite_test_"):
        raise SystemExit("an explicitly isolated test container and template are required")

    def shell(*cmd, **kwargs):
        return subprocess.run(["docker", "exec", "-i", args.container, *cmd], text=True,
                              capture_output=True, **kwargs)

    def sql(db, source, fail=False):
        r = shell("psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", db,
                  input=source, timeout=90)
        if fail:
            assert r.returncode != 0, "expected SQL failure"
        elif r.returncode:
            raise RuntimeError(r.stderr[-4000:])
        return r

    fixture = r"""
INSERT INTO ledger.seeding_swarm_snapshots
 (snapshot_id,source_id,routing_epoch,snapshot_sequence,observed_at,chunk_count,received_chunk_count,status,created_at,completed_at)
SELECT ('00000000-0000-0000-0000-'||lpad(n::text,12,'0'))::uuid,'rewrite-test',1,n,
 date_trunc('hour',now()-interval '72 hours')+n*interval '1 minute',1,1,'complete',now()-interval '72 hours',now()-interval '72 hours'
FROM generate_series(1,4) n;
INSERT INTO settlement.seeding_swarm_snapshot_inbox
 (event_id,snapshot_id,payload_sha256,source_stream,source_subject,source_sequence,delivery_count,observed_at,received_at)
SELECT snapshot_id,snapshot_id,decode(repeat('00',32),'hex'),'REWRITE_TEST','test.rewrite',snapshot_sequence,1,observed_at,observed_at
FROM ledger.seeding_swarm_snapshots;
-- First and last route frames and the first frame in another stream survive.
INSERT INTO settlement.seeding_swarm_snapshot_inbox
 (event_id,snapshot_id,payload_sha256,source_stream,source_subject,source_sequence,delivery_count,observed_at,received_at)
SELECT '10000000-0000-0000-0000-000000000003',snapshot_id,decode(repeat('00',32),'hex'),'REWRITE_TEST_NEW','test.rewrite',1,1,observed_at,observed_at
FROM ledger.seeding_swarm_snapshots WHERE snapshot_sequence=3;
INSERT INTO ledger.seeding_swarm_snapshot_entries
SELECT snapshot_id,decode(lpad(n::text,40,'0'),'hex'),n,2
FROM ledger.seeding_swarm_snapshots CROSS JOIN generate_series(1,6000) n;
"""
    passed = []
    for case in ["success", "rollback_after_truncate", "copy_corruption", "new_legacy_frame", "original_change", "guard_change", "unverified_archive"]:
        db = "rewrite_test_" + case
        r = shell("createdb", "-U", "postgres", "-T", args.template, db)
        if r.returncode:
            raise RuntimeError(r.stderr)
        sql(db, fixture)
        before = sql(db, "SELECT count(*) FROM ledger.seeding_swarm_snapshot_entries;").stdout.strip()
        original_oid = sql(db, "SELECT 'ledger.seeding_swarm_snapshot_entries'::regclass::oid;").stdout.strip()
        sql(db, PREPARE)
        retained = sql(db, "SELECT count(*) FROM storage_snapshot_rewrite.retained;").stdout.strip()
        assert retained == "18000", retained
        if case != "unverified_archive":
            sql(db, "UPDATE storage_snapshot_rewrite.control SET backup_verified=true;")
        operation = APPLY
        if case == "rollback_after_truncate":
            operation = operation.replace("INSERT INTO ledger.seeding_swarm_snapshot_entries(snapshot_id,info_hash_v1,seeders,leechers)",
                                          "SELECT 1/0;\nINSERT INTO ledger.seeding_swarm_snapshot_entries(snapshot_id,info_hash_v1,seeders,leechers)")
        elif case == "copy_corruption":
            sql(db, "UPDATE storage_snapshot_rewrite.retained SET seeders=seeders+1 WHERE info_hash_v1=decode(lpad('1',40,'0'),'hex');")
        elif case == "new_legacy_frame":
            sql(db, r"""INSERT INTO ledger.seeding_swarm_snapshots
 (snapshot_id,source_id,routing_epoch,snapshot_sequence,observed_at,chunk_count,received_chunk_count,status,created_at,completed_at)
 VALUES('00000000-0000-0000-0000-000000000005','rewrite-test',1,5,now(),1,1,'complete',now(),now());
 INSERT INTO ledger.seeding_swarm_snapshot_entries VALUES('00000000-0000-0000-0000-000000000005',decode(repeat('ff',20),'hex'),1,1);""")
            before = "24001"
        elif case == "original_change":
            sql(db, "INSERT INTO ledger.seeding_swarm_snapshot_entries VALUES('00000000-0000-0000-0000-000000000001',decode(repeat('ff',20),'hex'),1,1);")
            before = "24001"
        elif case == "guard_change":
            sql(db, r"""INSERT INTO settlement.seeding_swarm_snapshot_inbox
 (event_id,snapshot_id,payload_sha256,source_stream,source_subject,source_sequence,delivery_count,observed_at,received_at)
 SELECT '10000000-0000-0000-0000-000000000002',snapshot_id,decode(repeat('00',32),'hex'),'REWRITE_TEST_NEWEST','test.rewrite',1,1,observed_at,observed_at
 FROM ledger.seeding_swarm_snapshots WHERE snapshot_sequence=2;""")
        result = sql(db, operation, fail=case != "success")
        expected = retained if case == "success" else before
        assert sql(db, "SELECT count(*) FROM ledger.seeding_swarm_snapshot_entries;").stdout.strip() == expected
        assert sql(db, "SELECT 'ledger.seeding_swarm_snapshot_entries'::regclass::oid;").stdout.strip() == original_oid
        if case == "success":
            assert sql(db, "SELECT count(*) FROM ledger.seeding_swarm_snapshot_entries WHERE snapshot_id='00000000-0000-0000-0000-000000000002';").stdout.strip() == "0"
            sql(db, "DELETE FROM ledger.seeding_swarm_snapshot_entries WHERE snapshot_id='00000000-0000-0000-0000-000000000001';", fail=True)
            sql(db, "UPDATE ledger.seeding_swarm_snapshot_entries SET seeders=0;", fail=True)
        else:
            assert sql(db, "SELECT applied_at IS NULL FROM storage_snapshot_rewrite.control;").stdout.strip() == "t"
        passed.append(case)
        print(json.dumps({"case": case, "status": "PASS"}), flush=True)
    print(json.dumps({"passed": passed, "production_database_used": False}), flush=True)


if __name__ == "__main__":
    main()
