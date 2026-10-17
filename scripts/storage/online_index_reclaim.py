#!/usr/bin/env python3
"""Bounded online reclamation of sparse indexes on disposable transport/work tables.

No business rows are deleted. Final accounting/evidence/raw tables are excluded
from unattended maintenance. Requires pgstattuple installed by the operator.
"""
import argparse
import json
import re
import shutil
import subprocess
import time

TABLES = {
    'peergo_tracker': [
        'ledger.traffic_settlements', 'ledger.traffic_settlement_segments',
        'settlement.policy_work', 'settlement.traffic_outbox', 'settlement.hnr_outbox',
        'settlement.seeding_evidence_outbox', 'settlement.session_states',
        'settlement.traffic_detail_gc', 'settlement.event_inbox',
    ],
    'peergo_core': [
        'traffic.settlement_inbox', 'traffic.user_traffic_entry_segments',
        'traffic.user_traffic_entry_explanations', 'audit.outbox',
    ],
}

def sparse(stats):
    pages = stats['leaf_pages'] + stats['deleted_pages'] + stats['empty_pages']
    return pages > 0 and stats['avg_leaf_density'] * stats['leaf_pages'] / pages < 25

def enough_space(free, size):
    return free >= max(40 * 1024**3, size * 3)

class Database:
    def __init__(self, container, name):
        self.container, self.name = container, name

    def sql(self, sql, seconds=60):
        p = subprocess.run(['docker', 'exec', '-i', self.container, 'sh', '-c',
            'exec psql -X -qAt -v ON_ERROR_STOP=1 -U "${POSTGRES_USER:-postgres}" -d "$1"', 'sh', self.name],
            input="SET application_name='peergo-index-reclaim'; SET lock_timeout='30s';\n"
                  + f"SET statement_timeout='{seconds}s';\n" + sql,
            text=True, capture_output=True, timeout=seconds + 15)
        if p.returncode:
            raise RuntimeError(p.stderr.strip()[:1200])
        return p.stdout.strip()

    def query(self, sql, seconds=60):
        return json.loads(self.sql(sql, seconds))

    def candidates(self):
        tables = ','.join("'" + table + "'" for table in TABLES[self.name])
        return self.query(f"""SELECT coalesce(json_agg(x),'[]'::json) FROM (
 SELECT format('%I.%I',n.nspname,c.relname) name, pg_relation_size(c.oid) bytes,
 i.indisvalid valid,i.indisready ready
 FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid
 JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_am am ON am.oid=c.relam
 WHERE i.indrelid::regclass::text IN ({tables}) AND am.amname='btree'
 AND pg_relation_size(c.oid)>=268435456 AND pg_relation_size(c.oid)<=8589934592
 AND (NOT i.indisvalid OR pg_relation_size(c.oid)/greatest(c.reltuples,1)>160)
 ORDER BY pg_relation_size(c.oid) DESC) x;""")

    def shape(self, index):
        assert re.fullmatch(r'[a-z_][a-z_0-9]*\.[a-z_][a-z_0-9]*', index), 'Unexpected identifier'
        return self.query(f"""SELECT json_build_object('definition',pg_get_indexdef(i.indexrelid),
 'table_oid',i.indrelid,'owner',c.relowner,'tablespace',c.reltablespace,
 'valid',i.indisvalid,'ready',i.indisready,'bytes',pg_relation_size(i.indexrelid),
 'constraints',(SELECT coalesce(json_agg(x ORDER BY conname),'[]'::json) FROM
 (SELECT conname,pg_get_constraintdef(oid) definition,convalidated FROM pg_constraint
 WHERE conindid=i.indexrelid) x)) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid
 WHERE i.indexrelid='{index}'::regclass;""")

    def rebuild(self, index):
        before = self.shape(index)
        if not before['valid'] or not before['ready']:
            raise RuntimeError('Invalid index requires operator review: ' + index)
        self.sql(f"SET maintenance_work_mem='256MB'; SET max_parallel_maintenance_workers=1; REINDEX INDEX CONCURRENTLY {index};", 600)
        after = self.shape(index)
        if {k:v for k,v in before.items() if k!='bytes'} != {k:v for k,v in after.items() if k!='bytes'}:
            raise RuntimeError('Index definition/constraint validation failed: ' + index)
        return {'index':index,'before_bytes':before['bytes'],'after_bytes':after['bytes']}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--container', required=True)
    parser.add_argument('--database', action='append', choices=TABLES)
    parser.add_argument('--execute', action='store_true', help='Default is a read-only plan')
    parser.add_argument('--max-indexes', type=int, default=3)
    parser.add_argument('--max-seconds', type=int, default=300, help='Soft limit checked between indexes')
    parser.add_argument('--lock-file', default='/run/lock/peergo-index-reclaim.lock')
    args = parser.parse_args()
    if not 1 <= args.max_indexes <= 3 or not 30 <= args.max_seconds <= 600:
        parser.error('max-indexes must be 1..3 and max-seconds 30..600')
    import fcntl
    with open(args.lock_file, 'a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            print(json.dumps({'status':'already_running'})); return
        started = time.monotonic()
        done = 0
        for name in args.database or list(TABLES):
            db = Database(args.container, name)
            for item in db.candidates():
                if not re.fullmatch(r'[a-z_][a-z_0-9]*\.[a-z_][a-z_0-9]*', item['name']):
                    raise RuntimeError('Unexpected index identifier')
                if done >= args.max_indexes or time.monotonic() - started >= args.max_seconds:
                    return
                if not item['valid'] or not item['ready']:
                    raise RuntimeError('Invalid index requires operator review: '+item['name'])
                if not enough_space(shutil.disk_usage('/').free, item['bytes']):
                    print(json.dumps({'status':'skipped_low_space','index':item['name']}));return
                if not args.execute:
                    print(json.dumps({'status':'candidate','database':name,**item}));continue
                stats = db.query(f"SELECT row_to_json(x) FROM pgstatindex('{item['name']}'::regclass) x;",120)
                if not sparse(stats):
                    continue
                # Recheck free disk after measurement; one index at a time.
                if not enough_space(shutil.disk_usage('/').free, item['bytes']):
                    return
                result=db.rebuild(item['name'])
                print(json.dumps({'status':'reclaimed','database':name,**result}),flush=True)
                done += 1

if __name__ == '__main__':
    main()
