-- +goose Up

-- Lossless, versioned user/hour packs. JSON arrays omit repeated column names,
-- window_start and user_id. All evidence fields (including hashes and ranges)
-- remain exact. Native PostgreSQL TOAST compresses the payload transparently.
-- Version 1 array positions: 0=torrent_id, 1=info_hash_v1, 2=active_seconds, 3=raw_uploaded, 4=source_interval_count, 5=first_active_at, 6=last_active_at, 7=snapshot_seeders, 8=snapshot_leechers, 9=evidence_sha256, 10=active_ranges, 11=source_commitment.
CREATE TABLE ledger.seeding_evidence_packs (
    window_start timestamptz NOT NULL REFERENCES ledger.seeding_evidence_windows(window_start) ON DELETE RESTRICT,
    user_id uuid NOT NULL,
    format_version smallint NOT NULL DEFAULT 1 CHECK (format_version=1),
    item_count integer NOT NULL CHECK (item_count>0),
    active_seconds bigint NOT NULL CHECK (active_seconds>=0),
    items jsonb NOT NULL CHECK (jsonb_typeof(items)='array' AND jsonb_array_length(items)=item_count),
    payload_sha256 bytea NOT NULL CHECK (payload_sha256=sha256(convert_to(items::text,'UTF8'))),
    packed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(window_start,user_id)
) WITH (toast_tuple_target=128);
CREATE INDEX seeding_evidence_packs_user_time_idx ON ledger.seeding_evidence_packs(user_id,window_start DESC);

-- Disabled until every historical reader has been deployed. No evidence moves
-- merely by applying this migration; operators then enable this singleton.
CREATE TABLE ledger.seeding_evidence_packing_control (
    singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
    enabled boolean NOT NULL DEFAULT false
);
INSERT INTO ledger.seeding_evidence_packing_control(singleton) VALUES(true);

-- +goose StatementBegin
CREATE FUNCTION ledger.seeding_evidence_pack_validate_insert() RETURNS trigger
LANGUAGE plpgsql SET timezone='UTC' AS $$
DECLARE expected jsonb; expected_seconds bigint;
BEGIN
    PERFORM 1 FROM ledger.seeding_evidence_windows w WHERE w.window_start=NEW.window_start
        AND w.window_end < clock_timestamp()-interval '72 hours'
        AND NOT EXISTS (SELECT 1 FROM ledger.seeding_evidence_sources s WHERE s.window_start=w.window_start)
        AND NOT EXISTS (SELECT 1 FROM ledger.seeding_evidence_anomalies a WHERE a.window_start=w.window_start)
        AND NOT EXISTS (SELECT 1 FROM settlement.seeding_reconciliation_work r WHERE r.window_start=w.window_start AND r.resolved_at IS NULL)
        AND NOT EXISTS (SELECT 1 FROM settlement.seeding_evidence_outbox o WHERE o.window_start=w.window_start AND o.published_at IS NULL) FOR UPDATE OF w;
    IF NOT FOUND THEN RAISE EXCEPTION 'evidence window is not eligible for packing'; END IF;
    SELECT jsonb_agg(jsonb_build_array(e.torrent_id, e.info_hash_v1, e.active_seconds, e.raw_uploaded, e.source_interval_count, e.first_active_at, e.last_active_at, e.snapshot_seeders, e.snapshot_leechers, e.evidence_sha256, e.active_ranges, e.source_commitment) ORDER BY e.torrent_id),sum(e.active_seconds)::bigint
      INTO expected,expected_seconds FROM ledger.seeding_evidence_items e
      WHERE e.window_start=NEW.window_start AND e.user_id=NEW.user_id;
    IF expected IS NULL OR NEW.items IS DISTINCT FROM expected
       OR NEW.active_seconds IS DISTINCT FROM expected_seconds THEN
        RAISE EXCEPTION 'evidence pack does not exactly match hot evidence';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER seeding_evidence_pack_validate_insert BEFORE INSERT ON ledger.seeding_evidence_packs
