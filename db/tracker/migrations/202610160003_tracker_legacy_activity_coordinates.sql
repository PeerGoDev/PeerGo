-- +goose Up
-- V1 records retain stream coordinates in event_inbox; V2 stores them on the
-- raw row. Both are durable and were already used by the accumulator. Unknown
-- provenance still pins raw evidence; never invent an ordering cursor.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.raw_seeding_is_compacted(raw ledger.raw_session_intervals)
RETURNS boolean LANGUAGE sql STABLE STRICT AS $$
    WITH coordinates AS (
        SELECT coalesce(raw.source_stream,inbox.source_stream) AS stream,
               coalesce(raw.source_sequence,inbox.source_sequence) AS sequence
        FROM (VALUES(1)) AS singleton(n)
        LEFT JOIN settlement.event_inbox inbox
          ON inbox.event_id=raw.event_id AND raw.source_stream IS NULL
    )
    SELECT EXISTS (
        SELECT 1 FROM settlement.activity_projection_profiles p CROSS JOIN coordinates c
        WHERE p.source_stream=c.stream AND c.sequence>0
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
                    WHERE a.source_stream=c.stream AND a.window_start=h
                      AND a.user_id=raw.user_id AND a.torrent_id=raw.torrent_id
                      AND a.info_hash_v1=raw.info_hash_v1 AND a.last_source_sequence>=c.sequence
                )
            )
          )
    );
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger.raw_hnr_is_required(raw ledger.raw_session_intervals)
RETURNS boolean LANGUAGE sql STABLE STRICT AS $$
    WITH coordinates AS (
        SELECT coalesce(raw.source_stream,inbox.source_stream) AS stream,
               coalesce(raw.source_sequence,inbox.source_sequence) AS sequence
        FROM (VALUES(1)) AS singleton(n)
        LEFT JOIN settlement.event_inbox inbox
          ON inbox.event_id=raw.event_id AND raw.source_stream IS NULL
    )
    SELECT EXISTS (
        SELECT 1 FROM ledger.hnr_obligations o
        JOIN ledger.hnr_completion_assessments a ON a.id=o.assessment_id
        LEFT JOIN settlement.hnr_progress_accumulators p ON p.obligation_id=o.id
        CROSS JOIN coordinates c
        WHERE o.state='tracking' AND a.user_id=raw.user_id AND a.torrent_id=raw.torrent_id
          AND raw.ends_at>a.completed_at
          AND (c.stream IS NULL OR c.sequence IS NULL OR p.obligation_id IS NULL
               OR coalesce((p.source_cursors->>c.stream)::bigint,0)<c.sequence)
    ) OR EXISTS (
        SELECT 1 FROM ledger.raw_session_intervals completion
        JOIN settlement.hnr_work work ON work.interval_event_id=completion.event_id
        WHERE completion.user_id=raw.user_id AND completion.torrent_id=raw.torrent_id
          AND completion.completed_transition AND completion.ends_at<raw.ends_at
          AND work.processed_at IS NULL
    );
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'known-source raw retirement is forward-only'; END $$;
-- +goose StatementEnd
