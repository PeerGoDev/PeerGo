#!/usr/bin/env python3
"""Exercise real evidence pack migrations on a disposable PostgreSQL container.

No production connection is accepted. The caller supplies a Docker container
carrying the peergo.test.evidence-packs=true label and no network/port exposure.
"""
import argparse
import json
import subprocess
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--container', required=True)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[2])
    args = parser.parse_args()
    info = json.loads(subprocess.check_output(['docker', 'inspect', args.container]))[0]
    assert info['Config']['Labels'].get('peergo.test.evidence-packs') == 'true'
    assert info['HostConfig']['NetworkMode'] == 'none' and not info['HostConfig']['PortBindings']

    def sql(database, text):
        p = subprocess.run(['docker', 'exec', '-i', args.container, 'psql', '-X', '-qAt',
                            '-v', 'ON_ERROR_STOP=1', '-U', 'postgres', '-d', database],
                           input=text, text=True, capture_output=True, timeout=120)
        if p.returncode:
            raise RuntimeError(p.stderr[-5000:])
        return p.stdout.strip()

    for db, schema, stem, version in [('tracker', 'ledger', 'seeding_evidence', 'v1'),
                                      ('tracker', 'ledger', 'seeding_evidence', 'v3'),
                                      ('core', 'economy', 'seeding_reward_evidence', 'v1')]:
        name = 'pack_test_' + db + '_' + version
        sql('postgres', 'CREATE DATABASE ' + name)
        sql(name, '''CREATE TABLE goose_db_version(id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
                     version_id bigint NOT NULL,is_applied boolean NOT NULL,tstamp timestamp DEFAULT now());''')
        for path in sorted((args.root / 'db' / db / 'migrations').glob('*.sql')):
            source = path.read_text()
            up = source.split('-- +goose Up', 1)[1].split('-- +goose Down', 1)[0]
            if '-- +goose NO TRANSACTION' not in source:
                up = 'BEGIN;\n' + up + '\nCOMMIT;'
            try:
                sql(name, up)
                sql(name, 'INSERT INTO goose_db_version(version_id,is_applied) VALUES ('+path.name.split('_')[0]+',true)')
            except Exception as err:
                raise RuntimeError(str(path)+': '+str(err)) from err
        hot = f'{schema}.{stem}_items'
        packs = f'{schema}.{stem}_packs'
        fn = f'{schema}.{stem}'
        if db == 'tracker':
            fixture = '''
CREATE TEMP TABLE fixture AS SELECT date_trunc('hour',now()-interval '100 hours') h,
  gen_random_uuid() u,gen_random_uuid() a,gen_random_uuid() b;
INSERT INTO ledger.seeding_swarm_snapshots
 (snapshot_id,source_id,routing_epoch,snapshot_sequence,observed_at,chunk_count,received_chunk_count,status,created_at,completed_at)
SELECT a,'pack-fixture',1,1,h+interval '50 minutes',1,1,'complete',h,h+interval '50 minutes' FROM fixture
UNION ALL SELECT b,'pack-fixture',1,2,h+interval '60 minutes',1,1,'complete',h,h+interval '60 minutes' FROM fixture;
INSERT INTO ledger.seeding_evidence_windows
 (window_start,window_end,schema_version,announce_source_stream,announce_fence_sequence,announce_fence_received_at,
  selected_snapshot_id,selected_snapshot_sequence,selected_snapshot_observed_at,snapshot_fence_id,
  snapshot_fence_sequence,snapshot_fence_observed_at,item_count,evidence_sha256,built_at)
SELECT h,h+interval '1 hour','seeding.evidence.v1','PACK_TEST',1,h+interval '2 hours',
 a,1,h+interval '50 minutes',b,2,h+interval '60 minutes',3,sha256('window'),h+interval '2 hours' FROM fixture;
INSERT INTO ledger.seeding_evidence_items
 (window_start,user_id,torrent_id,info_hash_v1,active_seconds,raw_uploaded,source_interval_count,
 first_active_at,last_active_at,snapshot_seeders,snapshot_leechers,evidence_sha256,active_ranges,source_commitment)
SELECT h,u,n,substring(sha256(n::text::bytea) for 20),n*100,9223372036854775800-n,n,
 h+interval '0.123456 seconds',h+interval '55 minutes',n,0,sha256(n::text::bytea),
 NULL,NULL FROM fixture,generate_series(1,3) n;
'''
            if version == 'v3':
                fixture = fixture.replace("'seeding.evidence.v1'", "'seeding.evidence.v3'")
                fixture = fixture.replace('item_count,evidence_sha256,built_at)',
                                          'item_count,evidence_sha256,built_at,closure_delay_seconds,max_interval_credit_seconds)')
                fixture = fixture.replace("sha256('window'),h+interval '2 hours' FROM fixture;",
                                          "sha256('window'),h+interval '2 hours',2700,2100 FROM fixture;")
                fixture = fixture.replace(' NULL,NULL FROM fixture,generate_series(1,3) n;', '''
 tstzmultirange(tstzrange(h+interval '0.123456 seconds',h+interval '0.123456 seconds'+n*interval '50 seconds','[)'),
 tstzrange(h+interval '10 minutes',h+interval '10 minutes'+n*interval '50 seconds','[)')),
 sha256('commitment') FROM fixture,generate_series(1,3) n;''')
        else:
            fixture = '''
CREATE TEMP TABLE fixture AS SELECT date_trunc('hour',now()-interval '100 hours') h,gen_random_uuid() u;
INSERT INTO identity.users(id,credential_ref,username,display_name,status,email_verified_at,created_at,updated_at,password_changed_at)
SELECT u,gen_random_uuid(),'pack-test','Pack test','active',h,h,h,h FROM fixture;
INSERT INTO catalog.categories(id,name,display_order,enabled,created_at,updated_at)
SELECT 'pack-test','Pack test',900001,true,h,h FROM fixture;
CREATE TEMP TABLE objects AS SELECT n,gen_random_uuid() id FROM generate_series(1,3) n;
INSERT INTO torrents.torrent_objects(id,content_sha256,byte_length,parser_version,validation_profile,compatibility_flags,info_offset,info_length,created_at)
SELECT o.id,sha256(n::text::bytea),256,'integration-v1','strict_upload',ARRAY[]::text[],0,128,h FROM fixture,objects o;
INSERT INTO torrents.torrents(uploader_id,category_id,object_id,info_hash_v1,content_name,title,subtitle,total_size_bytes,payload_size_bytes,
 file_count,padding_file_count,piece_length_bytes,piece_count,state,version,submitted_at,published_at,state_changed_at,updated_at)
SELECT u,'pack-test',o.id,substring(sha256(n::text::bytea) for 20),'pack-test-'||n,'Pack test '||n,'',4096,4096,
 1,0,16384,1,'published',2,h-interval '1 hour',h,h,h FROM fixture,objects o;
INSERT INTO economy.seeding_reward_evidence_windows(window_start,window_end,built_at,window_evidence_sha256,projection_sha256,
 snapshot_id,snapshot_sequence,snapshot_observed_at,item_count,chunk_count,received_chunk_count,status,created_at,completed_at)
SELECT h,h+interval '1 hour',h+interval '2 hours',sha256('window'),sha256('projection'),gen_random_uuid(),1,h,
 3,1,1,'complete',h+interval '2 hours',h+interval '2 hours' FROM fixture;
INSERT INTO economy.seeding_reward_evidence_items(window_start,user_id,torrent_id,active_seconds,raw_uploaded_bytes,
 snapshot_seeders,snapshot_leechers,tracker_evidence_sha256)
SELECT h,u,t.id,o.n*100,9223372036854775800-o.n,o.n,0,sha256(o.n::text::bytea)
FROM fixture,objects o JOIN torrents.torrents t ON t.object_id=o.id;
'''
        columns = json.loads(sql(name, "SELECT json_agg(attname ORDER BY attnum) FROM pg_attribute WHERE attrelid='"+hot+"'::regclass AND attnum>0 AND NOT attisdropped"))
        arr = 'jsonb_build_array('+','.join('e.'+c for c in columns if c not in ('window_start', 'user_id'))+')'
        insert = f'''INSERT INTO {packs}(window_start,user_id,item_count,active_seconds,items,payload_sha256)
          SELECT window_start,user_id,count(*)::integer,sum(active_seconds)::bigint,
            jsonb_agg({arr} ORDER BY torrent_id),sha256(convert_to(jsonb_agg({arr} ORDER BY torrent_id)::text,'UTF8'))
          FROM {hot} e GROUP BY window_start,user_id;'''
        # Each negative case uses an exception subtransaction. An unexpected
        # success throws outside that subtransaction and fails the test.
        def denied(statement, label):
            return f'''DO $test$ DECLARE rejected boolean:=false; BEGIN
            BEGIN {statement} EXCEPTION WHEN OTHERS THEN rejected:=true; END;
            IF NOT rejected THEN RAISE EXCEPTION 'unsafe operation accepted: {label}'; END IF;
            END $test$;\n'''
        checks = fixture + f'CREATE TEMP TABLE expected AS TABLE {hot};\n'
        checks += denied(f'DELETE FROM {hot};', 'delete without pack')
        checks += denied(f'TRUNCATE {hot};', 'truncate hot evidence')
        checks += denied(f'UPDATE {hot} SET active_seconds=0;', 'change hot evidence')
        checks += denied(insert+' SET CONSTRAINTS ALL IMMEDIATE;', 'pack without retirement')
        checks += denied(insert+f' DELETE FROM {hot} WHERE torrent_id=(SELECT min(torrent_id) FROM {hot});', 'partial retirement')
        checks += denied(insert.replace('sum(active_seconds)::bigint', '0::bigint'), 'incorrect summary')
        checks += denied(insert.replace("sha256(convert_to(jsonb_agg("+arr+" ORDER BY torrent_id)::text,'UTF8'))", "sha256('wrong')"), 'incorrect digest')
        if db == 'tracker':
            checks += denied("INSERT INTO settlement.seeding_reconciliation_work(window_start,reason_code,detected_at) "
                             "SELECT h,'late_source_fact',now() FROM fixture;"+insert, 'reconciliation pin')
        checks += f'''DO $test$ BEGIN BEGIN
          {insert} DELETE FROM {hot}; RAISE EXCEPTION 'simulated interruption';
          EXCEPTION WHEN raise_exception THEN NULL; END;
          IF EXISTS(SELECT 1 FROM {packs}) OR (SELECT count(*) FROM {hot})<>3 THEN
            RAISE EXCEPTION 'interrupted move did not roll back'; END IF;
          END $test$;
          SELECT {fn}_pack_batch(64)=0 AS disabled_by_default;
          UPDATE {fn}_packing_control SET enabled=true;
          SET timezone='Asia/Shanghai';
          SELECT {fn}_pack_batch(1) AS moved;
          DO $test$ BEGIN
            IF (SELECT count(*) FROM {hot})<>0 OR (SELECT count(*) FROM {packs})<>1 THEN
              RAISE EXCEPTION 'batch did not move group'; END IF;
            IF EXISTS((SELECT * FROM expected EXCEPT ALL SELECT * FROM {hot}_read)
                UNION ALL (SELECT * FROM {hot}_read EXCEPT ALL SELECT * FROM expected)) THEN
              RAISE EXCEPTION 'lossless roundtrip failed'; END IF;
          END $test$;
        '''
        checks += denied(f'INSERT INTO {hot} SELECT * FROM expected;', 'late append')
        checks += denied(f'DELETE FROM {packs};', 'delete archive')
        checks += denied(f'TRUNCATE {packs};', 'truncate archive')
        checks += denied(f'UPDATE {packs} SET items=items;', 'mutate archive')
        if db == 'core':
            checks += '''DO $test$ BEGIN
            IF EXISTS((SELECT window_start,user_id,sum(active_seconds)::bigint active_seconds FROM expected GROUP BY window_start,user_id
                       EXCEPT ALL SELECT * FROM economy.seeding_reward_evidence_user_hours)
                      UNION ALL (SELECT * FROM economy.seeding_reward_evidence_user_hours EXCEPT ALL
                       SELECT window_start,user_id,sum(active_seconds)::bigint FROM expected GROUP BY window_start,user_id)) THEN
              RAISE EXCEPTION 'user/hour assessment summary changed'; END IF;
            IF (SELECT count(*) FROM economy.seeding_reward_evidence_torrent_refs)<>3 THEN
              RAISE EXCEPTION 'torrent references were not retained'; END IF;
            END $test$;'''
        # Keep fixture and checks in one session, but not one transaction:
        # constraint triggers are exercised at the real commit boundary.
        result = sql(name, checks)
        print(json.dumps({'database': name, 'migrations': 'passed', 'guards_and_roundtrip': 'passed',
                          'disabled_then_moved': result}), flush=True)


if __name__ == '__main__':
    main()
