-- +goose Up

-- A contribution policy opens two runtime experience authorities: torrent
-- publication has its own source kind, while raw-upload and account-age are
-- both non-magic activities.  The suffixes keep the public administration
-- revision compact while preserving the progression ledger's source check.
ALTER TABLE progression.contribution_experience_policy_revisions
    ADD CONSTRAINT contribution_experience_policy_revision_length
    CHECK (char_length(revision) <= 55);

-- +goose StatementBegin
CREATE FUNCTION progression.open_contribution_experience_policies()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO progression.experience_policy_revisions (
        revision, source_kind, effective_from, payload_sha256, created_at
    ) VALUES
        (
            NEW.revision || '.publish', 'torrent_publish',
            NEW.effective_from, NEW.snapshot_sha256, NEW.created_at
        ),
        (
            NEW.revision || '.activity', 'activity',
            NEW.effective_from, NEW.snapshot_sha256, NEW.created_at
        );
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER contribution_experience_policy_opening
AFTER INSERT ON progression.contribution_experience_policy_revisions
FOR EACH ROW EXECUTE FUNCTION progression.open_contribution_experience_policies();

INSERT INTO progression.experience_policy_revisions (
    revision, source_kind, effective_from, payload_sha256, created_at
)
SELECT revision || '.publish', 'torrent_publish', effective_from,
       snapshot_sha256, created_at
FROM progression.contribution_experience_policy_revisions
UNION ALL
SELECT revision || '.activity', 'activity', effective_from,
       snapshot_sha256, created_at
FROM progression.contribution_experience_policy_revisions;

-- Projection order is assigned by Core when a privacy-minimized traffic
-- settlement is committed.  The experience worker consumes this local order;
-- it never needs Tracker credentials or direct Ledger access.
ALTER TABLE traffic.user_traffic_entries
    ADD COLUMN projection_sequence bigint GENERATED ALWAYS AS IDENTITY;

ALTER TABLE traffic.user_traffic_entries
    ADD CONSTRAINT user_traffic_entries_projection_sequence_unique
    UNIQUE (projection_sequence);

CREATE INDEX user_traffic_entries_upload_projection_idx
    ON traffic.user_traffic_entries (projection_sequence)
    WHERE raw_uploaded > 0;

CREATE TABLE progression.contribution_upload_cursor (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    last_projection_sequence bigint NOT NULL DEFAULT 0
        CHECK (last_projection_sequence >= 0),
    updated_at timestamptz NOT NULL
);

INSERT INTO progression.contribution_upload_cursor (
    singleton, last_projection_sequence, updated_at
) VALUES (true, 0, '2026-08-20T00:00:00Z');

-- Only a remainder smaller than one GiB is mutable.  Every whole-GiB crossing
-- is independently committed through progression.experience_entries, so a
-- crash cannot advance this checkpoint without the matching ledger entry.
CREATE TABLE progression.contribution_upload_remainders (
    user_id uuid PRIMARY KEY REFERENCES identity.users (id) ON DELETE RESTRICT,
    remainder_bytes bigint NOT NULL CHECK (
        remainder_bytes BETWEEN 0 AND 1073741823
    ),
    processed_raw_uploaded bigint NOT NULL CHECK (processed_raw_uploaded >= 0),
    version bigint NOT NULL CHECK (version > 0),
    updated_at timestamptz NOT NULL
);

-- Publication receipts are retained even when the configured reward is zero.
-- This prevents a later policy from retroactively rewarding an old publish.
CREATE TABLE progression.torrent_publish_experience_receipts (
    torrent_id bigint PRIMARY KEY
        REFERENCES torrents.torrents (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    published_at timestamptz NOT NULL,
    policy_revision text NOT NULL
        REFERENCES progression.contribution_experience_policy_revisions (revision)
        ON DELETE RESTRICT,
    experience_amount numeric(38, 20) NOT NULL CHECK (experience_amount >= 0),
    experience_entry_id uuid UNIQUE
        REFERENCES progression.experience_entries (id) ON DELETE RESTRICT,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    processed_at timestamptz NOT NULL CHECK (processed_at >= published_at),
    CHECK ((experience_amount = 0) = (experience_entry_id IS NULL))
);

CREATE TRIGGER torrent_publish_experience_receipts_immutable
BEFORE UPDATE OR DELETE ON progression.torrent_publish_experience_receipts
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE INDEX torrents_publish_experience_candidate_idx
    ON torrents.torrents (published_at, id)
    WHERE published_at IS NOT NULL;

-- Account age is credited in complete 24-hour periods starting at the later
-- of registration or the first contribution policy.  Migrated account history
-- is therefore not back-paid, while every post-cutover day remains replayable.
CREATE TABLE progression.account_age_experience_checkpoints (
    user_id uuid PRIMARY KEY REFERENCES identity.users (id) ON DELETE RESTRICT,
    anchor_at timestamptz NOT NULL,
    credited_days bigint NOT NULL CHECK (credited_days >= 0),
    version bigint NOT NULL CHECK (version > 0),
    updated_at timestamptz NOT NULL CHECK (updated_at >= anchor_at)
);

REVOKE ALL ON progression.contribution_upload_cursor FROM PUBLIC;
REVOKE ALL ON progression.contribution_upload_remainders FROM PUBLIC;
REVOKE ALL ON progression.torrent_publish_experience_receipts FROM PUBLIC;
REVOKE ALL ON progression.account_age_experience_checkpoints FROM PUBLIC;

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM progression.experience_entries AS entry
        JOIN progression.contribution_experience_policy_revisions AS policy
          ON entry.policy_revision IN (
              policy.revision || '.publish',
              policy.revision || '.activity'
          )
    ) THEN
        RAISE EXCEPTION '202608200002 cannot roll back after contribution experience was settled';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TABLE progression.account_age_experience_checkpoints;
DROP INDEX torrents.torrents_publish_experience_candidate_idx;
DROP TRIGGER torrent_publish_experience_receipts_immutable
    ON progression.torrent_publish_experience_receipts;
DROP TABLE progression.torrent_publish_experience_receipts;
DROP TABLE progression.contribution_upload_remainders;
DROP TABLE progression.contribution_upload_cursor;
DROP INDEX traffic.user_traffic_entries_upload_projection_idx;
ALTER TABLE traffic.user_traffic_entries
    DROP CONSTRAINT user_traffic_entries_projection_sequence_unique,
    DROP COLUMN projection_sequence;

ALTER TABLE progression.experience_policy_revisions DISABLE TRIGGER USER;
DELETE FROM progression.experience_policy_revisions AS experience
USING progression.contribution_experience_policy_revisions AS policy
WHERE experience.revision IN (
    policy.revision || '.publish',
    policy.revision || '.activity'
);
ALTER TABLE progression.experience_policy_revisions ENABLE TRIGGER USER;

DROP TRIGGER contribution_experience_policy_opening
    ON progression.contribution_experience_policy_revisions;
DROP FUNCTION progression.open_contribution_experience_policies();
ALTER TABLE progression.contribution_experience_policy_revisions
    DROP CONSTRAINT contribution_experience_policy_revision_length;
