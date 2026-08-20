-- +goose Up

-- The imported opening statement is the only existing economy writer before
-- this migration. Refuse to reinterpret unknown runtime entries as balanced
-- transactions during an upgrade.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM economy.magic_ledger_entries
        WHERE entry_type <> 'legacy_opening'
    ) THEN
        RAISE EXCEPTION 'economy ledger contains unsupported pre-kernel entries';
    END IF;
END;
$$;
-- +goose StatementEnd

-- Existing member account IDs deliberately equal the user UUID. This keeps
-- the imported projection stable while allowing explicitly named system
-- accounts to balance mint, sink and migration transactions.
ALTER TABLE economy.magic_accounts
    ADD COLUMN id uuid,
    ADD COLUMN account_kind text,
    ADD COLUMN account_code text;

UPDATE economy.magic_accounts
SET id = user_id,
    account_kind = 'member',
    account_code = 'member:' || user_id::text;

ALTER TABLE economy.magic_accounts
    DROP CONSTRAINT magic_accounts_pkey;

ALTER TABLE economy.magic_accounts
    ALTER COLUMN id SET NOT NULL,
    ALTER COLUMN account_kind SET NOT NULL,
    ALTER COLUMN account_code SET NOT NULL,
    ALTER COLUMN user_id DROP NOT NULL,
    ADD CONSTRAINT magic_accounts_pkey PRIMARY KEY (id),
    ADD CONSTRAINT magic_accounts_code_unique UNIQUE (account_code),
    ADD CONSTRAINT magic_accounts_member_unique UNIQUE (user_id),
    ADD CONSTRAINT magic_accounts_shape CHECK (
        (
            account_kind = 'member'
            AND user_id IS NOT NULL
            AND id = user_id
            AND account_code = 'member:' || user_id::text
        )
        OR
        (
            account_kind = 'system'
            AND user_id IS NULL
            AND account_code ~ '^system:[a-z][a-z0-9:_-]{0,95}$'
        )
    );
INSERT INTO economy.magic_accounts (
    id, user_id, account_kind, account_code, balance, version, updated_at
) VALUES
    ('00000000-0000-7000-8000-000000000001', NULL, 'system', 'system:migration:rousi', 0, 1, clock_timestamp()),
    ('00000000-0000-7000-8000-000000000002', NULL, 'system', 'system:mint:seeding', 0, 1, clock_timestamp()),
    ('00000000-0000-7000-8000-000000000003', NULL, 'system', 'system:mint:activity', 0, 1, clock_timestamp()),
    ('00000000-0000-7000-8000-000000000004', NULL, 'system', 'system:sink:torrent_purchase', 0, 1, clock_timestamp()),
    ('00000000-0000-7000-8000-000000000005', NULL, 'system', 'system:sink:fee', 0, 1, clock_timestamp());

