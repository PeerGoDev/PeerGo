-- +goose Up

-- Historical reward repair is never allowed to replace a closed calculation.
-- An operator first approves one exact mode-0600 preview artifact by SHA-256;
-- each positive user-hour delta is then linked to ordinary immutable magic and
-- experience ledger entries. A crash may leave an incomplete approval, but
-- source-unique receipts make replay resume without issuing a second credit.
CREATE TABLE economy.seeding_reward_compensation_approvals (
    artifact_sha256 bytea PRIMARY KEY CHECK (octet_length(artifact_sha256) = 32),
    artifact_size_bytes bigint NOT NULL CHECK (artifact_size_bytes > 0),
    schema_version text NOT NULL CHECK (
        schema_version = 'seeding.reward.compensation.preview.v1'
    ),
    tracker_source_stream text NOT NULL CHECK (
        tracker_source_stream = 'PEERGO_TRACKER_ANNOUNCE_V1'
    ),
    tracker_fence_sequence bigint NOT NULL CHECK (tracker_fence_sequence > 0),
    first_window timestamptz NOT NULL,
    last_window timestamptz NOT NULL,
    record_count bigint NOT NULL CHECK (record_count BETWEEN 1 AND 500000),
    magic_delta bigint NOT NULL CHECK (magic_delta > 0),
    experience_delta numeric(38, 20) NOT NULL CHECK (experience_delta >= 0),
    operator_reference text NOT NULL CHECK (
        operator_reference ~ '^[a-z0-9][a-z0-9:._-]{0,127}$'
    ),
    approved_at timestamptz NOT NULL,
    CHECK (
        first_window = date_trunc('hour', first_window)
        AND last_window = date_trunc('hour', last_window)
        AND last_window >= first_window
        AND approved_at >= last_window + interval '1 hour'
    )
);

CREATE TABLE economy.seeding_reward_compensation_receipts (
    artifact_sha256 bytea NOT NULL
        REFERENCES economy.seeding_reward_compensation_approvals (artifact_sha256)
        ON DELETE RESTRICT,
    source_reference text PRIMARY KEY CHECK (
        source_reference ~ '^seeding_compensation:v1:[0-9]+:[0-9a-f-]{36}$'
    ),
    window_start timestamptz NOT NULL
        REFERENCES economy.seeding_reward_evidence_windows (window_start)
        ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    policy_revision text NOT NULL
        REFERENCES economy.seeding_reward_policy_revisions (revision)
        ON DELETE RESTRICT,
    benefit_revision text NOT NULL CHECK (
        benefit_revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    level_policy_version text NOT NULL
        REFERENCES progression.level_policy_revisions (policy_version)
        ON DELETE RESTRICT,
    original_calculation_sha256 bytea CHECK (
        original_calculation_sha256 IS NULL
        OR octet_length(original_calculation_sha256) = 32
    ),
    corrected_calculation_sha256 bytea NOT NULL UNIQUE CHECK (
        octet_length(corrected_calculation_sha256) = 32
    ),
    corrected_evidence_sha256 bytea NOT NULL CHECK (
        octet_length(corrected_evidence_sha256) = 32
    ),
    original_reward bigint NOT NULL CHECK (original_reward >= 0),
    corrected_reward bigint NOT NULL CHECK (corrected_reward > 0),
    magic_delta bigint NOT NULL CHECK (magic_delta > 0),
    experience_delta numeric(38, 20) NOT NULL CHECK (experience_delta >= 0),
    eligible_torrent_count integer NOT NULL CHECK (eligible_torrent_count >= 0),
    capped boolean NOT NULL,
    magic_transaction_id uuid NOT NULL UNIQUE
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    experience_entry_id uuid UNIQUE
        REFERENCES progression.experience_entries (id) ON DELETE RESTRICT,
    applied_at timestamptz NOT NULL,
    UNIQUE (artifact_sha256, window_start, user_id),
    CHECK (corrected_reward = original_reward + magic_delta),
    CHECK ((experience_delta = 0) = (experience_entry_id IS NULL)),
    CHECK (applied_at >= window_start + interval '1 hour')
);

CREATE INDEX seeding_reward_compensation_receipts_artifact_idx
    ON economy.seeding_reward_compensation_receipts (artifact_sha256, window_start, user_id);

CREATE TABLE economy.seeding_reward_compensation_completions (
    artifact_sha256 bytea PRIMARY KEY
        REFERENCES economy.seeding_reward_compensation_approvals (artifact_sha256)
        ON DELETE RESTRICT,
    receipt_count bigint NOT NULL CHECK (receipt_count BETWEEN 1 AND 500000),
    magic_delta bigint NOT NULL CHECK (magic_delta > 0),
    experience_delta numeric(38, 20) NOT NULL CHECK (experience_delta >= 0),
    completed_at timestamptz NOT NULL
);

CREATE TRIGGER seeding_reward_compensation_approvals_immutable
BEFORE UPDATE OR DELETE ON economy.seeding_reward_compensation_approvals
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER seeding_reward_compensation_receipts_immutable
BEFORE UPDATE OR DELETE ON economy.seeding_reward_compensation_receipts
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER seeding_reward_compensation_completions_immutable
BEFORE UPDATE OR DELETE ON economy.seeding_reward_compensation_completions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

REVOKE ALL ON economy.seeding_reward_compensation_approvals FROM PUBLIC;
REVOKE ALL ON economy.seeding_reward_compensation_receipts FROM PUBLIC;
REVOKE ALL ON economy.seeding_reward_compensation_completions FROM PUBLIC;

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM economy.seeding_reward_compensation_approvals) THEN
        RAISE EXCEPTION '202608220001 cannot roll back after a compensation artifact was approved';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER seeding_reward_compensation_completions_immutable
    ON economy.seeding_reward_compensation_completions;
DROP TRIGGER seeding_reward_compensation_receipts_immutable
    ON economy.seeding_reward_compensation_receipts;
DROP TRIGGER seeding_reward_compensation_approvals_immutable
    ON economy.seeding_reward_compensation_approvals;
DROP TABLE economy.seeding_reward_compensation_completions;
DROP INDEX economy.seeding_reward_compensation_receipts_artifact_idx;
DROP TABLE economy.seeding_reward_compensation_receipts;
DROP TABLE economy.seeding_reward_compensation_approvals;
