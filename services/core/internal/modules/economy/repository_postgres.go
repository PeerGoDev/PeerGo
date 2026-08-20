package economy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var statementNamespace = uuid.MustParse("3fab6059-bfc6-5cb7-9781-6ed461e9f539")

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, ErrInput
	}
	return &PostgresRepository{pool: pool}, nil
}

type lockedAccount struct {
	ID      uuid.UUID
	UserID  *uuid.UUID
	Kind    string
	Balance int64
}

func (repository *PostgresRepository) Record(ctx context.Context, command RecordCommand) (Transaction, error) {
	normalized, err := normalizeRecordCommand(command)
	if err != nil {
		return Transaction{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Transaction{}, fmt.Errorf("begin economy transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := repository.recordInTransaction(ctx, tx, normalized)
	if err != nil {
		return Transaction{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Transaction{}, classifyDatabaseError("commit magic transaction", err)
	}
	return result, nil
}

// RecordInTransaction exposes the same validated ledger kernel to Core-owned
// workflows that must commit magic and another local projection atomically.
// The caller owns commit/rollback; no business writer is allowed to reproduce
// the posting or balance-chain SQL outside this package.
func (repository *PostgresRepository) RecordInTransaction(ctx context.Context, tx pgx.Tx, command RecordCommand) (Transaction, error) {
	if tx == nil {
		return Transaction{}, ErrInput
	}
	normalized, err := normalizeRecordCommand(command)
	if err != nil {
		return Transaction{}, err
	}
	return repository.recordInTransaction(ctx, tx, normalized)
}

func (repository *PostgresRepository) recordInTransaction(ctx context.Context, tx pgx.Tx, normalized RecordCommand) (Transaction, error) {

	// The stable advisory lock closes the race between replay lookup and the
	// unique idempotency insert without globally serializing unrelated users.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, normalized.IdempotencyKey); err != nil {
		return Transaction{}, fmt.Errorf("lock economy idempotency key: %w", err)
	}
	if replayed, found, err := readReplay(ctx, tx, normalized); found || err != nil {
		if err != nil {
			return Transaction{}, err
		}
		return replayed, nil
	}

	accountIDs := make([]uuid.UUID, len(normalized.Postings))
	for index, posting := range normalized.Postings {
		accountIDs[index] = posting.AccountID
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.magic_accounts (
    id, user_id, account_kind, account_code, balance, version, updated_at
)
SELECT
    user_account.id,
    user_account.id,
    'member',
    'member:' || user_account.id::text,
    0,
    1,
    $2
FROM identity.users AS user_account
WHERE user_account.id = ANY($1::uuid[])
ON CONFLICT (user_id) DO NOTHING`, accountIDs, normalized.RecordedAt); err != nil {
		return Transaction{}, classifyDatabaseError("ensure member magic accounts", err)
	}

	accounts, err := lockAccounts(ctx, tx, accountIDs)
	if err != nil {
		return Transaction{}, err
	}
	if len(accounts) != len(accountIDs) {
		return Transaction{}, ErrAccountNotFound
	}

	nextBalances := make(map[uuid.UUID]int64, len(accounts))
	for _, posting := range normalized.Postings {
		account := accounts[posting.AccountID]
		next, ok := addInt64(account.Balance, posting.Amount)
		if !ok {
			return Transaction{}, ErrInvariant
		}
		// A negative legacy opening is preserved, but no debit may create or
		// deepen a negative member balance. Credits can repair it over time.
		if account.Kind == "member" && posting.Amount < 0 && next < 0 {
			return Transaction{}, ErrInsufficientBalance
		}
		nextBalances[posting.AccountID] = next
	}

	var ledgerSequence int64
	if err := tx.QueryRow(ctx, `
INSERT INTO economy.magic_transactions (
    id, transaction_type, idempotency_key, source_reference,
    policy_revision, posting_count, payload_sha256, occurred_at, recorded_at
) VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9)
RETURNING ledger_sequence`,
		normalized.TransactionID, string(normalized.TransactionType), normalized.IdempotencyKey,
		normalized.SourceReference, normalized.PolicyRevision, int16(len(normalized.Postings)),
		normalized.PayloadSHA256[:], normalized.OccurredAt, normalized.RecordedAt,
	).Scan(&ledgerSequence); err != nil {
		return Transaction{}, classifyDatabaseError("insert magic transaction", err)
	}

	result := Transaction{
		ID: normalized.TransactionID, LedgerSequence: ledgerSequence,
		TransactionType: normalized.TransactionType, IdempotencyKey: normalized.IdempotencyKey,
		SourceReference: normalized.SourceReference, PolicyRevision: normalized.PolicyRevision,
		PayloadSHA256: normalized.PayloadSHA256, OccurredAt: normalized.OccurredAt,
		RecordedAt: normalized.RecordedAt, Postings: make([]Posting, 0, len(normalized.Postings)),
	}
	for index, posting := range normalized.Postings {
		balanceAfter := nextBalances[posting.AccountID]
		if _, err := tx.Exec(ctx, `
INSERT INTO economy.magic_postings (
    transaction_id, ledger_sequence, posting_index,
    account_id, amount, balance_after
) VALUES ($1, $2, $3, $4, $5, $6)`,
			normalized.TransactionID, ledgerSequence, int16(index), posting.AccountID, posting.Amount, balanceAfter,
		); err != nil {
			return Transaction{}, classifyDatabaseError("insert magic posting", err)
		}
		account := accounts[posting.AccountID]
		if account.UserID != nil {
			entryID := uuid.NewSHA1(statementNamespace, []byte(normalized.TransactionID.String()+":"+account.UserID.String()))
			entryType := "earn"
			if posting.Amount < 0 {
				entryType = "spend"
			}
			if normalized.TransactionType == TransactionAdjustment {
				entryType = "adjustment"
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO economy.magic_ledger_entries (
    id, transaction_id, user_id, entry_type, amount, balance_after,
    source_reference, occurred_at, recorded_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
				entryID, normalized.TransactionID, *account.UserID, entryType, posting.Amount,
				balanceAfter, normalized.SourceReference, normalized.OccurredAt, normalized.RecordedAt,
			); err != nil {
				return Transaction{}, classifyDatabaseError("insert member magic statement", err)
			}
		}
		if _, err := tx.Exec(ctx, `
UPDATE economy.magic_accounts
SET balance = $2, version = version + 1, updated_at = $3
WHERE id = $1`, posting.AccountID, balanceAfter, normalized.RecordedAt); err != nil {
			return Transaction{}, classifyDatabaseError("update magic account projection", err)
		}
		result.Postings = append(result.Postings, Posting{
			AccountID: posting.AccountID, Amount: posting.Amount, BalanceAfter: balanceAfter,
		})
	}
	return result, nil
}

func lockAccounts(ctx context.Context, tx pgx.Tx, accountIDs []uuid.UUID) (map[uuid.UUID]lockedAccount, error) {
	rows, err := tx.Query(ctx, `
SELECT id, user_id, account_kind, balance
FROM economy.magic_accounts
WHERE id = ANY($1::uuid[])
ORDER BY id
FOR UPDATE`, accountIDs)
	if err != nil {
		return nil, classifyDatabaseError("lock magic accounts", err)
	}
	defer rows.Close()
	accounts := make(map[uuid.UUID]lockedAccount, len(accountIDs))
	for rows.Next() {
		var account lockedAccount
		var userID pgtype.UUID
		if err := rows.Scan(&account.ID, &userID, &account.Kind, &account.Balance); err != nil {
			return nil, fmt.Errorf("scan magic account: %w", err)
		}
		if userID.Valid {
			value := uuid.UUID(userID.Bytes)
			account.UserID = &value
		}
		accounts[account.ID] = account
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish locking magic accounts: %w", err)
	}
	return accounts, nil
}

func readReplay(ctx context.Context, tx pgx.Tx, command RecordCommand) (Transaction, bool, error) {
	var result Transaction
	var transactionType string
	var policyRevision pgtype.Text
	var payload []byte
	var postingCount int16
	err := tx.QueryRow(ctx, `
SELECT
    id, ledger_sequence, transaction_type, idempotency_key,
    source_reference, policy_revision, posting_count, payload_sha256,
    occurred_at, recorded_at
FROM economy.magic_transactions
WHERE idempotency_key = $1`, command.IdempotencyKey).Scan(
		&result.ID, &result.LedgerSequence, &transactionType, &result.IdempotencyKey,
		&result.SourceReference, &policyRevision, &postingCount, &payload,
		&result.OccurredAt, &result.RecordedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Transaction{}, false, nil
	}
	if err != nil {
		return Transaction{}, true, fmt.Errorf("read economy replay: %w", err)
	}
	result.TransactionType = TransactionType(transactionType)
	if policyRevision.Valid {
		result.PolicyRevision = policyRevision.String
	}
	if len(payload) == 32 {
		copy(result.PayloadSHA256[:], payload)
	}
	if result.ID != command.TransactionID || result.TransactionType != command.TransactionType ||
		result.SourceReference != command.SourceReference || result.PolicyRevision != command.PolicyRevision ||
		int(postingCount) != len(command.Postings) || !bytes.Equal(payload, command.PayloadSHA256[:]) ||
		!result.OccurredAt.Equal(command.OccurredAt) || !result.RecordedAt.Equal(command.RecordedAt) {
		return Transaction{}, true, ErrIdempotencyConflict
	}
	rows, err := tx.Query(ctx, `
SELECT account_id, amount, balance_after
FROM economy.magic_postings
WHERE transaction_id = $1
ORDER BY posting_index`, result.ID)
	if err != nil {
		return Transaction{}, true, fmt.Errorf("read economy replay postings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var posting Posting
		if err := rows.Scan(&posting.AccountID, &posting.Amount, &posting.BalanceAfter); err != nil {
			return Transaction{}, true, fmt.Errorf("scan economy replay posting: %w", err)
		}
		result.Postings = append(result.Postings, posting)
	}
	if err := rows.Err(); err != nil {
		return Transaction{}, true, fmt.Errorf("finish economy replay postings: %w", err)
	}
	if len(result.Postings) != len(command.Postings) {
		return Transaction{}, true, ErrInvariant
	}
	for index, posting := range result.Postings {
		if posting.AccountID != command.Postings[index].AccountID || posting.Amount != command.Postings[index].Amount {
			return Transaction{}, true, ErrIdempotencyConflict
		}
	}
	result.Replayed = true
	return result, true, nil
}

func addInt64(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, false
	}
	if right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}

func classifyDatabaseError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) &&
		(postgresError.Code == "P0001" || postgresError.Code == "23514" || postgresError.Code == "23503") {
		return fmt.Errorf("%w: %s: %v", ErrInvariant, operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ Repository = (*PostgresRepository)(nil)