CREATE TABLE economy.magic_transactions (
    id uuid PRIMARY KEY,
    ledger_sequence bigint GENERATED ALWAYS AS IDENTITY,
    transaction_type text NOT NULL CHECK (
        transaction_type ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    idempotency_key text NOT NULL CHECK (
        idempotency_key ~ '^[a-z0-9][a-z0-9:._-]{0,191}$'
    ),
    source_reference text NOT NULL CHECK (
        source_reference ~ '^[a-z0-9][a-z0-9:._-]{0,127}$'
    ),
    policy_revision text CHECK (
        policy_revision IS NULL
        OR policy_revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    posting_count smallint NOT NULL CHECK (posting_count BETWEEN 2 AND 32),
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    UNIQUE (ledger_sequence),
    UNIQUE (id, ledger_sequence),
    UNIQUE (idempotency_key),
    CHECK (recorded_at >= occurred_at)
);

INSERT INTO economy.magic_transactions (
    id,
    transaction_type,
    idempotency_key,
    source_reference,
    policy_revision,
    posting_count,
    payload_sha256,
    occurred_at,
    recorded_at
)
SELECT
    ledger.id,
    'legacy_opening',
    'legacy-opening:' || ledger.source_reference,
    ledger.source_reference,
    'rousi-cutover-v1',
    2,
    opening.source_fingerprint,
    ledger.occurred_at,
    ledger.recorded_at
FROM economy.magic_ledger_entries AS ledger
JOIN migration.user_operational_openings AS opening
  ON opening.user_id = ledger.user_id
WHERE ledger.entry_type = 'legacy_opening'
ORDER BY ledger.recorded_at, ledger.id;

DROP TRIGGER economy_magic_ledger_entries_immutable
    ON economy.magic_ledger_entries;

ALTER TABLE economy.magic_ledger_entries
    ADD COLUMN transaction_id uuid;

UPDATE economy.magic_ledger_entries
SET transaction_id = id;

ALTER TABLE economy.magic_ledger_entries
    ALTER COLUMN transaction_id SET NOT NULL,
    ADD CONSTRAINT magic_ledger_entries_transaction_fk
        FOREIGN KEY (transaction_id)
        REFERENCES economy.magic_transactions (id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT magic_ledger_entries_transaction_user_unique
        UNIQUE (transaction_id, user_id),
    DROP CONSTRAINT magic_ledger_entries_entry_type_source_reference_key,
    ADD CONSTRAINT magic_ledger_entries_user_source_unique
        UNIQUE (user_id, entry_type, source_reference);

CREATE TRIGGER economy_magic_ledger_entries_immutable
BEFORE UPDATE OR DELETE ON economy.magic_ledger_entries
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TABLE economy.magic_postings (
    transaction_id uuid NOT NULL,
    ledger_sequence bigint NOT NULL,
    posting_index smallint NOT NULL CHECK (posting_index BETWEEN 0 AND 31),
    account_id uuid NOT NULL REFERENCES economy.magic_accounts (id) ON DELETE RESTRICT,
    amount bigint NOT NULL,
    balance_after bigint NOT NULL,
    PRIMARY KEY (transaction_id, posting_index),
    UNIQUE (transaction_id, account_id),
    FOREIGN KEY (transaction_id, ledger_sequence)
        REFERENCES economy.magic_transactions (id, ledger_sequence)
        ON DELETE RESTRICT
);

CREATE INDEX magic_postings_account_sequence_idx
    ON economy.magic_postings (account_id, ledger_sequence DESC);

WITH opening_postings AS (
    SELECT
        transaction.id AS transaction_id,
        transaction.ledger_sequence,
        ledger.user_id AS member_account_id,
        ledger.amount,
        ledger.balance_after AS member_balance_after,
        -sum(ledger.amount) OVER (
            ORDER BY transaction.ledger_sequence
            ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
        ) AS migration_balance_after
    FROM economy.magic_ledger_entries AS ledger
    JOIN economy.magic_transactions AS transaction
      ON transaction.id = ledger.transaction_id
)
INSERT INTO economy.magic_postings (
    transaction_id, ledger_sequence, posting_index,
    account_id, amount, balance_after
)
SELECT
    transaction_id,
    ledger_sequence,
    0,
    member_account_id,
    amount,
    member_balance_after
FROM opening_postings
UNION ALL
SELECT
    transaction_id,
    ledger_sequence,
    1,
    '00000000-0000-7000-8000-000000000001'::uuid,
    -amount,
    migration_balance_after
FROM opening_postings;

UPDATE economy.magic_accounts
SET balance = (
        SELECT COALESCE(sum(posting.amount), 0)::bigint
        FROM economy.magic_postings AS posting
        WHERE posting.account_id = '00000000-0000-7000-8000-000000000001'
    ),
    version = 1 + (
        SELECT count(*)
        FROM economy.magic_postings AS posting
        WHERE posting.account_id = '00000000-0000-7000-8000-000000000001'
    ),
    updated_at = COALESCE((
        SELECT max(transaction.recorded_at)
        FROM economy.magic_transactions AS transaction
        JOIN economy.magic_postings AS posting
          ON posting.transaction_id = transaction.id
        WHERE posting.account_id = '00000000-0000-7000-8000-000000000001'
    ), updated_at)
WHERE id = '00000000-0000-7000-8000-000000000001';

-- +goose StatementBegin
CREATE FUNCTION economy.assert_magic_transaction_consistent()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_transaction_id uuid;
    expected_posting_count smallint;
    actual_posting_count bigint;
    posting_sum numeric;
BEGIN
    IF TG_TABLE_NAME = 'magic_transactions' THEN
        target_transaction_id := NEW.id;
    ELSE
        target_transaction_id := NEW.transaction_id;
    END IF;

    SELECT transaction.posting_count
    INTO expected_posting_count
    FROM economy.magic_transactions AS transaction
    WHERE transaction.id = target_transaction_id;

    SELECT count(*), COALESCE(sum(posting.amount), 0)
    INTO actual_posting_count, posting_sum
    FROM economy.magic_postings AS posting
    WHERE posting.transaction_id = target_transaction_id;

    IF expected_posting_count IS NULL
       OR actual_posting_count <> expected_posting_count
       OR posting_sum <> 0 THEN
        RAISE EXCEPTION 'magic transaction % is not balanced and complete', target_transaction_id;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM economy.magic_postings AS current_posting
        LEFT JOIN LATERAL (
            SELECT previous_posting.balance_after
            FROM economy.magic_postings AS previous_posting
            WHERE previous_posting.account_id = current_posting.account_id
              AND previous_posting.ledger_sequence < current_posting.ledger_sequence
            ORDER BY previous_posting.ledger_sequence DESC
            LIMIT 1
        ) AS previous ON true
        WHERE current_posting.transaction_id = target_transaction_id
          AND current_posting.balance_after
              <> COALESCE(previous.balance_after, 0) + current_posting.amount
    ) THEN
        RAISE EXCEPTION 'magic transaction % has an invalid balance chain', target_transaction_id;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM (
            SELECT DISTINCT posting.account_id
            FROM economy.magic_postings AS posting
            WHERE posting.transaction_id = target_transaction_id
        ) AS affected
        JOIN economy.magic_accounts AS account ON account.id = affected.account_id
        JOIN LATERAL (
            SELECT latest.balance_after
            FROM economy.magic_postings AS latest
            WHERE latest.account_id = affected.account_id
            ORDER BY latest.ledger_sequence DESC
            LIMIT 1
        ) AS projected ON true
        WHERE account.balance <> projected.balance_after
    ) THEN
        RAISE EXCEPTION 'magic transaction % does not match account projections', target_transaction_id;
    END IF;

    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER magic_transactions_consistent
AFTER INSERT ON economy.magic_transactions
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION economy.assert_magic_transaction_consistent();

CREATE CONSTRAINT TRIGGER magic_postings_consistent
AFTER INSERT ON economy.magic_postings
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION economy.assert_magic_transaction_consistent();

CREATE TRIGGER economy_magic_transactions_immutable
BEFORE UPDATE OR DELETE ON economy.magic_transactions
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

CREATE TRIGGER economy_magic_postings_immutable
BEFORE UPDATE OR DELETE ON economy.magic_postings
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- Experience remains exact numeric because the imported PtYes values contain
-- fractional units. It is a separate append-only progression ledger and never
-- uses float64 money arithmetic.
CREATE TABLE progression.experience_entries (
    id uuid PRIMARY KEY,
    entry_sequence bigint GENERATED ALWAYS AS IDENTITY,
    user_id uuid NOT NULL REFERENCES identity.users (id) ON DELETE RESTRICT,
    entry_type text NOT NULL CHECK (
        entry_type IN ('legacy_opening', 'earn', 'reversal', 'adjustment')
    ),
    amount numeric(38, 20) NOT NULL,
    balance_after numeric(38, 20) NOT NULL CHECK (balance_after >= 0),
    source_reference text NOT NULL CHECK (
        source_reference ~ '^[a-z0-9][a-z0-9:._-]{0,127}$'
    ),
    policy_revision text CHECK (
        policy_revision IS NULL
        OR policy_revision ~ '^[a-z0-9][a-z0-9._-]{0,63}$'
    ),
    magic_transaction_id uuid
        REFERENCES economy.magic_transactions (id) ON DELETE RESTRICT,
    source_run_id uuid REFERENCES migration.runs (id) ON DELETE RESTRICT,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    UNIQUE (entry_sequence),
    UNIQUE (user_id, entry_type, source_reference),
    CHECK (recorded_at >= occurred_at),
    CHECK (entry_type <> 'legacy_opening' OR source_run_id IS NOT NULL)
);

CREATE INDEX experience_entries_user_sequence_idx
    ON progression.experience_entries (user_id, entry_sequence DESC);

INSERT INTO progression.experience_entries (
    id,
    user_id,
    entry_type,
    amount,
    balance_after,
    source_reference,
    policy_revision,
    magic_transaction_id,
    source_run_id,
    occurred_at,
    recorded_at
)
SELECT
    ledger.id,
    opening.user_id,
    'legacy_opening',
    opening.source_experience,
    opening.source_experience,
    ledger.source_reference,
    'rousi-cutover-v1',
    ledger.transaction_id,
    opening.first_run_id,
    ledger.occurred_at,
    opening.imported_at
FROM migration.user_operational_openings AS opening
JOIN economy.magic_ledger_entries AS ledger
  ON ledger.user_id = opening.user_id
 AND ledger.entry_type = 'legacy_opening'
ORDER BY ledger.recorded_at, ledger.id;

CREATE TRIGGER progression_experience_entries_immutable
BEFORE UPDATE OR DELETE ON progression.experience_entries
FOR EACH ROW EXECUTE FUNCTION migration.reject_append_only_mutation();

-- +goose StatementBegin
CREATE FUNCTION progression.assert_experience_entry_consistent()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    previous_balance numeric(38, 20);
    projected_balance numeric(38, 20);
    latest_balance numeric(38, 20);
BEGIN
    SELECT previous.balance_after
    INTO previous_balance
    FROM progression.experience_entries AS previous
    WHERE previous.user_id = NEW.user_id
      AND previous.entry_sequence < NEW.entry_sequence
    ORDER BY previous.entry_sequence DESC
    LIMIT 1;

    IF NEW.balance_after <> COALESCE(previous_balance, 0) + NEW.amount THEN
        RAISE EXCEPTION 'experience entry % has an invalid balance chain', NEW.id;
    END IF;

    SELECT progress.experience
    INTO projected_balance
    FROM progression.user_progress AS progress
    WHERE progress.user_id = NEW.user_id;

    SELECT latest.balance_after
    INTO latest_balance
    FROM progression.experience_entries AS latest
    WHERE latest.user_id = NEW.user_id
    ORDER BY latest.entry_sequence DESC
    LIMIT 1;

    IF projected_balance IS NULL OR projected_balance <> latest_balance THEN
        RAISE EXCEPTION 'experience entry % does not match user projection', NEW.id;
    END IF;

    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER experience_entries_consistent
AFTER INSERT ON progression.experience_entries
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION progression.assert_experience_entry_consistent();

REVOKE ALL ON economy.magic_transactions FROM PUBLIC;
REVOKE ALL ON economy.magic_postings FROM PUBLIC;
REVOKE ALL ON progression.experience_entries FROM PUBLIC;

-- +goose Down

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM economy.magic_transactions
        WHERE transaction_type <> 'legacy_opening'
    ) OR EXISTS (
        SELECT 1
        FROM progression.experience_entries
        WHERE entry_type <> 'legacy_opening'
    ) THEN
        RAISE EXCEPTION '202608150012 cannot roll back after runtime economy entries exist';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER experience_entries_consistent ON progression.experience_entries;
