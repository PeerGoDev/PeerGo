-- +goose Up

-- A small, rebuildable cache for active consumers of exact user/hour totals.
-- Packed evidence already has this summary. Cache only requested hot groups;
-- retire cache rows atomically when their hot evidence is packed/deleted.
CREATE TABLE economy.seeding_reward_activity_cache (
    window_start timestamptz NOT NULL REFERENCES economy.seeding_reward_evidence_windows(window_start) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users(id) ON DELETE RESTRICT,
    item_count integer NOT NULL CHECK(item_count > 0),
    active_seconds bigint NOT NULL CHECK(active_seconds >= 0),
    PRIMARY KEY(window_start,user_id)
);

-- +goose StatementBegin
CREATE FUNCTION economy.validate_seeding_reward_activity_cache() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
DECLARE n bigint; seconds bigint;
BEGIN
    SELECT count(*),sum(e.active_seconds)::bigint INTO n,seconds
    FROM economy.seeding_reward_evidence_items e
    WHERE e.window_start=NEW.window_start AND e.user_id=NEW.user_id;
    IF n=0 OR n<>NEW.item_count OR seconds IS DISTINCT FROM NEW.active_seconds THEN
        RAISE EXCEPTION 'activity cache does not match immutable hot evidence';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER seeding_reward_activity_cache_exact
BEFORE INSERT OR UPDATE ON economy.seeding_reward_activity_cache
FOR EACH ROW EXECUTE FUNCTION economy.validate_seeding_reward_activity_cache();

-- +goose StatementBegin
CREATE FUNCTION economy.invalidate_seeding_reward_activity_cache() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        DELETE FROM economy.seeding_reward_activity_cache c
        USING (SELECT DISTINCT window_start,user_id FROM activity_inserted) changed
        WHERE c.window_start=changed.window_start AND c.user_id=changed.user_id;
    ELSE
        DELETE FROM economy.seeding_reward_activity_cache c
        USING (SELECT DISTINCT window_start,user_id FROM activity_deleted) changed
        WHERE c.window_start=changed.window_start AND c.user_id=changed.user_id;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER seeding_reward_activity_cache_after_insert
AFTER INSERT ON economy.seeding_reward_evidence_items
REFERENCING NEW TABLE AS activity_inserted
FOR EACH STATEMENT EXECUTE FUNCTION economy.invalidate_seeding_reward_activity_cache();
CREATE TRIGGER seeding_reward_activity_cache_after_delete
AFTER DELETE ON economy.seeding_reward_evidence_items
REFERENCING OLD TABLE AS activity_deleted
FOR EACH STATEMENT EXECUTE FUNCTION economy.invalidate_seeding_reward_activity_cache();

-- +goose StatementBegin
CREATE FUNCTION economy.fill_completed_seeding_reward_activity_cache() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,public AS $$
BEGIN
    IF OLD.status='collecting' AND NEW.status='complete' THEN
        INSERT INTO economy.seeding_reward_activity_cache(window_start,user_id,item_count,active_seconds)
        SELECT e.window_start,e.user_id,count(*)::integer,sum(e.active_seconds)::bigint
        FROM economy.seeding_reward_evidence_items e
        WHERE e.window_start=NEW.window_start
          AND e.user_id IN (
            SELECT user_id FROM workgroups.memberships WHERE group_kind='retention'
            UNION
            SELECT user_id FROM newcomer.assessments WHERE status IN('active','download_restricted')
          )
        GROUP BY e.window_start,e.user_id
        ON CONFLICT(window_start,user_id) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER seeding_reward_completed_activity_cache
AFTER UPDATE OF status ON economy.seeding_reward_evidence_windows
FOR EACH ROW EXECUTE FUNCTION economy.fill_completed_seeding_reward_activity_cache();

-- The ordered lookup is deliberately procedural: a valid small cache/pack hit
-- must not execute or plan an aggregate across the hot evidence history.
-- +goose StatementBegin
CREATE FUNCTION economy.seeding_reward_user_hour_active_seconds(wanted_user uuid,wanted_hour timestamptz)
RETURNS bigint LANGUAGE plpgsql STABLE COST 1 SET search_path=pg_catalog,public AS $$
DECLARE seconds bigint;
BEGIN
    SELECT c.active_seconds INTO seconds FROM economy.seeding_reward_activity_cache c
    WHERE c.window_start=wanted_hour AND c.user_id=wanted_user;
    IF FOUND THEN RETURN seconds; END IF;
    SELECT p.active_seconds INTO seconds FROM economy.seeding_reward_evidence_packs p
    WHERE p.window_start=wanted_hour AND p.user_id=wanted_user;
    IF FOUND THEN RETURN seconds; END IF;
    SELECT sum(e.active_seconds)::bigint INTO seconds FROM economy.seeding_reward_evidence_items e
    WHERE e.window_start=wanted_hour AND e.user_id=wanted_user;
    RETURN seconds;