FOR EACH ROW EXECUTE FUNCTION ledger.seeding_evidence_pack_validate_insert();
CREATE TRIGGER seeding_evidence_packs_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON ledger.seeding_evidence_packs
FOR EACH STATEMENT EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();

-- +goose StatementBegin
CREATE FUNCTION ledger.seeding_evidence_pack_require_move() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS(SELECT 1 FROM ledger.seeding_evidence_items e WHERE e.window_start=NEW.window_start AND e.user_id=NEW.user_id) THEN
        RAISE EXCEPTION 'evidence pack and hot group cannot coexist at commit';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER seeding_evidence_pack_require_move AFTER INSERT ON ledger.seeding_evidence_packs
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION ledger.seeding_evidence_pack_require_move();

-- +goose StatementBegin
CREATE FUNCTION ledger.seeding_evidence_reject_archived_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    -- Packs require an hour older than 72h. Recent ingest needs no extra lock.
    IF NEW.window_start < clock_timestamp()-interval '72 hours' THEN
        PERFORM 1 FROM ledger.seeding_evidence_windows w WHERE w.window_start=NEW.window_start FOR UPDATE;
        IF EXISTS(SELECT 1 FROM ledger.seeding_evidence_packs p WHERE p.window_start=NEW.window_start AND p.user_id=NEW.user_id) THEN
            RAISE EXCEPTION 'cannot append to archived evidence';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER seeding_evidence_reject_archived_insert BEFORE INSERT ON ledger.seeding_evidence_items
FOR EACH ROW EXECUTE FUNCTION ledger.seeding_evidence_reject_archived_insert();

-- No permission flag can bypass this guard. A delete must move the complete
-- group byte-for-value into an immutable pack in the same transaction.
-- +goose StatementBegin
CREATE FUNCTION ledger.seeding_evidence_validate_packed_delete() RETURNS trigger
LANGUAGE plpgsql SET timezone='UTC' AS $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM (
            SELECT d.window_start,d.user_id,jsonb_agg(jsonb_build_array(d.torrent_id, d.info_hash_v1, d.active_seconds, d.raw_uploaded, d.source_interval_count, d.first_active_at, d.last_active_at, d.snapshot_seeders, d.snapshot_leechers, d.evidence_sha256, d.active_ranges, d.source_commitment) ORDER BY d.torrent_id) AS items
            FROM deleted_evidence d GROUP BY d.window_start,d.user_id
        ) removed LEFT JOIN ledger.seeding_evidence_packs p USING(window_start,user_id)
        WHERE p.items IS DISTINCT FROM removed.items
    ) THEN RAISE EXCEPTION 'evidence deletion requires an exact immutable pack'; END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
DROP TRIGGER seeding_evidence_items_immutable ON ledger.seeding_evidence_items;
CREATE TRIGGER seeding_evidence_items_immutable BEFORE UPDATE OR TRUNCATE ON ledger.seeding_evidence_items
FOR EACH STATEMENT EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();
CREATE TRIGGER seeding_evidence_items_packed_delete AFTER DELETE ON ledger.seeding_evidence_items
REFERENCING OLD TABLE AS deleted_evidence FOR EACH STATEMENT EXECUTE FUNCTION ledger.seeding_evidence_validate_packed_delete();

-- Parent keys come from the pack index so user/time filters can be pushed
-- below expansion. A snapshot sees the entire hot group or its entire pack.
CREATE VIEW ledger.seeding_evidence_items_read AS
SELECT * FROM ledger.seeding_evidence_items
UNION ALL
SELECT p.window_start,p.user_id,
    (item.value->>0)::bigint AS torrent_id,
    (item.value->>1)::bytea AS info_hash_v1,
    (item.value->>2)::bigint AS active_seconds,
    (item.value->>3)::bigint AS raw_uploaded,
    (item.value->>4)::integer AS source_interval_count,
    (item.value->>5)::timestamptz AS first_active_at,
    (item.value->>6)::timestamptz AS last_active_at,
    (item.value->>7)::integer AS snapshot_seeders,
    (item.value->>8)::integer AS snapshot_leechers,
    (item.value->>9)::bytea AS evidence_sha256,
    (item.value->>10)::tstzmultirange AS active_ranges,
    (item.value->>11)::bytea AS source_commitment
