-- +goose Up

-- Lossless, versioned user/hour packs. JSON arrays omit repeated column names,
-- window_start and user_id. All evidence fields (including hashes and ranges)
-- remain exact. Native PostgreSQL TOAST compresses the payload transparently.
-- Version 1 array positions: 0=torrent_id, 1=active_seconds, 2=raw_uploaded_bytes, 3=snapshot_seeders, 4=snapshot_leechers, 5=tracker_evidence_sha256.
CREATE TABLE economy.seeding_reward_evidence_packs (
    window_start timestamptz NOT NULL REFERENCES economy.seeding_reward_evidence_windows(window_start) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users(id) ON DELETE RESTRICT,
    format_version smallint NOT NULL DEFAULT 1 CHECK (format_version=1),
    item_count integer NOT NULL CHECK (item_count>0),
    active_seconds bigint NOT NULL CHECK (active_seconds>=0),
    items jsonb NOT NULL CHECK (jsonb_typeof(items)='array' AND jsonb_array_length(items)=item_count),
    payload_sha256 bytea NOT NULL CHECK (payload_sha256=sha256(convert_to(items::text,'UTF8'))),
    packed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(window_start,user_id)
) WITH (toast_tuple_target=128);
CREATE INDEX seeding_reward_evidence_packs_user_time_idx ON economy.seeding_reward_evidence_packs(user_id,window_start DESC);

-- Preserve the original torrent deletion restriction after hot rows are moved.
-- One reference per torrent replaces millions of repeated foreign keys.
CREATE TABLE economy.seeding_reward_evidence_torrent_refs (
    torrent_id bigint PRIMARY KEY REFERENCES torrents.torrents(id) ON DELETE RESTRICT
);
CREATE TRIGGER seeding_reward_evidence_torrent_refs_immutable
BEFORE UPDATE OR DELETE OR TRUNCATE ON economy.seeding_reward_evidence_torrent_refs
FOR EACH STATEMENT EXECUTE FUNCTION economy.reject_seeding_reward_evidence_mutation();

-- Disabled until every historical reader has been deployed. No evidence moves
-- merely by applying this migration; operators then enable this singleton.
CREATE TABLE economy.seeding_reward_evidence_packing_control (
    singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
    enabled boolean NOT NULL DEFAULT false
);
INSERT INTO economy.seeding_reward_evidence_packing_control(singleton) VALUES(true);

-- +goose StatementBegin
CREATE FUNCTION economy.seeding_reward_evidence_pack_validate_insert() RETURNS trigger
LANGUAGE plpgsql SET timezone='UTC' AS $$
DECLARE expected jsonb; expected_seconds bigint;
BEGIN
    PERFORM 1 FROM economy.seeding_reward_evidence_windows w WHERE w.window_start=NEW.window_start
        AND w.window_end < clock_timestamp()-interval '72 hours'
        AND w.status = 'complete' FOR UPDATE OF w;
    IF NOT FOUND THEN RAISE EXCEPTION 'evidence window is not eligible for packing'; END IF;
    SELECT jsonb_agg(jsonb_build_array(e.torrent_id, e.active_seconds, e.raw_uploaded_bytes, e.snapshot_seeders, e.snapshot_leechers, e.tracker_evidence_sha256) ORDER BY e.torrent_id),sum(e.active_seconds)::bigint
      INTO expected,expected_seconds FROM economy.seeding_reward_evidence_items e
      WHERE e.window_start=NEW.window_start AND e.user_id=NEW.user_id;
    IF expected IS NULL OR NEW.items IS DISTINCT FROM expected
       OR NEW.active_seconds IS DISTINCT FROM expected_seconds THEN
        RAISE EXCEPTION 'evidence pack does not exactly match hot evidence';
    END IF;
    INSERT INTO economy.seeding_reward_evidence_torrent_refs(torrent_id)
    SELECT torrent_id FROM economy.seeding_reward_evidence_items
    WHERE window_start=NEW.window_start AND user_id=NEW.user_id
    ON CONFLICT DO NOTHING;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER seeding_reward_evidence_pack_validate_insert BEFORE INSERT ON economy.seeding_reward_evidence_packs
