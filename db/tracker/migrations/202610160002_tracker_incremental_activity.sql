-- +goose Up

-- Only hours that start after registration use the new representation. The
-- configured credible gap is immutable so historical decisions remain exact.
CREATE TABLE settlement.activity_projection_profiles (
    source_stream text PRIMARY KEY,
    activates_at timestamptz NOT NULL,
    max_interval_credit_seconds integer NOT NULL CHECK (max_interval_credit_seconds BETWEEN 60 AND 3600),
    shadow_verified_windows integer NOT NULL DEFAULT 0 CHECK (shadow_verified_windows >= 0),
    required_shadow_windows integer NOT NULL DEFAULT 3 CHECK (required_shadow_windows BETWEEN 1 AND 168),
    CHECK (activates_at = date_trunc('hour', activates_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')
);
CREATE TABLE settlement.seeding_activity_hours (
    source_stream text NOT NULL REFERENCES settlement.activity_projection_profiles(source_stream),
    window_start timestamptz NOT NULL,
    user_id uuid NOT NULL,
    torrent_id bigint NOT NULL CHECK (torrent_id > 0),
    info_hash_v1 bytea NOT NULL CHECK (octet_length(info_hash_v1) = 20),
    active_ranges tstzmultirange NOT NULL,
    raw_uploaded bigint NOT NULL CHECK (raw_uploaded >= 0),
    source_count bigint NOT NULL CHECK (source_count > 0),
    source_commitment bytea NOT NULL CHECK (octet_length(source_commitment) = 32),
    last_source_sequence bigint NOT NULL CHECK (last_source_sequence > 0),
    PRIMARY KEY (source_stream, window_start, user_id, torrent_id)
);
ALTER TABLE settlement.seeding_activity_hours SET (
    autovacuum_vacuum_scale_factor=0.02, autovacuum_vacuum_threshold=1000
);
ALTER TABLE ledger.seeding_evidence_items
    ADD COLUMN active_ranges tstzmultirange,
    ADD COLUMN source_commitment bytea,
    ADD CONSTRAINT seeding_compact_item_proof CHECK (
        (active_ranges IS NULL AND source_commitment IS NULL)
        OR (active_ranges IS NOT NULL AND source_commitment IS NOT NULL AND octet_length(source_commitment)=32)
    );
ALTER TABLE ledger.seeding_evidence_windows
    DROP CONSTRAINT seeding_evidence_windows_schema_policy_check;
ALTER TABLE ledger.seeding_evidence_windows
    ADD CONSTRAINT seeding_evidence_windows_schema_policy_check CHECK (
        (schema_version='seeding.evidence.v1' AND closure_delay_seconds IS NULL AND max_interval_credit_seconds IS NULL)
        OR (
            schema_version IN ('seeding.evidence.v2','seeding.evidence.v3')
            AND closure_delay_seconds BETWEEN 60 AND 3600
            AND max_interval_credit_seconds BETWEEN 60 AND 3600
            AND closure_delay_seconds >= max_interval_credit_seconds
            AND announce_fence_received_at >= window_end+(closure_delay_seconds*interval '1 second')
            AND built_at >= window_end+(closure_delay_seconds*interval '1 second')
        )
    );

-- +goose StatementBegin
CREATE FUNCTION ledger.activity_source_hash(event_id uuid, source_sequence bigint,
    starts_at timestamptz, ends_at timestamptz, raw_uploaded bigint)
RETURNS bytea LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT sha256(uuid_send(event_id)||int8send(source_sequence)
        ||timestamptz_send(starts_at)||timestamptz_send(ends_at)||int8send(raw_uploaded));
$$;
-- +goose StatementEnd

-- The ingest transaction also advances its durable stream cursor. A rollback
-- therefore loses neither source progress nor the matching activity state.
-- +goose StatementBegin
CREATE FUNCTION settlement.fold_seeding_activity()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    stream text;
    sequence bigint;
    profile settlement.activity_projection_profiles%ROWTYPE;
    hour_start timestamptz;
    clipped_start timestamptz;
    clipped_end timestamptz;
    leaf bytea;
    affected integer;
BEGIN
    IF NEW.previous_left<>0 OR NEW.current_left<>0 THEN RETURN NEW; END IF;
    stream:=NEW.source_stream;
    sequence:=NEW.source_sequence;
    IF stream IS NULL OR sequence IS NULL THEN
        SELECT source_stream,source_sequence INTO stream,sequence
        FROM settlement.event_inbox WHERE event_id=NEW.event_id;
    END IF;
    SELECT * INTO profile FROM settlement.activity_projection_profiles WHERE source_stream=stream;
    IF NOT FOUND OR NEW.ends_at>NEW.starts_at+(profile.max_interval_credit_seconds*interval '1 second') THEN
        RETURN NEW;
    END IF;
    FOR hour_start IN
        SELECT h FROM generate_series(
            date_trunc('hour',NEW.starts_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC',
            date_trunc('hour',(NEW.ends_at-interval '1 microsecond') AT TIME ZONE 'UTC') AT TIME ZONE 'UTC',
            interval '1 hour') AS h WHERE h>=profile.activates_at
    LOOP
        clipped_start:=greatest(NEW.starts_at,hour_start);
        clipped_end:=least(NEW.ends_at,hour_start+interval '1 hour');
        leaf:=ledger.activity_source_hash(NEW.event_id,sequence,clipped_start,clipped_end,NEW.raw_uploaded);
        INSERT INTO settlement.seeding_activity_hours AS existing (
            source_stream,window_start,user_id,torrent_id,info_hash_v1,active_ranges,
            raw_uploaded,source_count,source_commitment,last_source_sequence
        ) VALUES (
            stream,hour_start,NEW.user_id,NEW.torrent_id,NEW.info_hash_v1,
            tstzmultirange(tstzrange(clipped_start,clipped_end,'[)')),
            NEW.raw_uploaded,1,sha256(decode(repeat('00',32),'hex')||leaf),sequence
        ) ON CONFLICT (source_stream,window_start,user_id,torrent_id) DO UPDATE SET
            active_ranges=existing.active_ranges+EXCLUDED.active_ranges,
            raw_uploaded=existing.raw_uploaded+EXCLUDED.raw_uploaded,
            source_count=existing.source_count+1,
            source_commitment=sha256(existing.source_commitment||leaf),
            last_source_sequence=EXCLUDED.last_source_sequence
        WHERE existing.info_hash_v1=EXCLUDED.info_hash_v1
          AND existing.last_source_sequence<EXCLUDED.last_source_sequence;
        GET DIAGNOSTICS affected=ROW_COUNT;
        IF affected<>1 THEN RAISE EXCEPTION 'compact activity identity or source ordering conflict'; END IF;
    END LOOP;
    INSERT INTO settlement.raw_interval_gc_ranges(window_start,cursor_ends_at,retry_after)
    SELECT h,h,h+interval '49 hours'
    FROM generate_series(
        date_trunc('hour',greatest(NEW.starts_at,profile.activates_at) AT TIME ZONE 'UTC') AT TIME ZONE 'UTC',
        date_trunc('hour',(NEW.ends_at-interval '1 microsecond') AT TIME ZONE 'UTC') AT TIME ZONE 'UTC',
        interval '1 hour') h ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER raw_session_interval_compact_activity
AFTER INSERT ON ledger.raw_session_intervals
FOR EACH ROW EXECUTE FUNCTION settlement.fold_seeding_activity();

CREATE TABLE settlement.hnr_progress_accumulators (
    obligation_id uuid PRIMARY KEY REFERENCES ledger.hnr_obligations(id),
    state_json jsonb NOT NULL,
    source_cursors jsonb NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (jsonb_typeof(state_json)='object' AND jsonb_typeof(source_cursors)='object')
);

CREATE TABLE settlement.seeding_window_scan (
    source_stream text PRIMARY KEY,
    next_window timestamptz NOT NULL
);
CREATE TABLE settlement.seeding_window_retries (
    source_stream text NOT NULL,
    window_start timestamptz NOT NULL,
    retry_after timestamptz NOT NULL,
    first_seen_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    attempts bigint NOT NULL DEFAULT 1 CHECK (attempts>0),
    reason_code text NOT NULL CHECK (reason_code='source_coverage_pending'),
    PRIMARY KEY(source_stream,window_start)
);
CREATE INDEX seeding_window_retry_ready_idx ON settlement.seeding_window_retries(source_stream,retry_after,window_start);

CREATE TABLE settlement.seeding_reconciliation_work (
    window_start timestamptz PRIMARY KEY REFERENCES ledger.seeding_evidence_windows(window_start),
    reason_code text NOT NULL CHECK(reason_code IN ('historical_coverage_recovered','late_source_fact')),
    detected_at timestamptz NOT NULL,
    resolved_at timestamptz,
    resolution_reference text,
    CHECK((resolved_at IS NULL AND resolution_reference IS NULL)
       OR (resolved_at IS NOT NULL AND length(resolution_reference) BETWEEN 1 AND 256))
);


-- Gzip is bounded by the canonical source contract (512 KiB). Hashes always
-- cover the uncompressed canonical message, not an opaque compressed file.
ALTER TABLE ledger.seeding_swarm_snapshot_chunks
    ADD COLUMN compact_payload boolean NOT NULL DEFAULT false;
CREATE TABLE ledger.seeding_snapshot_payloads (
    snapshot_id uuid NOT NULL,
    chunk_index integer NOT NULL,
    payload_gzip bytea NOT NULL,
    uncompressed_bytes integer NOT NULL CHECK (uncompressed_bytes BETWEEN 2 AND 524288),
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256)=32),
    PRIMARY KEY(snapshot_id,chunk_index),
    FOREIGN KEY(snapshot_id,chunk_index) REFERENCES ledger.seeding_swarm_snapshot_chunks(snapshot_id,chunk_index)
);
ALTER TABLE ledger.seeding_snapshot_payloads ALTER COLUMN payload_gzip SET STORAGE EXTERNAL;

-- Preserve a genuinely contiguous watermark for old metadata guards. The new
-- row-level GC checks complete coverage of each interval instead of trusting
-- the maximum successful hour across an unresolved hole.
-- +goose StatementBegin
CREATE FUNCTION ledger.seeding_closed_through()
RETURNS timestamptz LANGUAGE sql STABLE AS $$
    WITH bounds AS (
        SELECT least(min(window_start),
            (SELECT min(window_start) FROM settlement.seeding_window_retries)) AS first_hour,
            max(window_end) AS last_end
        FROM ledger.seeding_evidence_windows
    )
    SELECT coalesce((
        SELECT min(h) FROM bounds,generate_series(first_hour,last_end-interval '1 hour',interval '1 hour') h
        WHERE NOT EXISTS (SELECT 1 FROM ledger.seeding_evidence_windows w WHERE w.window_start=h)
    ),last_end) FROM bounds;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION ledger.seeding_interval_is_closed(starts_at timestamptz,ends_at timestamptz)
RETURNS boolean LANGUAGE sql STABLE STRICT AS $$
    SELECT ends_at>starts_at AND
        (SELECT count(*) FROM ledger.seeding_evidence_windows AS w
         WHERE w.window_start>=date_trunc('hour',starts_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
           AND w.window_start<ends_at)
        = (extract(epoch FROM (
            (date_trunc('hour',(ends_at-interval '1 microsecond') AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')
            -(date_trunc('hour',starts_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')
          ))::bigint/3600)+1;
$$;
-- +goose StatementEnd


-- +goose StatementBegin
CREATE FUNCTION settlement.protect_activity_profile()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE'
       OR NEW.source_stream IS DISTINCT FROM OLD.source_stream
       OR NEW.activates_at IS DISTINCT FROM OLD.activates_at
       OR NEW.max_interval_credit_seconds IS DISTINCT FROM OLD.max_interval_credit_seconds
       OR NEW.required_shadow_windows IS DISTINCT FROM OLD.required_shadow_windows
       OR NEW.shadow_verified_windows<OLD.shadow_verified_windows THEN
        RAISE EXCEPTION 'activity projection policy is immutable and validation cannot regress';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER activity_profile_policy BEFORE UPDATE OR DELETE ON settlement.activity_projection_profiles
FOR EACH ROW EXECUTE FUNCTION settlement.protect_activity_profile();

-- +goose StatementBegin
CREATE FUNCTION ledger.raw_seeding_is_compacted(raw ledger.raw_session_intervals)
RETURNS boolean LANGUAGE sql STABLE STRICT AS $$
    SELECT EXISTS (
        SELECT 1 FROM settlement.activity_projection_profiles p
        WHERE p.source_stream=raw.source_stream
          AND p.shadow_verified_windows>=p.required_shadow_windows
          AND raw.starts_at>=p.activates_at
          AND (
            raw.ends_at>raw.starts_at+(p.max_interval_credit_seconds*interval '1 second')
            OR NOT EXISTS (
                SELECT 1 FROM generate_series(
                    date_trunc('hour',raw.starts_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC',
                    date_trunc('hour',(raw.ends_at-interval '1 microsecond') AT TIME ZONE 'UTC') AT TIME ZONE 'UTC',
                    interval '1 hour') h
                WHERE NOT EXISTS (
                    SELECT 1 FROM settlement.seeding_activity_hours a
                    WHERE a.source_stream=raw.source_stream AND a.window_start=h
                      AND a.user_id=raw.user_id AND a.torrent_id=raw.torrent_id
                      AND a.info_hash_v1=raw.info_hash_v1 AND a.last_source_sequence>=raw.source_sequence
                )
            )
          )
    );
$$;
-- +goose StatementEnd

-- Pending completion work must be represented before any later activity can
-- be retired, even if the new obligation has not been created yet.
-- +goose StatementBegin
CREATE FUNCTION ledger.raw_hnr_is_required(raw ledger.raw_session_intervals)
RETURNS boolean LANGUAGE sql STABLE STRICT AS $$
    SELECT EXISTS (
        SELECT 1 FROM ledger.hnr_obligations o
        JOIN ledger.hnr_completion_assessments a ON a.id=o.assessment_id
        LEFT JOIN settlement.hnr_progress_accumulators p ON p.obligation_id=o.id
        WHERE o.state='tracking' AND a.user_id=raw.user_id AND a.torrent_id=raw.torrent_id
          AND raw.ends_at>a.completed_at
          AND (raw.source_stream IS NULL OR raw.source_sequence IS NULL OR p.obligation_id IS NULL
               OR coalesce((p.source_cursors->>raw.source_stream)::bigint,0)<raw.source_sequence)
    ) OR EXISTS (
        SELECT 1 FROM ledger.raw_session_intervals completion
        JOIN settlement.hnr_work work ON work.interval_event_id=completion.event_id
        WHERE completion.user_id=raw.user_id AND completion.torrent_id=raw.torrent_id
          AND completion.completed_transition AND completion.ends_at<raw.ends_at
          AND work.processed_at IS NULL
    );
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.reject_raw_interval_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP<>'DELETE' OR OLD.ends_at>=statement_timestamp()-interval '48 hours'
       OR ledger.raw_hnr_is_required(OLD)
       OR (OLD.previous_left=0 AND OLD.current_left=0
           AND NOT ledger.seeding_interval_is_closed(OLD.starts_at,OLD.ends_at)
           AND NOT ledger.raw_seeding_is_compacted(OLD)) THEN
        RAISE EXCEPTION 'raw interval is recent or still required by an unrepresented consumer';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION ledger.protect_snapshot_payload()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF octet_length(NEW.payload_gzip)>525312 OR NOT EXISTS (
            SELECT 1 FROM ledger.seeding_swarm_snapshot_chunks c
            WHERE c.snapshot_id=NEW.snapshot_id AND c.chunk_index=NEW.chunk_index
              AND c.compact_payload AND c.payload_sha256=NEW.payload_sha256
        ) THEN RAISE EXCEPTION 'snapshot payload digest does not match canonical chunk'; END IF;
        RETURN NEW;
    END IF;
    IF TG_OP='UPDATE' OR NOT ledger.snapshot_payload_can_expire(OLD.snapshot_id) THEN
        RAISE EXCEPTION 'recent, boundary or anomalous snapshot payload is retained';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION ledger.snapshot_payload_can_expire(id uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM ledger.seeding_swarm_snapshots s WHERE s.snapshot_id=id
          AND s.status='complete' AND s.observed_at<statement_timestamp()-interval '12 hours'
          AND NOT EXISTS (
              SELECT 1 FROM ledger.seeding_evidence_windows w
              JOIN settlement.seeding_reconciliation_work r USING(window_start)
              WHERE r.resolved_at IS NULL AND (w.selected_snapshot_id=s.snapshot_id OR w.snapshot_fence_id=s.snapshot_id)
          )
          AND (
            ledger.is_redundant_hourly_snapshot(s.snapshot_id)
            OR (
                EXISTS (SELECT 1 FROM ledger.seeding_evidence_windows w
                        WHERE w.selected_snapshot_id=s.snapshot_id AND w.window_end<statement_timestamp()-interval '12 hours')
                AND NOT EXISTS (
                    SELECT 1 FROM ledger.seeding_evidence_windows w JOIN ledger.seeding_evidence_anomalies a USING(window_start)
                    WHERE w.selected_snapshot_id=s.snapshot_id
                )
            )
          )
    );
$$;
-- +goose StatementEnd
CREATE TRIGGER snapshot_payload_retention BEFORE INSERT OR UPDATE OR DELETE ON ledger.seeding_snapshot_payloads
FOR EACH ROW EXECUTE FUNCTION ledger.protect_snapshot_payload();


CREATE TABLE settlement.raw_interval_gc_ranges (
    window_start timestamptz PRIMARY KEY,
    cursor_ends_at timestamptz NOT NULL,
    cursor_event_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    retry_after timestamptz NOT NULL
);
CREATE INDEX raw_interval_gc_ranges_ready_idx ON settlement.raw_interval_gc_ranges(retry_after,window_start);

-- +goose StatementBegin
CREATE FUNCTION settlement.enqueue_closed_activity_range()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO settlement.raw_interval_gc_ranges(window_start,cursor_ends_at,retry_after)
    VALUES(NEW.window_start,NEW.window_start,NEW.window_end+interval '48 hours')
    ON CONFLICT(window_start) DO UPDATE SET
        cursor_ends_at=least(settlement.raw_interval_gc_ranges.cursor_ends_at,EXCLUDED.cursor_ends_at),
        cursor_event_id='00000000-0000-0000-0000-000000000000',
        retry_after=least(settlement.raw_interval_gc_ranges.retry_after,EXCLUDED.retry_after);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER seeding_window_raw_gc_range AFTER INSERT ON ledger.seeding_evidence_windows
FOR EACH ROW EXECUTE FUNCTION settlement.enqueue_closed_activity_range();
INSERT INTO settlement.raw_interval_gc_ranges(window_start,cursor_ends_at,retry_after)
SELECT window_start,window_start,window_end+interval '48 hours' FROM ledger.seeding_evidence_windows
ON CONFLICT DO NOTHING;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.enqueue_raw_interval_gc()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO settlement.raw_interval_gc(event_id,retry_after)
    SELECT raw.event_id,greatest(raw.ends_at+interval '48 hours',statement_timestamp())
    FROM (
        SELECT DISTINCT (to_jsonb(d)->>TG_ARGV[0])::uuid AS event_id
        FROM deleted_gc_dependencies d
    ) candidate JOIN ledger.raw_session_intervals raw USING(event_id)
    WHERE raw.previous_left<>0 OR raw.current_left<>0
       OR ledger.seeding_interval_is_closed(raw.starts_at,raw.ends_at)
       OR ledger.raw_seeding_is_compacted(raw)
    ORDER BY raw.event_id ON CONFLICT(event_id) DO NOTHING;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION settlement.protect_bounded_seeding_snapshot_inbox()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    snapshot_observed_at timestamptz;
    snapshot_status text;
    evidence_closed_through timestamptz;
BEGIN
    IF TG_OP <> 'DELETE' THEN
        RAISE EXCEPTION 'seeding swarm snapshot inbox evidence is immutable';
    END IF;
    SELECT snapshot.observed_at, snapshot.status
    INTO snapshot_observed_at, snapshot_status
    FROM ledger.seeding_swarm_snapshots AS snapshot
    WHERE snapshot.snapshot_id = OLD.snapshot_id;
    SELECT ledger.seeding_closed_through() INTO evidence_closed_through;
    IF snapshot_status IS NULL
        OR snapshot_status NOT IN ('collecting', 'complete')
        OR snapshot_observed_at IS NULL
        OR evidence_closed_through IS NULL
        OR snapshot_observed_at >= evidence_closed_through
        OR OLD.received_at >= clock_timestamp() - interval '12 hours' THEN
        RAISE EXCEPTION 'active, recent or unclosed seeding snapshot inbox cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_bounded_seeding_snapshot_chunks()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    snapshot_observed_at timestamptz;
    snapshot_status text;
    evidence_closed_through timestamptz;
BEGIN
    IF TG_OP <> 'DELETE' THEN
        RAISE EXCEPTION 'seeding swarm snapshot chunk evidence is immutable';
    END IF;
    SELECT snapshot.observed_at, snapshot.status
    INTO snapshot_observed_at, snapshot_status
    FROM ledger.seeding_swarm_snapshots AS snapshot
    WHERE snapshot.snapshot_id = OLD.snapshot_id;
    SELECT ledger.seeding_closed_through() INTO evidence_closed_through;
    IF snapshot_status IS NULL
        OR snapshot_status NOT IN ('collecting', 'complete')
        OR snapshot_observed_at IS NULL
        OR evidence_closed_through IS NULL
        OR snapshot_observed_at >= evidence_closed_through
        OR snapshot_observed_at >= clock_timestamp() - interval '12 hours' THEN
        RAISE EXCEPTION 'active, recent or unclosed seeding snapshot chunks cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_seeding_snapshot_run()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.observed_at >= clock_timestamp() - interval '12 hours'
            OR OLD.observed_at >= coalesce(
                (SELECT ledger.seeding_closed_through()),
                '-infinity'::timestamptz
            )
            OR EXISTS (
                SELECT 1 FROM settlement.seeding_swarm_snapshot_inbox AS inbox
                WHERE inbox.snapshot_id = OLD.snapshot_id
            )
            OR EXISTS (
                SELECT 1 FROM ledger.seeding_swarm_snapshot_chunks AS chunk
                WHERE chunk.snapshot_id = OLD.snapshot_id
            )
            OR EXISTS (
                SELECT 1 FROM ledger.seeding_swarm_snapshot_entries AS entry
                WHERE entry.snapshot_id = OLD.snapshot_id
            )
            OR EXISTS (
                SELECT 1 FROM ledger.seeding_evidence_windows AS evidence_window
                WHERE evidence_window.selected_snapshot_id = OLD.snapshot_id
                   OR evidence_window.snapshot_fence_id = OLD.snapshot_id
            )
            OR NOT EXISTS (
                SELECT 1 FROM ledger.seeding_swarm_snapshots AS newer
                WHERE newer.source_id = OLD.source_id
                  AND newer.routing_epoch = OLD.routing_epoch
                  AND newer.snapshot_sequence > OLD.snapshot_sequence
            ) THEN
            RAISE EXCEPTION 'active, recent, referenced or route-head seeding snapshot cannot be deleted';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.status = 'complete' THEN
        RAISE EXCEPTION 'complete seeding swarm snapshot evidence is immutable';
    END IF;
    IF OLD.snapshot_id IS DISTINCT FROM NEW.snapshot_id
        OR OLD.source_id IS DISTINCT FROM NEW.source_id
        OR OLD.routing_epoch IS DISTINCT FROM NEW.routing_epoch
        OR OLD.snapshot_sequence IS DISTINCT FROM NEW.snapshot_sequence
        OR OLD.observed_at IS DISTINCT FROM NEW.observed_at
        OR OLD.chunk_count IS DISTINCT FROM NEW.chunk_count
        OR OLD.created_at IS DISTINCT FROM NEW.created_at
        OR NEW.received_chunk_count < OLD.received_chunk_count THEN
        RAISE EXCEPTION 'seeding swarm snapshot identity is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_recent_seeding_snapshot_entries()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM (SELECT DISTINCT snapshot_id FROM deleted_snapshot_entries) AS deleted
        INNER JOIN ledger.seeding_swarm_snapshots AS snapshot USING (snapshot_id)
        WHERE snapshot.observed_at >= coalesce(
            (SELECT ledger.seeding_closed_through()), '-infinity'::timestamptz)
          AND (snapshot.observed_at >= statement_timestamp() - interval '12 hours'
               OR NOT ledger.is_redundant_hourly_snapshot(snapshot.snapshot_id))
    ) OR EXISTS (
        SELECT 1
        FROM (SELECT DISTINCT snapshot_id FROM deleted_snapshot_entries) AS deleted
        INNER JOIN ledger.seeding_evidence_windows AS w ON w.selected_snapshot_id = deleted.snapshot_id
        WHERE w.window_end >= statement_timestamp() - interval '12 hours'
           OR EXISTS (SELECT 1 FROM ledger.seeding_evidence_anomalies AS a WHERE a.window_start = w.window_start)
           OR EXISTS (SELECT 1 FROM settlement.seeding_reconciliation_work r
                      WHERE r.window_start=w.window_start AND r.resolved_at IS NULL)
    ) THEN
        RAISE EXCEPTION 'recent, boundary or anomalous selected seeding snapshot entries are retained';
    END IF;
    INSERT INTO settlement.snapshot_entry_gc_progress (snapshot_id)
    SELECT DISTINCT deleted.snapshot_id FROM deleted_snapshot_entries AS deleted
    WHERE NOT EXISTS (
        SELECT 1 FROM ledger.seeding_swarm_snapshot_entries AS remaining
        WHERE remaining.snapshot_id = deleted.snapshot_id LIMIT 1 OFFSET 0)
    ON CONFLICT (snapshot_id) DO NOTHING;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd


-- +goose StatementBegin
CREATE FUNCTION settlement.protect_activity_hour()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        IF OLD.window_start>=statement_timestamp()-interval '49 hours'
           OR NOT EXISTS(SELECT 1 FROM ledger.seeding_evidence_windows w
                         WHERE w.window_start=OLD.window_start AND w.schema_version='seeding.evidence.v3')
           OR EXISTS(SELECT 1 FROM ledger.seeding_evidence_anomalies a WHERE a.window_start=OLD.window_start)
           OR EXISTS(SELECT 1 FROM settlement.seeding_reconciliation_work r
                     WHERE r.window_start=OLD.window_start AND r.resolved_at IS NULL) THEN
            RAISE EXCEPTION 'unsealed or anomalous incremental activity is retained';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.source_stream IS DISTINCT FROM OLD.source_stream OR NEW.window_start IS DISTINCT FROM OLD.window_start
       OR NEW.user_id IS DISTINCT FROM OLD.user_id OR NEW.torrent_id IS DISTINCT FROM OLD.torrent_id
       OR NEW.info_hash_v1 IS DISTINCT FROM OLD.info_hash_v1 OR NEW.source_count<>OLD.source_count+1
       OR NEW.raw_uploaded<OLD.raw_uploaded OR NEW.last_source_sequence<=OLD.last_source_sequence
       OR NOT (NEW.active_ranges @> OLD.active_ranges) THEN
        RAISE EXCEPTION 'incremental activity must preserve identity and monotonically accumulate';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER activity_hour_monotonic BEFORE UPDATE OR DELETE ON settlement.seeding_activity_hours
FOR EACH ROW EXECUTE FUNCTION settlement.protect_activity_hour();

-- +goose StatementBegin
CREATE FUNCTION settlement.protect_hnr_checkpoint()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN
        IF EXISTS(SELECT 1 FROM ledger.hnr_obligations o WHERE o.id=OLD.obligation_id
                  AND (o.state='tracking' OR o.updated_at>=statement_timestamp()-interval '48 hours')) THEN
            RAISE EXCEPTION 'active or recent H&R checkpoint is retained';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.state_json->'cursors' IS DISTINCT FROM NEW.source_cursors
       OR NEW.state_json->>'obligation_id' IS DISTINCT FROM NEW.obligation_id::text THEN
        RAISE EXCEPTION 'H&R checkpoint cursor or identity mismatch';
    END IF;
    IF TG_OP='UPDATE' AND (
        NEW.obligation_id IS DISTINCT FROM OLD.obligation_id OR NEW.updated_at<OLD.updated_at
        OR NEW.state_json->'completed_at' IS DISTINCT FROM OLD.state_json->'completed_at'
        OR NEW.state_json->'max_interval_credit_seconds' IS DISTINCT FROM OLD.state_json->'max_interval_credit_seconds'
        OR EXISTS(SELECT 1 FROM jsonb_each_text(OLD.source_cursors) cursor
                  WHERE coalesce((NEW.source_cursors->>cursor.key)::bigint,0)<cursor.value::bigint)
    ) THEN RAISE EXCEPTION 'H&R checkpoint cannot change policy or regress its durable cursor'; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER hnr_checkpoint_monotonic BEFORE INSERT OR UPDATE OR DELETE ON settlement.hnr_progress_accumulators
FOR EACH ROW EXECUTE FUNCTION settlement.protect_hnr_checkpoint();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.protect_bounded_seeding_detail()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP <> 'DELETE' THEN
        RAISE EXCEPTION 'seeding evidence detail is immutable';
    END IF;
    IF TG_TABLE_NAME = 'seeding_evidence_sources'
        AND (
            OLD.window_start >= clock_timestamp() - interval '12 hours'
            OR EXISTS (SELECT 1 FROM settlement.seeding_reconciliation_work r
                       WHERE r.window_start=OLD.window_start AND r.resolved_at IS NULL)
            OR EXISTS (
                SELECT 1
                FROM ledger.seeding_evidence_anomalies AS anomaly
                WHERE anomaly.window_start = OLD.window_start
            )
        ) THEN
        RAISE EXCEPTION 'recent or anomalous seeding evidence sources cannot be deleted';
    END IF;
    IF TG_TABLE_NAME = 'seeding_evidence_anomalies'
        AND (OLD.detected_at >= clock_timestamp() - interval '30 days'
             OR EXISTS(SELECT 1 FROM settlement.seeding_reconciliation_work r
                       WHERE r.window_start=OLD.window_start AND r.resolved_at IS NULL)) THEN
        RAISE EXCEPTION 'recent seeding evidence anomalies cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd


-- Every late source creates an explicit durable review pin. It cannot silently
-- disappear just because the diagnostic row itself reaches a retention age.
-- +goose StatementBegin
CREATE FUNCTION settlement.pin_late_seeding_reconciliation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO settlement.seeding_reconciliation_work(window_start,reason_code,detected_at)
    VALUES(NEW.window_start,'late_source_fact',NEW.detected_at)
    ON CONFLICT(window_start) DO UPDATE
    SET reason_code='late_source_fact',detected_at=EXCLUDED.detected_at,resolved_at=NULL,resolution_reference=NULL
    WHERE settlement.seeding_reconciliation_work.resolved_at IS NOT NULL;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER seeding_anomaly_reconciliation AFTER INSERT ON ledger.seeding_evidence_anomalies
FOR EACH ROW EXECUTE FUNCTION settlement.pin_late_seeding_reconciliation();

-- +goose StatementBegin
CREATE FUNCTION ledger.verify_compact_evidence_item()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE version text; seconds bigint;
BEGIN
    SELECT schema_version INTO version FROM ledger.seeding_evidence_windows WHERE window_start=NEW.window_start;
    IF version='seeding.evidence.v3' THEN
        IF NEW.active_ranges IS NULL OR isempty(NEW.active_ranges) OR NEW.source_commitment IS NULL
           OR NOT (tstzmultirange(tstzrange(NEW.window_start,NEW.window_start+interval '1 hour','[)')) @> NEW.active_ranges) THEN
            RAISE EXCEPTION 'v3 evidence requires bounded exact ranges and a source commitment';
        END IF;
        SELECT floor(sum(extract(epoch FROM upper(r)-lower(r))))::bigint INTO seconds
        FROM unnest(NEW.active_ranges) r;
        IF seconds IS DISTINCT FROM NEW.active_seconds THEN
            RAISE EXCEPTION 'v3 evidence active seconds disagree with the normalized range union';
        END IF;
    ELSIF NEW.active_ranges IS NOT NULL OR NEW.source_commitment IS NOT NULL THEN
        RAISE EXCEPTION 'legacy evidence cannot silently adopt the v3 proof format';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER seeding_compact_evidence_consistent BEFORE INSERT ON ledger.seeding_evidence_items
FOR EACH ROW EXECUTE FUNCTION ledger.verify_compact_evidence_item();


CREATE TABLE settlement.seeding_reconciliation_history (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    window_start timestamptz NOT NULL REFERENCES ledger.seeding_evidence_windows(window_start),
    recorded_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    previous_state jsonb NOT NULL,
    next_state jsonb NOT NULL
);
-- +goose StatementBegin
CREATE FUNCTION settlement.record_seeding_reconciliation_transition()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' OR NEW.window_start IS DISTINCT FROM OLD.window_start THEN
        RAISE EXCEPTION 'reconciliation identity and history are retained';
    END IF;
    INSERT INTO settlement.seeding_reconciliation_history(window_start,previous_state,next_state)
    VALUES(OLD.window_start,to_jsonb(OLD),to_jsonb(NEW));
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER seeding_reconciliation_history BEFORE UPDATE OR DELETE ON settlement.seeding_reconciliation_work
FOR EACH ROW EXECUTE FUNCTION settlement.record_seeding_reconciliation_transition();
CREATE TRIGGER seeding_reconciliation_history_immutable BEFORE UPDATE OR DELETE ON settlement.seeding_reconciliation_history
FOR EACH ROW EXECUTE FUNCTION ledger.reject_seeding_evidence_mutation();

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'incremental activity storage is forward-only after compact evidence or retained raw detail changes';
END $$;
-- +goose StatementEnd
