package progression

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var levelTransitionNamespace = uuid.MustParse("8d19c69f-e75d-5d32-a6b6-d6b77ea3cf66")

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, ErrInput
	}
	return &PostgresRepository{pool: pool}, nil
}

func (repository *PostgresRepository) Record(ctx context.Context, command RecordCommand) (Entry, error) {
	normalized, err := normalizeRecordCommand(command)
	if err != nil {
		return Entry{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Entry{}, fmt.Errorf("begin progression transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := repository.recordInTransaction(ctx, tx, normalized)
	if err != nil {
		return Entry{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Entry{}, classifyProgressionDatabaseError("commit progression transaction", err)
	}
	return result, nil
}

// RecordInTransaction lets a Core workflow reuse the only progression write
// boundary while atomically coupling it to another Core-owned ledger.  The
// caller owns the surrounding transaction.
func (repository *PostgresRepository) RecordInTransaction(ctx context.Context, tx pgx.Tx, command RecordCommand) (Entry, error) {
	if tx == nil {
		return Entry{}, ErrInput
	}
	normalized, err := normalizeRecordCommand(command)
	if err != nil {
		return Entry{}, err
	}
	return repository.recordInTransaction(ctx, tx, normalized)
}

func (repository *PostgresRepository) recordInTransaction(ctx context.Context, tx pgx.Tx, normalized RecordCommand) (Entry, error) {

	// The global idempotency lock makes replay lookup and insert one critical
	// section without serializing independent experience sources.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, normalized.IdempotencyKey); err != nil {
		return Entry{}, fmt.Errorf("lock progression idempotency key: %w", err)
	}
	if replayed, found, err := readProgressionReplay(ctx, tx, normalized); found || err != nil {
		if err != nil {
			return Entry{}, err
		}
		return replayed, nil
	}

	// A policy must both exist and have been created before the event. This
	// prevents a later revision from being backdated to manufacture experience.
	var policySource string
	if err := tx.QueryRow(ctx, `
SELECT source_kind
FROM progression.experience_policy_revisions
WHERE revision = $1
  AND created_at <= $2
  AND effective_from <= $2`, normalized.PolicyRevision, normalized.OccurredAt).Scan(&policySource); errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, ErrPolicyNotFound
	} else if err != nil {
		return Entry{}, fmt.Errorf("read experience policy: %w", err)
	}
	if policySource != string(normalized.SourceKind) {
		return Entry{}, ErrPolicyMismatch
	}

	var userExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM identity.users WHERE id = $1)`, normalized.UserID).Scan(&userExists); err != nil {
		return Entry{}, fmt.Errorf("read progression user: %w", err)
	}
	if !userExists {
		return Entry{}, ErrUserNotFound
	}
	var initialLevel int16
	if err := tx.QueryRow(ctx, `
SELECT level
FROM progression.level_definitions
WHERE policy_version = $1
  AND minimum_experience <= 0
ORDER BY minimum_experience DESC
LIMIT 1`, normalized.LevelPolicyVersion).Scan(&initialLevel); errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, ErrLevelPolicyNotFound
	} else if err != nil {
		return Entry{}, fmt.Errorf("read initial level definition: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO progression.user_progress (
    user_id, experience, level, policy_version, version, updated_at
) VALUES ($1, 0, $2, $3, 1, $4)
ON CONFLICT (user_id) DO NOTHING`, normalized.UserID, initialLevel, normalized.LevelPolicyVersion, normalized.RecordedAt); err != nil {
		return Entry{}, classifyProgressionDatabaseError("ensure user progress", err)
	}

	var previousBalanceText, previousPolicy string
	var previousLevel int16
	if err := tx.QueryRow(ctx, `
SELECT experience::text, level, policy_version
FROM progression.user_progress
WHERE user_id = $1
FOR UPDATE`, normalized.UserID).Scan(&previousBalanceText, &previousLevel, &previousPolicy); err != nil {
		return Entry{}, classifyProgressionDatabaseError("lock user progress", err)
	}

	var conflictingKey string
	err := tx.QueryRow(ctx, `
SELECT idempotency_key
FROM progression.experience_entries
WHERE user_id = $1
  AND entry_type = $2
  AND source_reference = $3`, normalized.UserID, string(normalized.EntryType), normalized.SourceReference).Scan(&conflictingKey)
	if err == nil {
		return Entry{}, ErrIdempotencyConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, fmt.Errorf("read progression source replay: %w", err)
	}

	if normalized.SourceKind == SourceSeedingReward ||
		(normalized.SourceKind == SourceActivity && normalized.MagicTransactionID != uuid.Nil) {
		expectedTransactionType := "seeding_reward"
		if normalized.SourceKind == SourceActivity {
			expectedTransactionType = "activity_reward"
		}
		var linked bool
		if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM economy.magic_transactions AS transaction
    JOIN economy.magic_postings AS posting
      ON posting.transaction_id = transaction.id
    WHERE transaction.id = $1
	  AND transaction.transaction_type = $3
      AND posting.account_id = $2
      AND posting.amount > 0
)`, normalized.MagicTransactionID, normalized.UserID, expectedTransactionType).Scan(&linked); err != nil {
			return Entry{}, fmt.Errorf("verify linked magic transaction: %w", err)
		}
		if !linked {
			return Entry{}, ErrInvariant
		}
	}

	var balanceAfterText string
	var levelAfter int16
	if err := tx.QueryRow(ctx, `
WITH next_balance AS (
    SELECT ($1::numeric(38, 20) + $2::numeric(38, 20))::numeric(38, 20) AS value
)
SELECT next_balance.value::text, definition.level
FROM next_balance
JOIN LATERAL (
    SELECT level
    FROM progression.level_definitions
    WHERE policy_version = $3
      AND minimum_experience <= next_balance.value
    ORDER BY minimum_experience DESC
    LIMIT 1
) AS definition ON true
WHERE next_balance.value >= 0`, previousBalanceText, normalized.Amount.String(), normalized.LevelPolicyVersion).Scan(&balanceAfterText, &levelAfter); errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, ErrInsufficientXP
	} else if err != nil {
		return Entry{}, classifyProgressionDatabaseError("calculate experience projection", err)
	}
	balanceAfter, err := ParseAmount(balanceAfterText)
	if err != nil {
		return Entry{}, fmt.Errorf("%w: invalid projected experience", ErrInvariant)
	}

	var sequence int64
	if err := tx.QueryRow(ctx, `
INSERT INTO progression.experience_entries (
    id, idempotency_key, user_id, entry_type, amount, balance_after,
    source_reference, source_kind, policy_revision, level_policy_version,
    level_after, payload_sha256, magic_transaction_id, occurred_at, recorded_at
) VALUES (
    $1, $2, $3, $4, $5::numeric(38, 20), $6::numeric(38, 20),
    $7, $8, $9, $10, $11, $12,
    NULLIF($13::uuid, '00000000-0000-0000-0000-000000000000'::uuid), $14, $15
)
RETURNING entry_sequence`, normalized.EntryID, normalized.IdempotencyKey, normalized.UserID,
		string(normalized.EntryType), normalized.Amount.String(), balanceAfter.String(),
		normalized.SourceReference, string(normalized.SourceKind), normalized.PolicyRevision,
		normalized.LevelPolicyVersion, levelAfter, normalized.PayloadSHA256[:],
		normalized.MagicTransactionID, normalized.OccurredAt, normalized.RecordedAt).Scan(&sequence); err != nil {
		return Entry{}, classifyProgressionDatabaseError("insert experience entry", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE progression.user_progress
SET experience = $2::numeric(38, 20),
    level = $3,
    policy_version = $4,
    version = version + 1,
    updated_at = $5
WHERE user_id = $1`, normalized.UserID, balanceAfter.String(), levelAfter,
		normalized.LevelPolicyVersion, normalized.RecordedAt); err != nil {
		return Entry{}, classifyProgressionDatabaseError("update user progress projection", err)
	}

	transitioned := previousLevel != levelAfter || previousPolicy != normalized.LevelPolicyVersion
	if transitioned {
		transitionID := uuid.NewSHA1(levelTransitionNamespace, []byte(normalized.EntryID.String()))
		if _, err := tx.Exec(ctx, `
INSERT INTO progression.level_transitions (
    id, experience_entry_id, user_id, from_level, to_level,
    from_policy_version, to_policy_version, occurred_at, recorded_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, transitionID,
			normalized.EntryID, normalized.UserID, previousLevel, levelAfter,
			previousPolicy, normalized.LevelPolicyVersion, normalized.OccurredAt, normalized.RecordedAt); err != nil {
			return Entry{}, classifyProgressionDatabaseError("insert level transition", err)
		}
	}

	return Entry{
		ID: normalized.EntryID, EntrySequence: sequence, IdempotencyKey: normalized.IdempotencyKey,
		UserID: normalized.UserID, EntryType: normalized.EntryType, Amount: normalized.Amount,
		BalanceAfter: balanceAfter, SourceReference: normalized.SourceReference,
		SourceKind: normalized.SourceKind, PolicyRevision: normalized.PolicyRevision,
		LevelPolicyVersion: normalized.LevelPolicyVersion, LevelAfter: levelAfter,
		PayloadSHA256: normalized.PayloadSHA256, MagicTransactionID: normalized.MagicTransactionID,
		OccurredAt: normalized.OccurredAt, RecordedAt: normalized.RecordedAt,
		LevelTransition: transitioned,
	}, nil
}

func readProgressionReplay(ctx context.Context, tx pgx.Tx, command RecordCommand) (Entry, bool, error) {
	var result Entry
	var entryType, sourceKind, amountText, balanceText string
	var magicTransaction pgtype.UUID
	var payload []byte
	err := tx.QueryRow(ctx, `
SELECT
    entry.id, entry.entry_sequence, entry.idempotency_key, entry.user_id,
    entry.entry_type, entry.amount::text, entry.balance_after::text,
    entry.source_reference, entry.source_kind, entry.policy_revision,
    entry.level_policy_version, entry.level_after, entry.payload_sha256,
    entry.magic_transaction_id, entry.occurred_at, entry.recorded_at,
    EXISTS (
        SELECT 1 FROM progression.level_transitions AS transition
        WHERE transition.experience_entry_id = entry.id
    )
FROM progression.experience_entries AS entry
WHERE entry.idempotency_key = $1`, command.IdempotencyKey).Scan(
		&result.ID, &result.EntrySequence, &result.IdempotencyKey, &result.UserID,
		&entryType, &amountText, &balanceText, &result.SourceReference, &sourceKind,
		&result.PolicyRevision, &result.LevelPolicyVersion, &result.LevelAfter, &payload,
		&magicTransaction, &result.OccurredAt, &result.RecordedAt, &result.LevelTransition,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, true, fmt.Errorf("read progression replay: %w", err)
	}
	result.EntryType = EntryType(entryType)
	result.SourceKind = SourceKind(sourceKind)
	result.Amount, err = ParseAmount(amountText)
	if err != nil {
		return Entry{}, true, fmt.Errorf("%w: invalid replay amount", ErrInvariant)
	}
	result.BalanceAfter, err = ParseAmount(balanceText)
	if err != nil {
		return Entry{}, true, fmt.Errorf("%w: invalid replay balance", ErrInvariant)
	}
	if magicTransaction.Valid {
		result.MagicTransactionID = uuid.UUID(magicTransaction.Bytes)
	}
	if len(payload) == 32 {
		copy(result.PayloadSHA256[:], payload)
	}
	if result.ID != command.EntryID || result.UserID != command.UserID ||
		result.EntryType != command.EntryType || result.Amount != command.Amount ||
		result.SourceReference != command.SourceReference || result.SourceKind != command.SourceKind ||
		result.PolicyRevision != command.PolicyRevision || result.LevelPolicyVersion != command.LevelPolicyVersion ||
		result.MagicTransactionID != command.MagicTransactionID || !bytes.Equal(payload, command.PayloadSHA256[:]) ||
		!result.OccurredAt.Equal(command.OccurredAt) || !result.RecordedAt.Equal(command.RecordedAt) {
		return Entry{}, true, ErrIdempotencyConflict
	}
	result.Replayed = true
	return result, true, nil
}

func classifyProgressionDatabaseError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "22003":
			return fmt.Errorf("%w: %s: %v", ErrInvariant, operation, err)
		case "23505":
			return fmt.Errorf("%w: %s: %v", ErrIdempotencyConflict, operation, err)
		case "P0001", "23514", "23503":
			return fmt.Errorf("%w: %s: %v", ErrInvariant, operation, err)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ Repository = (*PostgresRepository)(nil)