FOR EACH ROW EXECUTE FUNCTION economy.seeding_reward_evidence_pack_validate_insert();
CREATE TRIGGER seeding_reward_evidence_packs_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON economy.seeding_reward_evidence_packs
FOR EACH STATEMENT EXECUTE FUNCTION economy.reject_seeding_reward_evidence_mutation();

-- +goose StatementBegin
CREATE FUNCTION economy.seeding_reward_evidence_pack_require_move() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS(SELECT 1 FROM economy.seeding_reward_evidence_items e WHERE e.window_start=NEW.window_start AND e.user_id=NEW.user_id) THEN
        RAISE EXCEPTION 'evidence pack and hot group cannot coexist at commit';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER seeding_reward_evidence_pack_require_move AFTER INSERT ON economy.seeding_reward_evidence_packs
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION economy.seeding_reward_evidence_pack_require_move();

-- +goose StatementBegin
CREATE FUNCTION economy.seeding_reward_evidence_reject_archived_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    -- Packs require an hour older than 72h. Recent ingest needs no extra lock.
    IF NEW.window_start < clock_timestamp()-interval '72 hours' THEN
        PERFORM 1 FROM economy.seeding_reward_evidence_windows w WHERE w.window_start=NEW.window_start FOR UPDATE;
        IF EXISTS(SELECT 1 FROM economy.seeding_reward_evidence_packs p WHERE p.window_start=NEW.window_start AND p.user_id=NEW.user_id) THEN
            RAISE EXCEPTION 'cannot append to archived evidence';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER seeding_reward_evidence_reject_archived_insert BEFORE INSERT ON economy.seeding_reward_evidence_items
FOR EACH ROW EXECUTE FUNCTION economy.seeding_reward_evidence_reject_archived_insert();

-- No permission flag can bypass this guard. A delete must move the complete
-- group byte-for-value into an immutable pack in the same transaction.
-- +goose StatementBegin
CREATE FUNCTION economy.seeding_reward_evidence_validate_packed_delete() RETURNS trigger
LANGUAGE plpgsql SET timezone='UTC' AS $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM (
            SELECT d.window_start,d.user_id,jsonb_agg(jsonb_build_array(d.torrent_id, d.active_seconds, d.raw_uploaded_bytes, d.snapshot_seeders, d.snapshot_leechers, d.tracker_evidence_sha256) ORDER BY d.torrent_id) AS items
            FROM deleted_evidence d GROUP BY d.window_start,d.user_id
        ) removed LEFT JOIN economy.seeding_reward_evidence_packs p USING(window_start,user_id)
        WHERE p.items IS DISTINCT FROM removed.items
    ) THEN RAISE EXCEPTION 'evidence deletion requires an exact immutable pack'; END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
DROP TRIGGER seeding_reward_evidence_items_immutable ON economy.seeding_reward_evidence_items;
CREATE TRIGGER seeding_reward_evidence_items_immutable BEFORE UPDATE OR TRUNCATE ON economy.seeding_reward_evidence_items
FOR EACH STATEMENT EXECUTE FUNCTION economy.reject_seeding_reward_evidence_mutation();
CREATE TRIGGER seeding_reward_evidence_items_packed_delete AFTER DELETE ON economy.seeding_reward_evidence_items
REFERENCING OLD TABLE AS deleted_evidence FOR EACH STATEMENT EXECUTE FUNCTION economy.seeding_reward_evidence_validate_packed_delete();

-- Parent keys come from the pack index so user/time filters can be pushed
-- below expansion. A snapshot sees the entire hot group or its entire pack.
CREATE VIEW economy.seeding_reward_evidence_items_read AS
SELECT * FROM economy.seeding_reward_evidence_items
UNION ALL
SELECT p.window_start,p.user_id,
    (item.value->>0)::bigint AS torrent_id,
    (item.value->>1)::bigint AS active_seconds,
    (item.value->>2)::bigint AS raw_uploaded_bytes,
    (item.value->>3)::integer AS snapshot_seeders,
    (item.value->>4)::integer AS snapshot_leechers,
    (item.value->>5)::bytea AS tracker_evidence_sha256
