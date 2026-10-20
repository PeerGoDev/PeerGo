-- Run only inside the disposable Core fixture, after lossless pack checks.
BEGIN;
CREATE TEMP TABLE new_window AS SELECT window_start+interval '1 hour' h,user_id u
FROM economy.seeding_reward_evidence_packs LIMIT 1;
INSERT INTO economy.seeding_reward_evidence_windows(window_start,window_end,built_at,
 window_evidence_sha256,projection_sha256,snapshot_id,snapshot_sequence,snapshot_observed_at,
 item_count,chunk_count,received_chunk_count,status,created_at,completed_at)
SELECT h,h+interval '1 hour',h+interval '2 hours',sha256('new-window'),sha256('new-projection'),
 gen_random_uuid(),1,h,3,1,0,'collecting',h+interval '2 hours',NULL FROM new_window;
INSERT INTO economy.seeding_reward_evidence_items
SELECT w.h,w.u,t.id,(row_number() OVER(ORDER BY t.id))*100,0,1,0,sha256('new-item')
FROM new_window w,torrents.torrents t ORDER BY t.id LIMIT 2;
INSERT INTO economy.seeding_reward_activity_cache SELECT h,u,2,300 FROM new_window;
INSERT INTO economy.seeding_reward_evidence_items
SELECT w.h,w.u,t.id,300,0,1,0,sha256('new-item')
FROM new_window w,torrents.torrents t ORDER BY t.id DESC LIMIT 1;
DO $test$ BEGIN
 IF EXISTS(SELECT 1 FROM economy.seeding_reward_activity_cache) THEN
  RAISE EXCEPTION 'late item left a stale cache'; END IF;
 IF economy.seeding_reward_user_hour_active_seconds((SELECT u FROM new_window),(SELECT h FROM new_window))<>600 THEN
  RAISE EXCEPTION 'fallback after invalidation changed'; END IF;
END $test$;
UPDATE economy.seeding_reward_evidence_windows SET status='complete',received_chunk_count=1,completed_at=built_at
WHERE window_start=(SELECT h FROM new_window);
DO $test$ BEGIN
 IF (SELECT active_seconds FROM economy.seeding_reward_activity_cache)<>600 OR
    (SELECT item_count FROM economy.seeding_reward_activity_cache)<>3 THEN
  RAISE EXCEPTION 'completion did not build the exact cache'; END IF;
END $test$;
ROLLBACK;
SELECT 'PASS late append invalidation, live completion summary, fallback exactness';
