-- +goose Up

-- Torrent eligibility and cumulative completion statistics change on
-- independent event streams. Keep a second monotonic cursor so signed Tracker
-- snapshots may refresh scrape statistics without pretending an eligibility
-- event occurred or diverging at the same ordered state.
ALTER TABLE tracker_control.projection_state
    ADD COLUMN completion_sequence bigint NOT NULL DEFAULT 0
        CHECK (completion_sequence >= 0);

UPDATE tracker_control.projection_state
SET completion_sequence = (
    SELECT count(*)::bigint
    FROM catalog.swarm_completion_inbox
)
WHERE singleton = true;

-- +goose Down

ALTER TABLE tracker_control.projection_state
    DROP COLUMN completion_sequence;