FROM economy.seeding_reward_evidence_packs p CROSS JOIN LATERAL jsonb_array_elements(p.items) AS item(value);

-- Routine assessments read exact user/hour sums without unpacking old proof.
CREATE VIEW economy.seeding_reward_evidence_user_hours AS
SELECT window_start, user_id, sum(active_seconds)::bigint AS active_seconds
FROM economy.seeding_reward_evidence_items GROUP BY window_start,user_id
UNION ALL
SELECT window_start,user_id,active_seconds FROM economy.seeding_reward_evidence_packs;

-- +goose StatementBegin
CREATE FUNCTION economy.seeding_reward_evidence_pack_batch(group_limit integer DEFAULT 64) RETURNS bigint
LANGUAGE plpgsql SET timezone='UTC' SET lock_timeout='2s' SET search_path=pg_catalog,public AS $$
DECLARE selected_hour timestamptz; selected_user uuid; moved bigint:=0; changed bigint;
BEGIN
    IF group_limit<1 OR group_limit>128 THEN RAISE EXCEPTION 'invalid evidence packing batch'; END IF;
    IF NOT (SELECT enabled FROM economy.seeding_reward_evidence_packing_control WHERE singleton) THEN RETURN 0; END IF;
    -- One packer per database. No queue that grows once per evidence row.
    IF NOT pg_try_advisory_xact_lock(hashtextextended('economy.seeding_reward_evidence_pack_batch',0)) THEN RETURN 0; END IF;
    SELECT w.window_start INTO selected_hour FROM economy.seeding_reward_evidence_windows w
      WHERE w.window_end < clock_timestamp()-interval '72 hours' AND w.status = 'complete'
        AND EXISTS(SELECT 1 FROM economy.seeding_reward_evidence_items e WHERE e.window_start=w.window_start)
      ORDER BY w.window_start LIMIT 1 FOR UPDATE OF w SKIP LOCKED;
    IF selected_hour IS NULL THEN RETURN 0; END IF;
    FOR selected_user IN SELECT user_id FROM economy.seeding_reward_evidence_items WHERE window_start=selected_hour
        GROUP BY user_id ORDER BY user_id LIMIT group_limit LOOP
        INSERT INTO economy.seeding_reward_evidence_packs(window_start,user_id,item_count,active_seconds,items,payload_sha256)
        SELECT selected_hour,selected_user,n,seconds,payload,sha256(convert_to(payload::text,'UTF8'))
        FROM (SELECT count(*)::integer n,sum(e.active_seconds)::bigint seconds,
              jsonb_agg(jsonb_build_array(e.torrent_id, e.active_seconds, e.raw_uploaded_bytes, e.snapshot_seeders, e.snapshot_leechers, e.tracker_evidence_sha256) ORDER BY e.torrent_id) payload
              FROM economy.seeding_reward_evidence_items e WHERE e.window_start=selected_hour AND e.user_id=selected_user) packed;
        DELETE FROM economy.seeding_reward_evidence_items WHERE window_start=selected_hour AND user_id=selected_user;
        GET DIAGNOSTICS changed = ROW_COUNT;
        moved := moved+changed;
    END LOOP;
    RETURN moved;
END;
$$;
-- +goose StatementEnd
-- Mutating maintenance is explicitly granted to the existing table owner,
-- not to arbitrary database users through PostgreSQL's PUBLIC function grant.
REVOKE ALL ON FUNCTION economy.seeding_reward_evidence_pack_batch(integer) FROM PUBLIC;

-- +goose Down
-- Packs are durable evidence. Downgrade requires an explicit tested unpack
-- migration; dropping them or silently hiding them would destroy history.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'evidence packing requires a forward recovery migration'; END $$;
-- +goose StatementEnd
