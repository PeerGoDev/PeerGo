-- +goose Up

-- The per-announce Core projection is a short replay/worker hand-off, not the
-- user-facing long-term ledger.  Keep a compact three-hour UTC rollup while
-- retaining the existing user and user/torrent totals as the authoritative
-- all-time balances.
CREATE TABLE traffic.user_traffic_three_hour_rollups (
    bucket_start timestamptz NOT NULL,
    rollup_id uuid NOT NULL UNIQUE,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    torrent_id bigint NOT NULL REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    interval_starts_at timestamptz NOT NULL,
    interval_ends_at timestamptz NOT NULL,
    raw_uploaded bigint NOT NULL CHECK (raw_uploaded >= 0),
    raw_downloaded bigint NOT NULL CHECK (raw_downloaded >= 0),
    credited_uploaded bigint NOT NULL CHECK (credited_uploaded >= 0),
    charged_downloaded bigint NOT NULL CHECK (charged_downloaded >= 0),
    settlement_count bigint NOT NULL CHECK (settlement_count > 0),
    first_occurred_at timestamptz NOT NULL,
    last_occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (bucket_start, user_id, torrent_id),
    CHECK (bucket_start = date_bin(
        interval '3 hours', bucket_start, timestamptz '1970-01-01 00:00:00+00'
    )),
    CHECK (interval_ends_at > interval_starts_at),
    CHECK (last_occurred_at >= first_occurred_at),
    CHECK (updated_at >= created_at)
);

-- +goose StatementBegin
CREATE FUNCTION traffic.protect_user_traffic_three_hour_rollup()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.bucket_start >= clock_timestamp() - interval '30 days' THEN
            RAISE EXCEPTION 'recent Core traffic rollups cannot be deleted';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.bucket_start IS DISTINCT FROM NEW.bucket_start
        OR OLD.rollup_id IS DISTINCT FROM NEW.rollup_id
        OR OLD.user_id IS DISTINCT FROM NEW.user_id
        OR OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR NEW.raw_uploaded < OLD.raw_uploaded
        OR NEW.raw_downloaded < OLD.raw_downloaded
        OR NEW.credited_uploaded < OLD.credited_uploaded
        OR NEW.charged_downloaded < OLD.charged_downloaded
        OR NEW.settlement_count <= OLD.settlement_count
        OR NEW.interval_starts_at > OLD.interval_starts_at
        OR NEW.interval_ends_at < OLD.interval_ends_at
        OR NEW.first_occurred_at > OLD.first_occurred_at
        OR NEW.last_occurred_at < OLD.last_occurred_at
        OR NEW.created_at > OLD.created_at
        OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'Core traffic rollup transition is not monotonic';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER user_traffic_three_hour_rollups_monotonic
BEFORE UPDATE OR DELETE ON traffic.user_traffic_three_hour_rollups
FOR EACH ROW EXECUTE FUNCTION traffic.protect_user_traffic_three_hour_rollup();

-- Each new projection merges into one user/torrent/three-hour row.  rollup_id
-- is only a stable public row identity, not an individual settlement event ID.
-- +goose StatementBegin
CREATE FUNCTION traffic.rollup_user_traffic_entry()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_bucket timestamptz;
BEGIN
    target_bucket := date_bin(
        interval '3 hours', NEW.interval_ends_at,
        timestamptz '1970-01-01 00:00:00+00'
    );
    INSERT INTO traffic.user_traffic_three_hour_rollups (
        bucket_start,
        rollup_id,
        user_id,
        torrent_id,
        interval_starts_at,
        interval_ends_at,
        raw_uploaded,
        raw_downloaded,
        credited_uploaded,
        charged_downloaded,
        settlement_count,
        first_occurred_at,
        last_occurred_at,
        created_at,
        updated_at
    ) VALUES (
        target_bucket,
        NEW.settlement_id,
        NEW.user_id,
        NEW.torrent_id,
        NEW.interval_starts_at,
        NEW.interval_ends_at,
        NEW.raw_uploaded,
        NEW.raw_downloaded,
        NEW.credited_uploaded,
        NEW.charged_downloaded,
        1,
        NEW.occurred_at,
        NEW.occurred_at,
        NEW.applied_at,
        NEW.applied_at
    )
    ON CONFLICT (bucket_start, user_id, torrent_id) DO UPDATE
    SET
        interval_starts_at = least(
            traffic.user_traffic_three_hour_rollups.interval_starts_at,
            EXCLUDED.interval_starts_at
        ),
        interval_ends_at = greatest(
            traffic.user_traffic_three_hour_rollups.interval_ends_at,
            EXCLUDED.interval_ends_at
        ),
        raw_uploaded = traffic.user_traffic_three_hour_rollups.raw_uploaded
            + EXCLUDED.raw_uploaded,
        raw_downloaded = traffic.user_traffic_three_hour_rollups.raw_downloaded
            + EXCLUDED.raw_downloaded,
        credited_uploaded = traffic.user_traffic_three_hour_rollups.credited_uploaded
            + EXCLUDED.credited_uploaded,
        charged_downloaded = traffic.user_traffic_three_hour_rollups.charged_downloaded
            + EXCLUDED.charged_downloaded,
        settlement_count = traffic.user_traffic_three_hour_rollups.settlement_count + 1,
        first_occurred_at = least(
            traffic.user_traffic_three_hour_rollups.first_occurred_at,
            EXCLUDED.first_occurred_at
        ),
        last_occurred_at = greatest(
            traffic.user_traffic_three_hour_rollups.last_occurred_at,
            EXCLUDED.last_occurred_at
        ),
        created_at = least(
            traffic.user_traffic_three_hour_rollups.created_at,
            EXCLUDED.created_at
        ),
        updated_at = greatest(
            traffic.user_traffic_three_hour_rollups.updated_at,
            EXCLUDED.updated_at
        );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Backfill without either double-counting concurrent projections or holding a
