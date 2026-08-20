package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/generated/identitydb"
)

// PostgresPasswordRecoveryRepository commits the Core projection, revokes all
// Web/staff sessions and appends immutable evidence in one transaction. Vault
// may already be committed, so recovery ID and timestamp checks are idempotent
// and reject an older completion racing behind a newer password.
type PostgresPasswordRecoveryRepository struct {
	pool         *pgxpool.Pool
	eventBuilder PasswordRecoveryEventBuilder
	newAppender  func(pgx.Tx) auditevent.Appender
}

func NewPostgresPasswordRecoveryRepository(pool *pgxpool.Pool, eventBuilder PasswordRecoveryEventBuilder, newAppender func(pgx.Tx) auditevent.Appender) (*PostgresPasswordRecoveryRepository, error) {
	if pool == nil || eventBuilder == nil || newAppender == nil {
		return nil, errors.New("password recovery repository dependencies are required")
	}
	return &PostgresPasswordRecoveryRepository{pool: pool, eventBuilder: eventBuilder, newAppender: newAppender}, nil
}

func (repository *PostgresPasswordRecoveryRepository) CompletePasswordRecovery(ctx context.Context, confirmation VaultPasswordRecoveryConfirmation) (PasswordRecoveryCompletion, error) {
	if confirmation.RecoveryID == uuid.Nil || confirmation.CredentialRef == uuid.Nil || confirmation.PasswordChangedAt.IsZero() {
		return PasswordRecoveryCompletion{}, ErrPasswordRecoveryStateConflict
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PasswordRecoveryCompletion{}, fmt.Errorf("begin core password recovery: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)
	user, err := queries.LockUserForPasswordRecovery(ctx, confirmation.CredentialRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return PasswordRecoveryCompletion{}, ErrPasswordRecoveryStateConflict
	}
	if err != nil {
		return PasswordRecoveryCompletion{}, fmt.Errorf("lock core user for password recovery: %w", err)
	}
	if !user.PasswordChangedAt.Valid {
		return PasswordRecoveryCompletion{}, ErrPasswordRecoveryStateConflict
	}
	if user.LastPasswordRecoveryID.Valid && uuid.UUID(user.LastPasswordRecoveryID.Bytes) == confirmation.RecoveryID {
		return PasswordRecoveryCompletion{
			RecoveryID: confirmation.RecoveryID, PasswordChangedAt: user.PasswordChangedAt.Time.UTC(), Changed: false,
		}, nil
	}
	if !confirmation.PasswordChangedAt.After(user.PasswordChangedAt.Time) {
		return PasswordRecoveryCompletion{
			RecoveryID: confirmation.RecoveryID, PasswordChangedAt: user.PasswordChangedAt.Time.UTC(), Changed: false,
		}, nil
	}
	updated, err := queries.MarkCoreUserPasswordRecovered(ctx, identitydb.MarkCoreUserPasswordRecoveredParams{
		PasswordChangedAt: timestamp(confirmation.PasswordChangedAt),
		RecoveryID:        pgtype.UUID{Bytes: confirmation.RecoveryID, Valid: true},
		UserID:            user.ID, CredentialRef: user.CredentialRef,
	})
	if err != nil {
		return PasswordRecoveryCompletion{}, fmt.Errorf("mark core user password recovered: %w", err)
	}
	if !updated.PasswordChangedAt.Valid || !updated.LastPasswordRecoveryID.Valid ||
		uuid.UUID(updated.LastPasswordRecoveryID.Bytes) != confirmation.RecoveryID {
		return PasswordRecoveryCompletion{}, ErrPasswordRecoveryStateConflict
	}
	changedAt := updated.PasswordChangedAt.Time.UTC()
	revokedSessions, err := queries.RevokeAllUserSessionsForPasswordRecovery(ctx, identitydb.RevokeAllUserSessionsForPasswordRecoveryParams{
		RevokedAt: timestamp(changedAt), UserID: updated.ID,
	})
	if err != nil {
		return PasswordRecoveryCompletion{}, fmt.Errorf("revoke sessions after password recovery: %w", err)
	}
	event, err := repository.eventBuilder.BuildPasswordRecoveredEvent(PasswordRecoveryAuditInput{
		RecoveryID: confirmation.RecoveryID, UserID: updated.ID,
		RevokedSessions: revokedSessions, OccurredAt: changedAt,
	})
	if err != nil {
		return PasswordRecoveryCompletion{}, fmt.Errorf("build password recovered audit event: %w", err)
	}
	if err := repository.newAppender(tx).Append(ctx, event); err != nil {
		return PasswordRecoveryCompletion{}, fmt.Errorf("append password recovered audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PasswordRecoveryCompletion{}, fmt.Errorf("commit core password recovery: %w", err)
	}
	return PasswordRecoveryCompletion{
		RecoveryID: confirmation.RecoveryID, PasswordChangedAt: changedAt,
		RevokedSessions: revokedSessions, Changed: true,
	}, nil
}

var _ PasswordRecoveryRepository = (*PostgresPasswordRecoveryRepository)(nil)
