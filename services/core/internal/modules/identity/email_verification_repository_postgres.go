package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/generated/identitydb"
)

// PostgresEmailVerificationRepository commits the Core identity projection and
// its immutable audit event together. Vault may already be committed, so this
// transaction is deliberately idempotent for the same credential reference.
type PostgresEmailVerificationRepository struct {
	pool         *pgxpool.Pool
	eventBuilder EmailVerificationEventBuilder
	newAppender  func(pgx.Tx) auditevent.Appender
}

func NewPostgresEmailVerificationRepository(pool *pgxpool.Pool, eventBuilder EmailVerificationEventBuilder, newAppender func(pgx.Tx) auditevent.Appender) (*PostgresEmailVerificationRepository, error) {
	if pool == nil || eventBuilder == nil || newAppender == nil {
		return nil, errors.New("email verification repository dependencies are required")
	}
	return &PostgresEmailVerificationRepository{pool: pool, eventBuilder: eventBuilder, newAppender: newAppender}, nil
}

func (repository *PostgresEmailVerificationRepository) CompleteEmailVerification(ctx context.Context, confirmation VaultEmailVerificationConfirmation) (EmailVerificationCompletion, error) {
	if confirmation.VerificationID == uuid.Nil || confirmation.CredentialRef == uuid.Nil || confirmation.VerifiedAt.IsZero() {
		return EmailVerificationCompletion{}, ErrEmailVerificationStateConflict
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return EmailVerificationCompletion{}, fmt.Errorf("begin core email verification: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)
	row, err := queries.LockUserForEmailVerification(ctx, confirmation.CredentialRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return EmailVerificationCompletion{}, ErrEmailVerificationStateConflict
	}
	if err != nil {
		return EmailVerificationCompletion{}, fmt.Errorf("lock core user for email verification: %w", err)
	}
	user := User{
		ID: row.ID, CredentialRef: row.CredentialRef,
		Username: row.Username, DisplayName: row.DisplayName,
		EmailVerifiedAt: nullableTimestamp(row.EmailVerifiedAt),
	}
	if user.EmailVerifiedAt != nil {
		return EmailVerificationCompletion{
			VerificationID: confirmation.VerificationID,
			User:           user,
			VerifiedAt:     *user.EmailVerifiedAt,
			Changed:        false,
		}, nil
	}
	updated, err := queries.MarkCoreUserEmailVerified(ctx, identitydb.MarkCoreUserEmailVerifiedParams{
		VerifiedAt: timestamp(confirmation.VerifiedAt), UserID: row.ID, CredentialRef: row.CredentialRef,
	})
	if err != nil {
		return EmailVerificationCompletion{}, fmt.Errorf("mark core user email verified: %w", err)
	}
	if !updated.EmailVerifiedAt.Valid {
		return EmailVerificationCompletion{}, ErrEmailVerificationStateConflict
	}
	verifiedAt := updated.EmailVerifiedAt.Time.UTC()
	event, err := repository.eventBuilder.BuildEmailVerifiedEvent(EmailVerificationAuditInput{
		VerificationID: confirmation.VerificationID,
		UserID:         updated.ID,
		OccurredAt:     verifiedAt,
	})
	if err != nil {
		return EmailVerificationCompletion{}, fmt.Errorf("build email verified audit event: %w", err)
	}
	if err := repository.newAppender(tx).Append(ctx, event); err != nil {
		return EmailVerificationCompletion{}, fmt.Errorf("append email verified audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return EmailVerificationCompletion{}, fmt.Errorf("commit core email verification: %w", err)
	}
	return EmailVerificationCompletion{
		VerificationID: confirmation.VerificationID,
		User: User{
			ID: updated.ID, CredentialRef: updated.CredentialRef,
			Username: updated.Username, DisplayName: updated.DisplayName,
			EmailVerifiedAt: &verifiedAt,
		},
		VerifiedAt: verifiedAt,
		Changed:    true,
	}, nil
}

var _ EmailVerificationRepository = (*PostgresEmailVerificationRepository)(nil)