FROM ledger.seeding_evidence_packs p CROSS JOIN LATERAL jsonb_array_elements(p.items) AS item(value);

-- +goose StatementBegin
CREATE FUNCTION ledger.seeding_evidence_pack_batch(group_limit integer DEFAULT 64) RETURNS bigint
LANGUAGE plpgsql SET timezone='UTC' SET lock_timeout='2s' SET search_path=pg_catalog,public AS $$
DECLARE selected_hour timestamptz; selected_user uuid; moved bigint:=0; changed bigint;
BEGIN
    IF group_limit<1 OR group_limit>128 THEN RAISE EXCEPTION 'invalid evidence packing batch'; END IF;
    IF NOT (SELECT enabled FROM ledger.seeding_evidence_packing_control WHERE singleton) THEN RETURN 0; END IF;
    -- One packer per database. No queue that grows once per evidence row.
    IF NOT pg_try_advisory_xact_lock(hashtextextended('ledger.seeding_evidence_pack_batch',0)) THEN RETURN 0; END IF;
    SELECT w.window_start INTO selected_hour FROM ledger.seeding_evidence_windows w
      WHERE w.window_end < clock_timestamp()-interval '72 hours' AND NOT EXISTS (SELECT 1 FROM ledger.seeding_evidence_sources s WHERE s.window_start=w.window_start)
        AND NOT EXISTS (SELECT 1 FROM ledger.seeding_evidence_anomalies a WHERE a.window_start=w.window_start)
        AND NOT EXISTS (SELECT 1 FROM settlement.seeding_reconciliation_work r WHERE r.window_start=w.window_start AND r.resolved_at IS NULL)
        AND NOT EXISTS (SELECT 1 FROM settlement.seeding_evidence_outbox o WHERE o.window_start=w.window_start AND o.published_at IS NULL)
        AND EXISTS(SELECT 1 FROM ledger.seeding_evidence_items e WHERE e.window_start=w.window_start)
      ORDER BY w.window_start LIMIT 1 FOR UPDATE OF w SKIP LOCKED;
    IF selected_hour IS NULL THEN RETURN 0; END IF;
    FOR selected_user IN SELECT user_id FROM ledger.seeding_evidence_items WHERE window_start=selected_hour
        GROUP BY user_id ORDER BY user_id LIMIT group_limit LOOP
        INSERT INTO ledger.seeding_evidence_packs(window_start,user_id,item_count,active_seconds,items,payload_sha256)
        SELECT selected_hour,selected_user,n,seconds,payload,sha256(convert_to(payload::text,'UTF8'))
        FROM (SELECT count(*)::integer n,sum(e.active_seconds)::bigint seconds,
              jsonb_agg(jsonb_build_array(e.torrent_id, e.info_hash_v1, e.active_seconds, e.raw_uploaded, e.source_interval_count, e.first_active_at, e.last_active_at, e.snapshot_seeders, e.snapshot_leechers, e.evidence_sha256, e.active_ranges, e.source_commitment) ORDER BY e.torrent_id) payload
              FROM ledger.seeding_evidence_items e WHERE e.window_start=selected_hour AND e.user_id=selected_user) packed;
        DELETE FROM ledger.seeding_evidence_items WHERE window_start=selected_hour AND user_id=selected_user;
        GET DIAGNOSTICS changed = ROW_COUNT;
        moved := moved+changed;
    END LOOP;
    RETURN moved;
END;
$$;
-- +goose StatementEnd
-- Mutating maintenance is explicitly granted to the existing table owner,
-- not to arbitrary database users through PostgreSQL's PUBLIC function grant.
REVOKE ALL ON FUNCTION ledger.seeding_evidence_pack_batch(integer) FROM PUBLIC;

-- +goose Down
-- Packs are durable evidence. Downgrade requires an explicit tested unpack
-- migration; dropping them or silently hiding them would destroy history.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'evidence packing requires a forward recovery migration'; END $$;
-- +goose StatementEnd