-- write lock during the ten-million-row aggregate.  First capture a fence and
-- aggregate through it without a trigger.  Then briefly block inserts, merge
-- the small tail committed during that scan, install the trigger, and commit;
-- waiting inserts resume with the trigger active.
CREATE TEMPORARY TABLE core_traffic_rollup_backfill_fence (
    initial_sequence bigint NOT NULL CHECK (initial_sequence >= 0)
) ON COMMIT DROP;

INSERT INTO core_traffic_rollup_backfill_fence (initial_sequence)
SELECT coalesce(max(projection_sequence), 0)
FROM traffic.user_traffic_entries;

-- +goose StatementBegin
CREATE FUNCTION traffic.backfill_user_traffic_three_hour_rollups(
    after_sequence bigint,
    through_sequence bigint
)
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
    IF after_sequence < 0 OR through_sequence < after_sequence THEN
        RAISE EXCEPTION 'invalid Core traffic rollup backfill fence';
    END IF;

    INSERT INTO traffic.user_traffic_three_hour_rollups (
        bucket_start,
        rollup_id,
        user_id,
        torrent_id,
        interval_starts_at,
        interval_ends_at,
        raw_uploaded,
        raw_downloaded,
        credited_uploaded,
        charged_downloaded,
        settlement_count,
        first_occurred_at,
        last_occurred_at,
        created_at,
        updated_at
    )
    SELECT
        date_bin(
            interval '3 hours', entry.interval_ends_at,
            timestamptz '1970-01-01 00:00:00+00'
        ),
        min(entry.settlement_id::text)::uuid,
        entry.user_id,
        entry.torrent_id,
        min(entry.interval_starts_at),
        max(entry.interval_ends_at),
        sum(entry.raw_uploaded)::bigint,
        sum(entry.raw_downloaded)::bigint,
        sum(entry.credited_uploaded)::bigint,
        sum(entry.charged_downloaded)::bigint,
        count(*)::bigint,
        min(entry.occurred_at),
        max(entry.occurred_at),
        min(entry.applied_at),
        max(entry.applied_at)
    FROM traffic.user_traffic_entries AS entry
    WHERE entry.projection_sequence > after_sequence
      AND entry.projection_sequence <= through_sequence
    GROUP BY
        date_bin(
            interval '3 hours', entry.interval_ends_at,
            timestamptz '1970-01-01 00:00:00+00'
        ),
        entry.user_id,
        entry.torrent_id
    ON CONFLICT (bucket_start, user_id, torrent_id) DO UPDATE
    SET
        interval_starts_at = least(
            traffic.user_traffic_three_hour_rollups.interval_starts_at,
            EXCLUDED.interval_starts_at
        ),
        interval_ends_at = greatest(
            traffic.user_traffic_three_hour_rollups.interval_ends_at,
            EXCLUDED.interval_ends_at
        ),
        raw_uploaded = traffic.user_traffic_three_hour_rollups.raw_uploaded
            + EXCLUDED.raw_uploaded,
        raw_downloaded = traffic.user_traffic_three_hour_rollups.raw_downloaded
            + EXCLUDED.raw_downloaded,
        credited_uploaded = traffic.user_traffic_three_hour_rollups.credited_uploaded
            + EXCLUDED.credited_uploaded,
        charged_downloaded = traffic.user_traffic_three_hour_rollups.charged_downloaded
            + EXCLUDED.charged_downloaded,
        settlement_count = traffic.user_traffic_three_hour_rollups.settlement_count
            + EXCLUDED.settlement_count,
        first_occurred_at = least(
            traffic.user_traffic_three_hour_rollups.first_occurred_at,
            EXCLUDED.first_occurred_at
        ),
        last_occurred_at = greatest(
            traffic.user_traffic_three_hour_rollups.last_occurred_at,
            EXCLUDED.last_occurred_at
        ),
        created_at = least(
            traffic.user_traffic_three_hour_rollups.created_at,
            EXCLUDED.created_at
        ),
        updated_at = greatest(
            traffic.user_traffic_three_hour_rollups.updated_at,
            EXCLUDED.updated_at
        );
