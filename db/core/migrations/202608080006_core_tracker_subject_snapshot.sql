-- +goose Up

-- Subject admission changes independently from torrent eligibility, so it has
-- its own monotonic revision. A builder reserves one revision and reads the
-- complete active subject allowlist in the same repeatable-read transaction.
-- Gaps are harmless if a builder crashes after commit and before publication;
-- rollback and same-sequence divergence remain forbidden at the file boundary.
CREATE TABLE tracker_control.subject_snapshot_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    last_sequence bigint NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    updated_at timestamptz
);

INSERT INTO tracker_control.subject_snapshot_state (singleton, last_sequence)
VALUES (true, 0);

-- +goose Down
DROP TABLE IF EXISTS tracker_control.subject_snapshot_state;
