-- +goose Up

-- Every authenticated member receives this narrow Web action, but the review
-- service also resolves an active review-workgroup membership at the exact
-- vote timestamp. The permission alone never makes somebody a reviewer.
INSERT INTO authz.permissions (
    action, description, risk_level, relationship,
    credential_audience, grantable, discoverable
) VALUES (
    'torrent.review.vote',
    '以有效种审组成员身份参与单个待审核种子的投票',
    'medium',
    'none',
    'web-session',
    true,
    true
);

INSERT INTO authz.role_permissions (role_id, action)
VALUES ('member', 'torrent.review.vote');

-- A round is a mutable current projection protected by row locks. Individual
-- votes below are immutable evidence and contain their post-vote counters, so
-- an idempotent retry never changes meaning after later reviewers vote.
CREATE TABLE review.torrent_review_rounds (
    id uuid PRIMARY KEY,
    torrent_id bigint NOT NULL REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    expected_torrent_version bigint NOT NULL CHECK (expected_torrent_version > 0),
    status text NOT NULL CHECK (status IN ('open', 'resolved', 'escalated')),
    required_votes smallint NOT NULL DEFAULT 3 CHECK (required_votes = 3),
    maximum_votes smallint NOT NULL DEFAULT 4 CHECK (maximum_votes = 4),
    approve_count smallint NOT NULL DEFAULT 0 CHECK (approve_count BETWEEN 0 AND 4),
    reject_count smallint NOT NULL DEFAULT 0 CHECK (reject_count BETWEEN 0 AND 4),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    final_decision_id uuid,
    opened_at timestamptz NOT NULL,
    resolved_at timestamptz,
    escalated_at timestamptz,
    updated_at timestamptz NOT NULL,
    UNIQUE (torrent_id, expected_torrent_version),
    CHECK (approve_count + reject_count <= maximum_votes),
    CHECK (
        (status = 'open' AND final_decision_id IS NULL AND resolved_at IS NULL AND escalated_at IS NULL)
        OR (status = 'resolved' AND final_decision_id IS NOT NULL AND resolved_at IS NOT NULL)
        OR (status = 'escalated' AND final_decision_id IS NULL AND resolved_at IS NULL AND escalated_at IS NOT NULL)
    )
);

CREATE INDEX torrent_review_rounds_current_idx
    ON review.torrent_review_rounds (status, opened_at, torrent_id);

CREATE TABLE review.torrent_review_votes (
    id uuid PRIMARY KEY,
    round_id uuid NOT NULL REFERENCES review.torrent_review_rounds (id) ON DELETE RESTRICT,
    torrent_id bigint NOT NULL REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    voter_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    membership_transition_id uuid NOT NULL
        REFERENCES workgroups.membership_transitions (id) ON DELETE RESTRICT,
    decision text NOT NULL CHECK (decision IN ('approve', 'reject')),
    reason_code text NOT NULL CHECK (reason_code IN (
        'meets_requirements',
        'metadata_incomplete',
        'duplicate_or_superseded',
        'content_policy_violation',
        'quality_requirements_not_met',
        'uploader_action_required',
        'other'
    )),
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 10 AND 1000),
    expected_torrent_version bigint NOT NULL CHECK (expected_torrent_version > 0),
    authorization_decision_id uuid NOT NULL,
    outcome_after_vote text NOT NULL CHECK (
        outcome_after_vote IN ('waiting', 'published', 'rejected', 'escalated')
    ),
    approve_count_after smallint NOT NULL CHECK (approve_count_after BETWEEN 0 AND 4),
    reject_count_after smallint NOT NULL CHECK (reject_count_after BETWEEN 0 AND 4),
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (round_id, voter_id),
    CHECK (approve_count_after + reject_count_after BETWEEN 1 AND 4),
    CHECK (
        (decision = 'approve' AND reason_code = 'meets_requirements')
        OR (decision = 'reject' AND reason_code <> 'meets_requirements')
    )
);

CREATE INDEX torrent_review_votes_voter_history_idx
    ON review.torrent_review_votes (voter_id, occurred_at DESC, id DESC);

ALTER TABLE review.torrent_decisions
    ADD COLUMN resolution_source text NOT NULL DEFAULT 'staff'
        CHECK (resolution_source IN ('staff', 'review_round')),
    ADD COLUMN review_round_id uuid
        REFERENCES review.torrent_review_rounds (id) ON DELETE RESTRICT,
    ADD CONSTRAINT torrent_decisions_round_source_check CHECK (
        resolution_source = 'staff' OR review_round_id IS NOT NULL
    );

ALTER TABLE review.torrent_review_rounds
    ADD CONSTRAINT torrent_review_rounds_final_decision_fkey
    FOREIGN KEY (final_decision_id)
    REFERENCES review.torrent_decisions (id)
    ON DELETE RESTRICT;

CREATE TRIGGER torrent_review_votes_immutable
BEFORE UPDATE OR DELETE ON review.torrent_review_votes
FOR EACH ROW EXECUTE FUNCTION review.reject_torrent_decision_mutation();

-- +goose StatementBegin
CREATE FUNCTION review.protect_torrent_review_round()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'torrent review rounds cannot be deleted';
    END IF;

    IF OLD.id IS DISTINCT FROM NEW.id
        OR OLD.torrent_id IS DISTINCT FROM NEW.torrent_id
        OR OLD.expected_torrent_version IS DISTINCT FROM NEW.expected_torrent_version
        OR OLD.required_votes IS DISTINCT FROM NEW.required_votes
        OR OLD.maximum_votes IS DISTINCT FROM NEW.maximum_votes
        OR OLD.opened_at IS DISTINCT FROM NEW.opened_at THEN
        RAISE EXCEPTION 'torrent review round identity is immutable';
    END IF;

    IF OLD.status NOT IN ('open', 'escalated') OR NEW.version <> OLD.version + 1
        OR NEW.approve_count < OLD.approve_count
        OR NEW.reject_count < OLD.reject_count
        OR NEW.updated_at < OLD.updated_at
        OR (OLD.status = 'escalated' AND NEW.status <> 'resolved') THEN
        RAISE EXCEPTION 'torrent review round must advance once from open state';
    END IF;

    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER torrent_review_rounds_monotonic
BEFORE UPDATE OR DELETE ON review.torrent_review_rounds
FOR EACH ROW EXECUTE FUNCTION review.protect_torrent_review_round();

REVOKE ALL ON ALL TABLES IN SCHEMA review FROM PUBLIC;

-- +goose Down

DROP TRIGGER IF EXISTS torrent_review_rounds_monotonic ON review.torrent_review_rounds;
DROP FUNCTION IF EXISTS review.protect_torrent_review_round();
DROP TRIGGER IF EXISTS torrent_review_votes_immutable ON review.torrent_review_votes;

ALTER TABLE review.torrent_review_rounds
    DROP CONSTRAINT IF EXISTS torrent_review_rounds_final_decision_fkey;
ALTER TABLE review.torrent_decisions
    DROP CONSTRAINT IF EXISTS torrent_decisions_round_source_check,
    DROP COLUMN IF EXISTS review_round_id,
    DROP COLUMN IF EXISTS resolution_source;

DROP TABLE IF EXISTS review.torrent_review_votes;
DROP TABLE IF EXISTS review.torrent_review_rounds;

DELETE FROM authz.role_permissions WHERE action = 'torrent.review.vote';
DELETE FROM authz.permissions WHERE action = 'torrent.review.vote';
