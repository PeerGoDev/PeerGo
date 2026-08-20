-- +goose Up

-- Explanation rows are an additive, privacy-minimized projection. Existing
-- settlement.traffic.v1 inbox/entry rows remain valid without one because
-- retained events produced before this field was introduced must still replay.
-- Core never stores policy IDs, rule applications, snapshot digests or Tracker
-- session evidence here.
CREATE TABLE traffic.user_traffic_entry_explanations (
    settlement_id uuid PRIMARY KEY
        REFERENCES traffic.user_traffic_entries (settlement_id) ON DELETE RESTRICT,
    status text NOT NULL CHECK (status IN ('complete', 'too_many_segments')),
    segment_count integer NOT NULL CHECK (segment_count > 0),
    CHECK (
        (status = 'complete' AND segment_count BETWEEN 1 AND 24)
        OR (status = 'too_many_segments' AND segment_count > 24)
    )
);

CREATE TABLE traffic.user_traffic_entry_segments (
    settlement_id uuid NOT NULL
        REFERENCES traffic.user_traffic_entry_explanations (settlement_id) ON DELETE RESTRICT,
    segment_index integer NOT NULL CHECK (segment_index BETWEEN 0 AND 23),
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    raw_uploaded bigint NOT NULL CHECK (raw_uploaded >= 0),
    raw_downloaded bigint NOT NULL CHECK (raw_downloaded >= 0),
    credited_uploaded bigint NOT NULL CHECK (credited_uploaded >= 0),
    charged_downloaded bigint NOT NULL CHECK (charged_downloaded >= 0),
    PRIMARY KEY (settlement_id, segment_index),
    CHECK (ends_at > starts_at)
);

CREATE TRIGGER traffic_user_entry_explanations_immutable
BEFORE UPDATE OR DELETE ON traffic.user_traffic_entry_explanations
FOR EACH ROW EXECUTE FUNCTION traffic.reject_projection_evidence_mutation();

CREATE TRIGGER traffic_user_entry_segments_immutable
BEFORE UPDATE OR DELETE ON traffic.user_traffic_entry_segments
FOR EACH ROW EXECUTE FUNCTION traffic.reject_projection_evidence_mutation();

-- +goose StatementBegin
CREATE FUNCTION traffic.require_complete_user_traffic_explanation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    header traffic.user_traffic_entries%ROWTYPE;
    explanation traffic.user_traffic_entry_explanations%ROWTYPE;
    actual_count bigint;
    first_index integer;
    last_index integer;
    first_start timestamptz;
    last_end timestamptz;
    contiguous boolean;
    total_raw_uploaded numeric;
    total_raw_downloaded numeric;
    total_credited_uploaded numeric;
    total_charged_downloaded numeric;
BEGIN
    SELECT * INTO explanation
    FROM traffic.user_traffic_entry_explanations
    WHERE settlement_id = NEW.settlement_id;

    SELECT * INTO header
    FROM traffic.user_traffic_entries
    WHERE settlement_id = NEW.settlement_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'traffic explanation has no immutable entry header';
    END IF;

    WITH ordered AS (
        SELECT
            segment_index,
            starts_at,
            ends_at,
            raw_uploaded,
            raw_downloaded,
            credited_uploaded,
            charged_downloaded,
            lag(ends_at) OVER (ORDER BY segment_index) AS previous_ends_at
        FROM traffic.user_traffic_entry_segments
        WHERE settlement_id = NEW.settlement_id
    )
    SELECT
        count(*),
        min(segment_index),
        max(segment_index),
        min(starts_at),
        max(ends_at),
        COALESCE(bool_and(previous_ends_at IS NULL OR starts_at = previous_ends_at), false),
        COALESCE(sum(raw_uploaded), 0),
        COALESCE(sum(raw_downloaded), 0),
        COALESCE(sum(credited_uploaded), 0),
        COALESCE(sum(charged_downloaded), 0)
    INTO
        actual_count,
        first_index,
        last_index,
        first_start,
        last_end,
        contiguous,
        total_raw_uploaded,
        total_raw_downloaded,
        total_credited_uploaded,
        total_charged_downloaded
    FROM ordered;

    IF explanation.status = 'too_many_segments' THEN
        IF actual_count <> 0 THEN
            RAISE EXCEPTION 'omitted traffic explanation cannot contain projected segments';
        END IF;
        RETURN NULL;
    END IF;

    IF actual_count <> explanation.segment_count
        OR first_index <> 0
        OR last_index <> explanation.segment_count - 1
        OR first_start IS DISTINCT FROM header.interval_starts_at
        OR last_end IS DISTINCT FROM header.interval_ends_at
        OR NOT contiguous
        OR total_raw_uploaded <> header.raw_uploaded
        OR total_raw_downloaded <> header.raw_downloaded
        OR total_credited_uploaded <> header.credited_uploaded
        OR total_charged_downloaded <> header.charged_downloaded THEN
        RAISE EXCEPTION 'traffic explanation segments do not reconcile with immutable entry';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER traffic_user_entry_explanation_complete
AFTER INSERT ON traffic.user_traffic_entry_explanations
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION traffic.require_complete_user_traffic_explanation();

CREATE CONSTRAINT TRIGGER traffic_user_entry_segments_complete
AFTER INSERT ON traffic.user_traffic_entry_segments
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION traffic.require_complete_user_traffic_explanation();

-- +goose Down

DROP TRIGGER IF EXISTS traffic_user_entry_segments_complete ON traffic.user_traffic_entry_segments;
DROP TRIGGER IF EXISTS traffic_user_entry_explanation_complete ON traffic.user_traffic_entry_explanations;
DROP FUNCTION IF EXISTS traffic.require_complete_user_traffic_explanation();
DROP TRIGGER IF EXISTS traffic_user_entry_segments_immutable ON traffic.user_traffic_entry_segments;
DROP TRIGGER IF EXISTS traffic_user_entry_explanations_immutable ON traffic.user_traffic_entry_explanations;
DROP TABLE IF EXISTS traffic.user_traffic_entry_segments;
DROP TABLE IF EXISTS traffic.user_traffic_entry_explanations;