END;
$$;
-- +goose StatementEnd

-- Backfill only consumers that need the totals; never enqueue one task per
-- original torrent item. Existing complete evidence remains the source of truth.
-- +goose StatementBegin
CREATE FUNCTION economy.refresh_seeding_reward_activity_cache(group_limit integer DEFAULT 128)
RETURNS bigint LANGUAGE plpgsql SET timezone='UTC' SET search_path=pg_catalog,public AS $$
DECLARE target record; changed bigint; total bigint:=0;
BEGIN
    IF group_limit<1 OR group_limit>128 THEN RAISE EXCEPTION 'invalid activity cache batch'; END IF;
    IF NOT pg_try_advisory_xact_lock(hashtextextended('economy.seeding_reward_activity_cache',0)) THEN RETURN 0; END IF;
    FOR target IN
      WITH consumers AS MATERIALIZED (
        SELECT user_id,min(starts_at) starts_at FROM (
          SELECT user_id,date_trunc('month',statement_timestamp())-interval '1 month' starts_at
          FROM workgroups.memberships WHERE group_kind='retention'
          UNION ALL
          SELECT user_id,started_at FROM newcomer.assessments WHERE status IN('active','download_restricted')
        ) c GROUP BY user_id
      )
      SELECT w.window_start,c.user_id
      FROM consumers c JOIN economy.seeding_reward_evidence_windows w
        ON w.status='complete' AND w.window_start>=c.starts_at
      WHERE (SELECT true FROM economy.seeding_reward_activity_cache a
             WHERE a.window_start=w.window_start AND a.user_id=c.user_id LIMIT 1 OFFSET 0) IS NULL
        AND (SELECT true FROM economy.seeding_reward_evidence_packs p
             WHERE p.window_start=w.window_start AND p.user_id=c.user_id LIMIT 1 OFFSET 0) IS NULL
        AND (SELECT true FROM economy.seeding_reward_evidence_items e
                   WHERE e.window_start=w.window_start AND e.user_id=c.user_id LIMIT 1 OFFSET 0) IS TRUE
      ORDER BY w.window_start DESC,c.user_id
      LIMIT group_limit FOR SHARE OF w SKIP LOCKED
    LOOP
      INSERT INTO economy.seeding_reward_activity_cache(window_start,user_id,item_count,active_seconds)
      SELECT e.window_start,e.user_id,count(*)::integer,sum(e.active_seconds)::bigint
      FROM economy.seeding_reward_evidence_items e
      WHERE e.window_start=target.window_start AND e.user_id=target.user_id
      GROUP BY e.window_start,e.user_id
      ON CONFLICT(window_start,user_id) DO NOTHING;
      GET DIAGNOSTICS changed=ROW_COUNT; total:=total+changed;
    END LOOP;
    RETURN total;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON economy.seeding_reward_activity_cache FROM PUBLIC;
REVOKE ALL ON FUNCTION economy.refresh_seeding_reward_activity_cache(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION economy.seeding_reward_user_hour_active_seconds(uuid,timestamptz) FROM PUBLIC;

-- +goose Down
DROP TRIGGER seeding_reward_completed_activity_cache ON economy.seeding_reward_evidence_windows;
DROP TRIGGER seeding_reward_activity_cache_after_delete ON economy.seeding_reward_evidence_items;
DROP TRIGGER seeding_reward_activity_cache_after_insert ON economy.seeding_reward_evidence_items;
DROP FUNCTION economy.refresh_seeding_reward_activity_cache(integer);
DROP FUNCTION economy.seeding_reward_user_hour_active_seconds(uuid,timestamptz);
DROP FUNCTION economy.fill_completed_seeding_reward_activity_cache();
DROP FUNCTION economy.invalidate_seeding_reward_activity_cache();
DROP TABLE economy.seeding_reward_activity_cache;
DROP FUNCTION economy.validate_seeding_reward_activity_cache();
