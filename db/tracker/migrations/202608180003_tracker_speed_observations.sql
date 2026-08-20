-- +goose Up

-- Speed evidence is derived from immutable raw intervals during final
-- settlement. It intentionally references the interval instead of copying a
-- peer address: the address never leaves Tracker, while the signed rule and
-- threshold remain available on ledger.raw_session_intervals.
CREATE TABLE ledger.speed_observations (
    interval_event_id uuid PRIMARY KEY
        REFERENCES ledger.raw_session_intervals(event_id) ON DELETE RESTRICT,
    interval_duration_nanoseconds bigint NOT NULL CHECK (interval_duration_nanoseconds > 0),
    raw_uploaded bigint NOT NULL CHECK (raw_uploaded >= 0),
    average_upload_bytes_per_second bigint NOT NULL CHECK (average_upload_bytes_per_second >= 0),
    outcome text NOT NULL CHECK (outcome IN (
        'within_limit',
        'exceeded',
        'vip_exempt',
        'partially_vip_exempt'
    )),
    observed_at timestamptz NOT NULL
);

CREATE INDEX speed_observations_outcome_observed_idx
    ON ledger.speed_observations (outcome, observed_at DESC, interval_event_id);

-- +goose StatementBegin
CREATE FUNCTION ledger.reject_speed_observation_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'speed observations are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER speed_observations_immutable
BEFORE UPDATE OR DELETE ON ledger.speed_observations
FOR EACH ROW EXECUTE FUNCTION ledger.reject_speed_observation_mutation();

-- +goose Down

DROP TRIGGER speed_observations_immutable ON ledger.speed_observations;
DROP FUNCTION ledger.reject_speed_observation_mutation();
DROP TABLE ledger.speed_observations;
