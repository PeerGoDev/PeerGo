#!/usr/bin/env python3
"""Exercise the actual Go SQL against an isolated PostgreSQL fixture only."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import uuid

p = argparse.ArgumentParser()
p.add_argument('--container', required=True)
p.add_argument('--root', type=Path, required=True)
p.add_argument('--references', type=Path, default=Path(__file__).with_name('fixtures'))
a = p.parse_args()
info = json.loads(subprocess.check_output(['docker', 'inspect', a.container]))[0]
assert info['Config']['Labels'].get('peergo.task') == 'operations-performance-20261017'
assert info['HostConfig']['NetworkMode'] == 'none'
assert not info['HostConfig']['PortBindings']
assert not any(m['Type'] in ('volume', 'bind') for m in info['Mounts'])
db = 'ops_monitor_query_test'


def sql(q, database=db):
    r = subprocess.run(['docker', 'exec', '-i', a.container, 'psql', '-X', '-qAt',
                        '-v', 'ON_ERROR_STOP=1', '-U', 'postgres', '-d', database],
                       input=q, text=True, capture_output=True, timeout=30)
    if r.returncode:
        raise RuntimeError(r.stderr)
    return r.stdout.strip()


sql('CREATE DATABASE ' + db, 'postgres')
sql('''
CREATE SCHEMA audit;
CREATE TABLE audit.outbox(event_id uuid PRIMARY KEY, attempts integer NOT NULL,
    lease_until timestamptz, delivered_at timestamptz, created_at timestamptz NOT NULL,
    last_error text, available_at timestamptz NOT NULL);
CREATE INDEX audit_outbox_pending_idx ON audit.outbox(available_at) WHERE delivered_at IS NULL;
CREATE SCHEMA workgroups;
CREATE TABLE workgroups.memberships(id uuid PRIMARY KEY,user_id uuid NOT NULL,group_kind text NOT NULL);
CREATE TABLE workgroups.membership_transitions(id uuid PRIMARY KEY,membership_id uuid NOT NULL,
    user_id uuid NOT NULL,group_kind text NOT NULL,to_status text NOT NULL,
    occurred_at timestamptz NOT NULL,state_version bigint NOT NULL);
CREATE SCHEMA economy;
CREATE TABLE economy.seeding_reward_evidence_windows(window_start timestamptz PRIMARY KEY,
    window_end timestamptz NOT NULL,status text NOT NULL);
CREATE TABLE economy.seeding_reward_evidence_items(window_start timestamptz NOT NULL,
    user_id uuid NOT NULL,torrent_id bigint NOT NULL,active_seconds bigint NOT NULL,
    PRIMARY KEY(window_start,user_id,torrent_id));
CREATE INDEX evidence_user_idx ON economy.seeding_reward_evidence_items(user_id,window_start,torrent_id);
CREATE TABLE economy.seeding_reward_evidence_packs(window_start timestamptz NOT NULL,
    user_id uuid NOT NULL,active_seconds bigint NOT NULL,PRIMARY KEY(window_start,user_id));
''')
view = (a.root / 'db/core/migrations/202610170001_core_evidence_packs.sql').read_text()
view = re.search(r'CREATE VIEW economy\.seeding_reward_evidence_user_hours AS[\s\S]+?;', view).group()
sql(view)
migration = (a.root / 'db/core/migrations/202610170002_core_operations_audit_indexes.sql').read_text()
up = migration.split('-- +goose Up', 1)[1].split('-- +goose Down', 1)[0]
down = migration.split('-- +goose Down', 1)[1]
# Concurrent index commands require separate top-level statements.
for part in up.split(';'):
    if part.strip():
        sql(part + ';')

audit = re.search(r'const auditQueueStatusSQL = `([\s\S]+?)`',
                 (a.root / 'services/core/internal/modules/operations/repository_postgres.go').read_text()).group(1)
contribution = re.search(r'const seedingContributionValuesSQL = `([\s\S]+?)`',
                 (a.root / 'services/core/internal/modules/workgroups/repository_postgres.go').read_text()).group(1)
old_audit = (a.references / 'audit-before.sql').read_text()
old_contribution = (a.references / 'contribution-before.sql').read_text()
as_of = "'2026-10-01T00:00:00Z'::timestamptz"


def audit_result(query):
    return json.loads(sql('WITH result(pending,processing,retrying,dead,completed,oldest,error,error_at) AS ('
                         + query.replace('$1', as_of) + ') SELECT row_to_json(result) FROM result'))


assert audit_result(audit) == audit_result(old_audit)
assert audit_result(audit)['completed'] == 0
sql('''
INSERT INTO audit.outbox
SELECT md5(n::text)::uuid,0,NULL,'2026-09-30Z','2026-09-29Z',NULL,'2026-09-30Z'
FROM generate_series(1,10000)n;
INSERT INTO audit.outbox VALUES
 ('00000000-0000-0000-0000-000000000001',0,NULL,NULL,'2026-09-28Z',NULL,'2026-09-30Z'),
 ('00000000-0000-0000-0000-000000000002',0,'2026-10-01Z',NULL,'2026-09-30Z',NULL,'2026-09-30Z'),
 ('00000000-0000-0000-0000-000000000003',1,'2026-10-01T01:00Z',NULL,'2026-09-30Z',NULL,'2026-09-30Z'),
 ('00000000-0000-0000-0000-000000000004',1,NULL,NULL,'2026-09-30Z','retry','2026-09-30Z'),
 ('00000000-0000-0000-0000-000000000005',2,'2026-10-01Z',NULL,'2026-09-30Z','expired','2026-09-30Z'),
 ('00000000-0000-0000-0000-000000000006',0,'2026-10-01T01:00Z',NULL,'2026-09-30Z',NULL,'2026-09-30Z'),
 ('00000000-0000-0000-0000-000000000007',3,NULL,'2026-09-30Z','2026-09-30Z',repeat('旧',70),'2026-10-01Z');
''')
r = audit_result(audit)
assert r == audit_result(old_audit), r
assert [r[k] for k in ('pending','processing','retrying','dead','completed')] == [2,2,2,0,10001], r
assert r['error'] == '旧'*64 and r['oldest'].startswith('2026-09-28'), r
print('PASS audit: empty queue, leases, expiry boundary, retries, exact completed count, historical errors', flush=True)

sql('''
INSERT INTO workgroups.memberships SELECT md5(('m'||n)::text)::uuid,md5(('u'||n)::text)::uuid,'retention'
FROM generate_series(1,3)n;
INSERT INTO economy.seeding_reward_evidence_windows
SELECT '2026-10-01Z'::timestamptz+n*interval '1 hour','2026-10-01Z'::timestamptz+(n+1)*interval '1 hour',
 CASE WHEN n=5 THEN 'collecting' ELSE 'complete' END FROM generate_series(-1,7)n;
INSERT INTO workgroups.membership_transitions
SELECT md5(('t'||n)::text)::uuid,md5('m1')::uuid,md5('u1')::uuid,'retention',status,
 '2026-10-01Z'::timestamptz+h*interval '1 hour',n
FROM (VALUES(1,0,'active'),(2,2,'suspended'),(3,4,'active'),(4,4,'suspended'),(5,4,'active'))v(n,h,status);
INSERT INTO workgroups.membership_transitions
SELECT md5('t6')::uuid,md5('m2')::uuid,md5('u2')::uuid,'retention','active','2026-10-01T01:00Z'::timestamptz,1
UNION ALL SELECT md5('t7')::uuid,md5('m3')::uuid,md5('u3')::uuid,'retention','active','2026-09-01Z',1
UNION ALL SELECT md5('t8')::uuid,md5('other')::uuid,md5('u1')::uuid,'review','suspended','2026-10-01T04:00Z',999;
INSERT INTO economy.seeding_reward_evidence_items
SELECT w.window_start,md5('u1')::uuid,n,n*10 FROM economy.seeding_reward_evidence_windows w,generate_series(1,2)n
WHERE w.window_start<>'2026-10-01T01:00Z';
INSERT INTO economy.seeding_reward_evidence_packs VALUES ('2026-10-01T01:00Z',md5('u1')::uuid,99);
INSERT INTO economy.seeding_reward_evidence_packs
SELECT w.window_start,md5('u2')::uuid,7 FROM economy.seeding_reward_evidence_windows w;
INSERT INTO economy.seeding_reward_evidence_items
SELECT w.window_start,md5('u3')::uuid,n,999 FROM economy.seeding_reward_evidence_windows w,generate_series(1,1000)n;
''')


def contribution_result(query, ids, end='2026-10-01T07:00:00Z'):
    query = query.replace('$1', ids).replace('$2', as_of).replace('$3', "'"+end+"'::timestamptz")
    return json.loads(sql('WITH result(id,value) AS ('+query+
                     ") SELECT coalesce(json_object_agg(id,value),'{}'::json) FROM result"))


ids = "ARRAY[md5('m1')::uuid,md5('m2')::uuid]"
for requested, end in [(ids,'2026-10-01T07:00:00Z'),(ids,'2026-10-01T06:30:00Z'),
                       ("ARRAY[]::uuid[]",'2026-10-01T07:00:00Z'),
                       ("NULL::uuid[]",'2026-10-01T07:00:00Z')]:
    assert contribution_result(contribution,requested,end) == contribution_result(old_contribution,requested,end)
r = contribution_result(contribution,ids)
uid = lambda value: str(uuid.UUID(hashlib.md5(value.encode()).hexdigest()))
assert r == {uid('m1'):189,uid('m2'):35}, r
print('PASS contribution: hot/packed equality, unrelated users, suspension/rejoin, transition ordering, complete hours, boundaries, empty/null IDs', flush=True)
# Verify the new compact count index is usable for an exact index-only scan.
sql('VACUUM ANALYZE audit.outbox')
plan = json.loads(sql('EXPLAIN (FORMAT JSON) SELECT count(*) FROM audit.outbox WHERE delivered_at IS NOT NULL'))
assert 'audit_outbox_completed_count_idx' in json.dumps(plan), plan
assert 'Index Only Scan' in json.dumps(plan), plan
for part in down.split(';'):
    if part.strip():
        sql(part + ';')
for part in up.split(';'):
    if part.strip():
        sql(part + ';')
print('PASS online index migration up/down/up; compact exact count access path', flush=True)