END;
$$;
-- +goose StatementEnd

SELECT traffic.backfill_user_traffic_three_hour_rollups(
    0,
    (SELECT initial_sequence FROM core_traffic_rollup_backfill_fence)
);

LOCK TABLE traffic.user_traffic_entries IN SHARE ROW EXCLUSIVE MODE;

SELECT traffic.backfill_user_traffic_three_hour_rollups(
    (SELECT initial_sequence FROM core_traffic_rollup_backfill_fence),
    (SELECT coalesce(max(projection_sequence), 0) FROM traffic.user_traffic_entries)
);

CREATE TRIGGER user_traffic_entries_three_hour_rollup
AFTER INSERT ON traffic.user_traffic_entries
FOR EACH ROW EXECUTE FUNCTION traffic.rollup_user_traffic_entry();

DROP FUNCTION traffic.backfill_user_traffic_three_hour_rollups(bigint, bigint);

CREATE INDEX user_traffic_three_hour_rollups_user_time_idx
    ON traffic.user_traffic_three_hour_rollups (
        user_id, interval_ends_at DESC, bucket_start DESC, torrent_id
    );
CREATE INDEX user_traffic_three_hour_rollups_retention_idx
    ON traffic.user_traffic_three_hour_rollups (
        bucket_start, user_id, torrent_id
    );

-- Full canonical payloads duplicated a high-volume NATS event in PostgreSQL.
-- A SHA-256 fence is sufficient to reject a conflicting duplicate; legacy
-- payloads age out with their short-lived inbox rows without a table rewrite.
ALTER TABLE traffic.settlement_inbox
    ALTER COLUMN payload_json DROP NOT NULL,
    DROP CONSTRAINT settlement_inbox_payload_json_check;

ALTER TABLE traffic.settlement_inbox SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05,
    autovacuum_analyze_threshold = 1000
);
ALTER TABLE traffic.user_traffic_entries SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05,
    autovacuum_analyze_threshold = 1000
);
ALTER TABLE traffic.user_traffic_entry_explanations SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05,
    autovacuum_analyze_threshold = 1000
);
ALTER TABLE traffic.user_traffic_entry_segments SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0.05,
    autovacuum_analyze_threshold = 1000
);

-- Per-event rows may be removed only after twelve hours and only after the
-- upload-experience cursor has consumed every relevant positive-upload row.
-- The cleanup worker uses the same predicates and deletes children first.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION traffic.reject_projection_evidence_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_applied_at timestamptz;
    source_occurred_at timestamptz;
    source_raw_uploaded bigint;
    source_projection_sequence bigint;
    upload_cursor bigint;
    first_upload_policy timestamptz;
BEGIN
    IF TG_OP <> 'DELETE' THEN
        RAISE EXCEPTION 'Core traffic projection evidence is immutable';
    END IF;

    IF TG_TABLE_NAME = 'settlement_inbox' THEN
        IF OLD.applied_at >= clock_timestamp() - interval '12 hours'
            OR EXISTS (
                SELECT 1 FROM traffic.user_traffic_entries AS entry
                WHERE entry.settlement_id = OLD.event_id
            ) THEN
            RAISE EXCEPTION 'recent or referenced Core traffic inbox evidence cannot be deleted';
        END IF;
        RETURN OLD;
    END IF;

    IF TG_TABLE_NAME = 'user_traffic_entries' THEN
        source_applied_at := OLD.applied_at;
        source_occurred_at := OLD.occurred_at;
        source_raw_uploaded := OLD.raw_uploaded;
        source_projection_sequence := OLD.projection_sequence;
    ELSE
        SELECT
            entry.applied_at,
            entry.occurred_at,
            entry.raw_uploaded,
            entry.projection_sequence
        INTO
            source_applied_at,
            source_occurred_at,
            source_raw_uploaded,
            source_projection_sequence
        FROM traffic.user_traffic_entries AS entry
        WHERE entry.settlement_id = OLD.settlement_id;
    END IF;

    SELECT last_projection_sequence
    INTO upload_cursor
    FROM progression.contribution_upload_cursor
    WHERE singleton = true;

    SELECT min(effective_from)
    INTO first_upload_policy
    FROM progression.contribution_experience_policy_revisions;

    IF source_applied_at IS NULL
        OR source_applied_at >= clock_timestamp() - interval '12 hours'
        OR (
            source_raw_uploaded > 0
            AND first_upload_policy IS NOT NULL
            AND source_occurred_at >= first_upload_policy
            AND (
                upload_cursor IS NULL
                OR source_projection_sequence > upload_cursor
            )
        ) THEN
        RAISE EXCEPTION 'recent or unprocessed Core traffic projection evidence cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd

-- +goose Down

-- Cleanup makes restoration of one immutable row per announce impossible.
-- Roll back by restoring a database backup from before this migration.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION '202608229999 is irreversible after compact traffic cleanup';
END;
$$;
-- +goose StatementEnd
