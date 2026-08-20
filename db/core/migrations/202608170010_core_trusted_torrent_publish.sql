-- +goose Up

-- Trusted publishing is still a review decision, but its authority comes from
-- the uploader's immutable reseed-workgroup membership transition rather than
-- a staff session or a multi-review round. Keeping that evidence on the same
-- append-only decision row makes later membership changes irrelevant to the
-- historical publication decision.
ALTER TABLE review.torrent_decisions
    DROP CONSTRAINT torrent_decisions_round_source_check,
    DROP CONSTRAINT torrent_decisions_resolution_source_check,
    ADD COLUMN membership_transition_id uuid
        REFERENCES workgroups.membership_transitions (id) ON DELETE RESTRICT,
    ADD CONSTRAINT torrent_decisions_resolution_source_check CHECK (
        resolution_source IN ('staff', 'review_round', 'trusted_workgroup')
    ),
    ADD CONSTRAINT torrent_decisions_source_evidence_check CHECK (
        (resolution_source = 'staff'
            AND review_round_id IS NULL
            AND membership_transition_id IS NULL)
        OR (resolution_source = 'review_round'
            AND review_round_id IS NOT NULL
            AND membership_transition_id IS NULL)
        OR (resolution_source = 'trusted_workgroup'
            AND review_round_id IS NULL
            AND membership_transition_id IS NOT NULL)
    );

-- +goose Down

ALTER TABLE review.torrent_decisions
    DROP CONSTRAINT torrent_decisions_source_evidence_check,
    DROP CONSTRAINT torrent_decisions_resolution_source_check,
    DROP COLUMN membership_transition_id,
    ADD CONSTRAINT torrent_decisions_resolution_source_check CHECK (
        resolution_source IN ('staff', 'review_round')
    ),
    ADD CONSTRAINT torrent_decisions_round_source_check CHECK (
        resolution_source = 'staff' OR review_round_id IS NOT NULL
    );
