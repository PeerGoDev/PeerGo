-- +goose Up

-- Inbox retention probes the last event still held by each live Settlement
-- session. Foreign keys do not create an index on the referencing column, so
-- add the narrow lookup required by bounded inbox cleanup.
CREATE INDEX session_states_last_event_retention_idx
    ON settlement.session_states (last_event_id);

-- +goose Down

DROP INDEX settlement.session_states_last_event_retention_idx;
