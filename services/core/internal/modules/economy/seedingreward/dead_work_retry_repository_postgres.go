package seedingreward

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresDeadWorkRetryRepository is the only write path that can return a
// terminal seeding-reward item to the worker. The mutation and its immutable
// audit outbox event commit in the same serializable transaction.
type PostgresDeadWorkRetryRepository struct {
	pool         *pgxpool.Pool
	eventBuilder DeadWorkRetryEventBuilder
	newAppender  TransactionEventAppenderFactory
}

func NewPostgresDeadWorkRetryRepository(
	pool *pgxpool.Pool,
	eventBuilder DeadWorkRetryEventBuilder,
	newAppender TransactionEventAppenderFactory,
) (*PostgresDeadWorkRetryRepository, error) {
	if pool == nil || eventBuilder == nil || newAppender == nil {
		return nil, ErrInput
	}
	return &PostgresDeadWorkRetryRepository{
		pool: pool, eventBuilder: eventBuilder, newAppender: newAppender,
	}, nil
}

func (repository *PostgresDeadWorkRetryRepository) RequeueDead(
	ctx context.Context,
	command DeadWorkRetryCommand,
) (DeadWorkRetryResult, error) {
	command.WindowStart = canonicalTime(command.WindowStart)
	command.OccurredAt = canonicalTime(command.OccurredAt)
	command.ExpectedErrorCode = strings.TrimSpace(command.ExpectedErrorCode)
	command.OperatorReference = strings.TrimSpace(command.OperatorReference)
	command.Reason = strings.TrimSpace(command.Reason)
	if !validDeadWorkRetryCommand(command) {
		return DeadWorkRetryResult{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return DeadWorkRetryResult{}, fmt.Errorf("begin seeding reward dead-work retry: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status, lastErrorCode string
	var attempts int32
	var calculationExists bool
	err = tx.QueryRow(ctx, `
SELECT work.status, work.attempts, COALESCE(work.last_error_code, ''),
       calculation.user_id IS NOT NULL
FROM economy.seeding_reward_work_items AS work
LEFT JOIN economy.seeding_reward_calculations AS calculation
  ON calculation.window_start = work.window_start
 AND calculation.user_id = work.user_id
WHERE work.window_start = $1 AND work.user_id = $2
FOR UPDATE OF work`, command.WindowStart, command.UserID).
		Scan(&status, &attempts, &lastErrorCode, &calculationExists)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeadWorkRetryResult{}, ErrDeadWorkNotFound
	}
	if err != nil {
		return DeadWorkRetryResult{}, fmt.Errorf("lock seeding reward dead work: %w", err)
	}
	if status != "dead" || calculationExists || attempts != command.ExpectedAttempts ||
		lastErrorCode != command.ExpectedErrorCode {
		return DeadWorkRetryResult{}, ErrDeadWorkConflict
	}

	result := DeadWorkRetryResult{
		RetryID: command.ID, WindowStart: command.WindowStart, UserID: command.UserID,
		PreviousAttempts: attempts, PreviousErrorCode: lastErrorCode,
		RequeuedAt: command.OccurredAt,
	}
	commandTag, err := tx.Exec(ctx, `
UPDATE economy.seeding_reward_work_items
SET status = 'pending', attempts = 0, available_at = $3,
    lease_token = NULL, lease_until = NULL,
    last_error_code = NULL, last_error_at = NULL,
    completed_at = NULL, updated_at = $3
WHERE window_start = $1 AND user_id = $2
  AND status = 'dead' AND attempts = $4 AND last_error_code = $5`,
		command.WindowStart, command.UserID, command.OccurredAt,
		command.ExpectedAttempts, command.ExpectedErrorCode)
	if err != nil {
		return DeadWorkRetryResult{}, fmt.Errorf("requeue seeding reward dead work: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return DeadWorkRetryResult{}, ErrDeadWorkConflict
	}
	event, err := repository.eventBuilder.BuildSeedingRewardRetryEvent(DeadWorkRetryAuditInput{
		Command: command, Result: result,
	})
	if err != nil {
		return DeadWorkRetryResult{}, fmt.Errorf("build seeding reward retry audit event: %w", err)
	}
	if err := repository.newAppender(tx).Append(ctx, event); err != nil {
		return DeadWorkRetryResult{}, fmt.Errorf("append seeding reward retry audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return DeadWorkRetryResult{}, fmt.Errorf("commit seeding reward dead-work retry: %w", err)
	}
	return result, nil
}
