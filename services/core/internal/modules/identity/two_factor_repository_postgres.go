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

// PostgresTwoFactorChangeRepository closes the Vault-first saga by recording a
// redacted idempotency projection, revoking other sessions when appropriate and
// appending the business audit event in one Core transaction.
type PostgresTwoFactorChangeRepository struct {
	pool         *pgxpool.Pool
	eventBuilder TwoFactorChangeEventBuilder
	newAppender  func(pgx.Tx) auditevent.Appender
}

func NewPostgresTwoFactorChangeRepository(pool *pgxpool.Pool, eventBuilder TwoFactorChangeEventBuilder, newAppender func(pgx.Tx) auditevent.Appender) (*PostgresTwoFactorChangeRepository, error) {
	if pool == nil || eventBuilder == nil || newAppender == nil {
		return nil, errors.New("two-factor change repository dependencies are required")
	}
	return &PostgresTwoFactorChangeRepository{pool: pool, eventBuilder: eventBuilder, newAppender: newAppender}, nil
}

func (repository *PostgresTwoFactorChangeRepository) ApplyTwoFactorChange(ctx context.Context, command TwoFactorChangeCommand) (TwoFactorChangeResult, error) {
	if err := validTwoFactorChangeCommand(command); err != nil {
		return TwoFactorChangeResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TwoFactorChangeResult{}, fmt.Errorf("begin Core two-factor change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)
	_, err = queries.ReserveTwoFactorChange(ctx, identitydb.ReserveTwoFactorChangeParams{
		ID: command.ID, UserID: command.UserID, Kind: string(command.Kind),
		OccurredAt: timestamp(command.OccurredAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := queries.GetTwoFactorChangeForUpdate(ctx, command.ID)
		if loadErr != nil {
			return TwoFactorChangeResult{}, fmt.Errorf("load existing two-factor change: %w", loadErr)
		}
		if existing.UserID != command.UserID || existing.Kind != string(command.Kind) {
			return TwoFactorChangeResult{}, errors.New("two-factor change id was reused for another command")
		}
		return TwoFactorChangeResult{
			RevokedWebSessions:   existing.RevokedWebSessions,
			RevokedStaffSessions: existing.RevokedStaffSessions,
		}, nil
	}
	if err != nil {
		return TwoFactorChangeResult{}, fmt.Errorf("reserve Core two-factor change: %w", err)
	}

	var result TwoFactorChangeResult
	if command.Kind == TwoFactorEnabled || command.Kind == TwoFactorDisabled {
		result.RevokedWebSessions, err = queries.RevokeOtherUserWebSessions(ctx, identitydb.RevokeOtherUserWebSessionsParams{
			RevokedAt: timestamp(command.OccurredAt), UserID: command.UserID,
			CurrentTokenHash: command.CurrentTokenHash,
		})
		if err != nil {
			return TwoFactorChangeResult{}, fmt.Errorf("revoke other Web sessions after two-factor change: %w", err)
		}
		result.RevokedStaffSessions, err = queries.RevokeOtherUserStaffSessions(ctx, identitydb.RevokeOtherUserStaffSessionsParams{
			RevokedAt: timestamp(command.OccurredAt), UserID: command.UserID,
			CurrentTokenHash: command.CurrentTokenHash,
		})
		if err != nil {
			return TwoFactorChangeResult{}, fmt.Errorf("revoke other staff sessions after two-factor change: %w", err)
		}
	}
	completed, err := queries.CompleteTwoFactorChange(ctx, identitydb.CompleteTwoFactorChangeParams{
		RevokedWebSessions:   result.RevokedWebSessions,
		RevokedStaffSessions: result.RevokedStaffSessions,
		ID:                   command.ID, UserID: command.UserID, Kind: string(command.Kind),
	})
	if err != nil {
		return TwoFactorChangeResult{}, fmt.Errorf("complete Core two-factor change: %w", err)
	}
	result.RevokedWebSessions = completed.RevokedWebSessions
	result.RevokedStaffSessions = completed.RevokedStaffSessions
	event, err := repository.eventBuilder.BuildTwoFactorChangeEvent(TwoFactorChangeAuditInput{Command: command, Result: result})
	if err != nil {
		return TwoFactorChangeResult{}, fmt.Errorf("build two-factor change audit event: %w", err)
	}
	if err := repository.newAppender(tx).Append(ctx, event); err != nil {
		return TwoFactorChangeResult{}, fmt.Errorf("append two-factor change audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return TwoFactorChangeResult{}, fmt.Errorf("commit Core two-factor change: %w", err)
	}
	return result, nil
}

func validTwoFactorChangeCommand(command TwoFactorChangeCommand) error {
	decision := command.Authorization
	if command.ID == uuid.Nil || command.UserID == uuid.Nil || len(command.CurrentTokenHash) != 32 ||
		command.OccurredAt.IsZero() || !decision.Allow || decision.ID == uuid.Nil ||
		decision.PolicyVersion == "" || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 ||
		decision.MandateID == uuid.Nil || decision.RoleID == "" {
		return errors.New("two-factor change command is missing required evidence")
	}
	if command.Kind != TwoFactorEnabled && command.Kind != TwoFactorRecoveryCodesRotated && command.Kind != TwoFactorDisabled {
		return errors.New("two-factor change kind is invalid")
	}
	return nil
}

var _ TwoFactorChangeRepository = (*PostgresTwoFactorChangeRepository)(nil)
