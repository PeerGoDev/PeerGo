-- +goose Up

-- Experience source policy is append-only. The runtime accepts only a named
-- revision whose source kind was fixed before the underlying event occurred;
-- callers cannot turn an arbitrary positive number into experience merely by
-- labelling it as a reward.
CREATE TABLE progression.experience_policy_revisions (
    revision text PRIMARY KEY CHECK (
        revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    source_kind text NOT NULL CHECK (
        source_kind IN (
            'legacy_opening',
            'seeding_reward',
            'torrent_publish',
            'activity',
            'assessment',
            'administrator_adjustment'
        )
    ),
    effective_from timestamptz NOT NULL,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    created_at timestamptz NOT NULL CHECK (created_at >= effective_from)
);

INSERT INTO progression.experience_policy_revisions (
    revision, source_kind, effective_from, payload_sha256, created_at
) VALUES (
    'rousi-cutover-v1',
    'legacy_opening',
    '-infinity',
    decode('9c7a23b4989d6352e0041c13e28fa3014fa107aeeada05b7eea7b6ea5019b922', 'hex'),
    '2026-08-15T00:00:00Z'
);

CREATE TRIGGER progression_experience_policy_revisions_immutable
BEFORE UPDATE OR DELETE ON progression.experience_policy_revisions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- Existing Rousi openings are upgraded in place. Their operational opening
-- fingerprint already commits to the exact imported experience, so it is also
-- the canonical replay digest for the experience entry.
DROP TRIGGER progression_experience_entries_immutable
    ON progression.experience_entries;

ALTER TABLE progression.experience_entries
    ADD COLUMN idempotency_key text,
    ADD COLUMN source_kind text,
    ADD COLUMN level_policy_version text,
    ADD COLUMN level_after smallint,
    ADD COLUMN payload_sha256 bytea;

UPDATE progression.experience_entries AS entry
SET idempotency_key = 'legacy-opening:' || entry.source_reference,
    source_kind = 'legacy_opening',
    level_policy_version = progress.policy_version,
    level_after = progress.level,
    payload_sha256 = transaction.payload_sha256
FROM progression.user_progress AS progress,
     economy.magic_transactions AS transaction
WHERE progress.user_id = entry.user_id
  AND transaction.id = entry.magic_transaction_id
  AND entry.entry_type = 'legacy_opening';

ALTER TABLE progression.experience_entries
    ALTER COLUMN idempotency_key SET NOT NULL,
    ALTER COLUMN source_kind SET NOT NULL,
    ALTER COLUMN level_policy_version SET NOT NULL,
    ALTER COLUMN level_after SET NOT NULL,
    ALTER COLUMN payload_sha256 SET NOT NULL,
    ADD CONSTRAINT experience_entries_idempotency_key_format CHECK (
        idempotency_key ~ '^[a-z0-9][a-z0-9:._-]{0,191}$'
    ),
    ADD CONSTRAINT experience_entries_source_kind_check CHECK (
        source_kind IN (
            'legacy_opening',
            'seeding_reward',
            'torrent_publish',
            'activity',
            'assessment',
            'administrator_adjustment'
        )
    ),
    ADD CONSTRAINT experience_entries_payload_size CHECK (
        octet_length(payload_sha256) = 32
    ),
    ADD CONSTRAINT experience_entries_runtime_amount_check CHECK (
        entry_type = 'legacy_opening' OR amount <> 0
    ),
    ADD CONSTRAINT experience_entries_legacy_shape CHECK (
        (entry_type = 'legacy_opening') = (source_kind = 'legacy_opening')
    ),
    ADD CONSTRAINT experience_entries_seeding_link CHECK (
        source_kind <> 'seeding_reward' OR magic_transaction_id IS NOT NULL
    ),
    ADD CONSTRAINT experience_entries_policy_fk
        FOREIGN KEY (policy_revision)
        REFERENCES progression.experience_policy_revisions (revision)
        ON DELETE RESTRICT,
    ADD CONSTRAINT experience_entries_level_definition_fk
        FOREIGN KEY (level_policy_version, level_after)
        REFERENCES progression.level_definitions (policy_version, level)
        ON DELETE RESTRICT,
    ADD CONSTRAINT experience_entries_idempotency_key_unique
        UNIQUE (idempotency_key);

CREATE TRIGGER progression_experience_entries_immutable
BEFORE UPDATE OR DELETE ON progression.experience_entries
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- A transition is emitted for both a numeric level change and a level-policy
-- switch. It is immutable evidence; notification delivery can consume it later
-- without making the experience transaction depend on an asynchronous send.
CREATE TABLE progression.level_transitions (
    id uuid PRIMARY KEY,
    experience_entry_id uuid NOT NULL UNIQUE
        REFERENCES progression.experience_entries (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    from_level smallint NOT NULL,
    to_level smallint NOT NULL,
    from_policy_version text NOT NULL,
    to_policy_version text NOT NULL,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    FOREIGN KEY (from_policy_version, from_level)
        REFERENCES progression.level_definitions (policy_version, level)
        ON DELETE RESTRICT,
    FOREIGN KEY (to_policy_version, to_level)
        REFERENCES progression.level_definitions (policy_version, level)
        ON DELETE RESTRICT,
    CHECK (
        from_level <> to_level
        OR from_policy_version <> to_policy_version
    ),
    CHECK (recorded_at >= occurred_at)
);

CREATE INDEX progression_level_transitions_user_recorded_idx
    ON progression.level_transitions (user_id, recorded_at DESC, id DESC);

CREATE TRIGGER progression_level_transitions_immutable
BEFORE UPDATE OR DELETE ON progression.level_transitions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

REVOKE ALL ON progression.experience_policy_revisions FROM PUBLIC;
REVOKE ALL ON progression.level_transitions FROM PUBLIC;

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM progression.experience_entries
        WHERE entry_type <> 'legacy_opening'
    ) OR EXISTS (
        SELECT 1 FROM progression.level_transitions
    ) THEN
        RAISE EXCEPTION '202608150013 cannot roll back after runtime progression entries exist';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER progression_level_transitions_immutable
    ON progression.level_transitions;
DROP INDEX progression.progression_level_transitions_user_recorded_idx;
DROP TABLE progression.level_transitions;

ALTER TABLE progression.experience_entries
    DROP CONSTRAINT experience_entries_idempotency_key_unique,
    DROP CONSTRAINT experience_entries_level_definition_fk,
    DROP CONSTRAINT experience_entries_policy_fk,
    DROP CONSTRAINT experience_entries_seeding_link,
    DROP CONSTRAINT experience_entries_legacy_shape,
    DROP CONSTRAINT experience_entries_runtime_amount_check,
    DROP CONSTRAINT experience_entries_payload_size,
    DROP CONSTRAINT experience_entries_source_kind_check,
    DROP CONSTRAINT experience_entries_idempotency_key_format,
    DROP COLUMN payload_sha256,
    DROP COLUMN level_after,
    DROP COLUMN level_policy_version,
    DROP COLUMN source_kind,
    DROP COLUMN idempotency_key;

DROP TRIGGER progression_experience_policy_revisions_immutable
    ON progression.experience_policy_revisions;
DROP TABLE progression.experience_policy_revisions;