DROP FUNCTION progression.assert_experience_entry_consistent();
DROP TRIGGER progression_experience_entries_immutable ON progression.experience_entries;
DROP INDEX progression.experience_entries_user_sequence_idx;
DROP TABLE progression.experience_entries;

DROP TRIGGER economy_magic_postings_immutable ON economy.magic_postings;
DROP TRIGGER economy_magic_transactions_immutable ON economy.magic_transactions;
DROP TRIGGER magic_postings_consistent ON economy.magic_postings;
DROP TRIGGER magic_transactions_consistent ON economy.magic_transactions;
DROP FUNCTION economy.assert_magic_transaction_consistent();
DROP INDEX economy.magic_postings_account_sequence_idx;
DROP TABLE economy.magic_postings;

ALTER TABLE economy.magic_ledger_entries
    DROP CONSTRAINT magic_ledger_entries_user_source_unique,
    DROP CONSTRAINT magic_ledger_entries_transaction_user_unique,
    DROP CONSTRAINT magic_ledger_entries_transaction_fk,
    DROP COLUMN transaction_id,
    ADD CONSTRAINT magic_ledger_entries_entry_type_source_reference_key
        UNIQUE (entry_type, source_reference);

DROP TABLE economy.magic_transactions;

DELETE FROM economy.magic_accounts
WHERE account_kind = 'system';

ALTER TABLE economy.magic_accounts
    DROP CONSTRAINT magic_accounts_shape,
    DROP CONSTRAINT magic_accounts_member_unique,
    DROP CONSTRAINT magic_accounts_code_unique,
    DROP CONSTRAINT magic_accounts_pkey;

ALTER TABLE economy.magic_accounts
    ALTER COLUMN user_id SET NOT NULL,
    DROP COLUMN account_code,
    DROP COLUMN account_kind,
    DROP COLUMN id,
    ADD CONSTRAINT magic_accounts_pkey PRIMARY KEY (user_id);
