package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/generated/identitydb"
)

// PostgresSessionSecurityRepository commits the session mutation and its
// immutable business event in one transaction. Authorization-decision audit is
// recorded by the policy kernel first; this outbox event captures the actual
// mutation counts and cannot survive without the matching state transition.
type PostgresSessionSecurityRepository struct {
	pool         *pgxpool.Pool
	eventBuilder SessionRevocationEventBuilder
	newAppender  func(pgx.Tx) auditevent.Appender
}

func NewPostgresSessionSecurityRepository(pool *pgxpool.Pool, eventBuilder SessionRevocationEventBuilder, newAppender func(pgx.Tx) auditevent.Appender) (*PostgresSessionSecurityRepository, error) {
	if pool == nil || eventBuilder == nil || newAppender == nil {
		return nil, errors.New("session security repository dependencies are required")
	}
	return &PostgresSessionSecurityRepository{pool: pool, eventBuilder: eventBuilder, newAppender: newAppender}, nil
}

func (repository *PostgresSessionSecurityRepository) SecurityOverview(ctx context.Context, userID uuid.UUID) (AccountSecurityOverview, error) {
	row, err := identitydb.New(repository.pool).GetAccountSecurityOverview(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountSecurityOverview{}, ErrSessionNotFound
	}
	if err != nil {
		return AccountSecurityOverview{}, fmt.Errorf("get account security overview: %w", err)
	}
	if !row.PasswordChangedAt.Valid {
		return AccountSecurityOverview{}, errors.New("account security overview contains an invalid password timestamp")
	}
	return AccountSecurityOverview{
		EmailVerified:     row.EmailVerifiedAt.Valid,
		PasswordChangedAt: row.PasswordChangedAt.Time.UTC(),
	}, nil
}

func (repository *PostgresSessionSecurityRepository) ListActiveSessions(ctx context.Context, userID uuid.UUID, currentTokenHash []byte, asOf time.Time) ([]UserWebSession, error) {
	rows, err := identitydb.New(repository.pool).ListActiveUserWebSessions(ctx, identitydb.ListActiveUserWebSessionsParams{
		CurrentTokenHash: currentTokenHash, UserID: userID, AsOf: timestamp(asOf),
	})
	if err != nil {
		return nil, fmt.Errorf("list user Web sessions: %w", err)
	}
	result := make([]UserWebSession, 0, len(rows))
	for _, row := range rows {
		if row.ID == uuid.Nil || !row.CreatedAt.Valid || !row.LastSeenAt.Valid || !row.ExpiresAt.Valid {
			return nil, errors.New("user Web session contains invalid public metadata")
		}
		result = append(result, UserWebSession{
			ID: row.ID, Current: row.IsCurrent, CreatedAt: row.CreatedAt.Time.UTC(),
			LastSeenAt: row.LastSeenAt.Time.UTC(), ExpiresAt: row.ExpiresAt.Time.UTC(),
		})
	}
	return result, nil
}

func (repository *PostgresSessionSecurityRepository) ApplySessionRevocation(ctx context.Context, command SessionRevocationCommand) (SessionRevocationResult, error) {
	if err := validSessionRevocationCommand(command); err != nil {
		return SessionRevocationResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return SessionRevocationResult{}, fmt.Errorf("begin session revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)

	var result SessionRevocationResult
	switch command.Scope {
	case SessionRevocationSingle:
		row, err := queries.RevokeUserWebSessionByID(ctx, identitydb.RevokeUserWebSessionByIDParams{
			RevokedAt: timestamp(command.OccurredAt), SessionID: command.TargetSessionID,
			UserID: command.UserID, CurrentTokenHash: command.CurrentTokenHash,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return SessionRevocationResult{}, fmt.Errorf("revoke selected Web session: %w", err)
		}
		result.RevokedWebSessions = 1
		result.CurrentSessionRevoked = row.WasCurrent
		result.RevokedStaffSessions, err = queries.RevokeStaffSessionsByParent(ctx, identitydb.RevokeStaffSessionsByParentParams{
			RevokedAt: timestamp(command.OccurredAt), UserID: command.UserID, ParentTokenHash: row.TokenHash,
		})
		if err != nil {
			return SessionRevocationResult{}, fmt.Errorf("revoke selected session's staff children: %w", err)
		}
	case SessionRevocationOthers:
		result.RevokedWebSessions, err = queries.RevokeOtherUserWebSessions(ctx, identitydb.RevokeOtherUserWebSessionsParams{
			RevokedAt: timestamp(command.OccurredAt), UserID: command.UserID, CurrentTokenHash: command.CurrentTokenHash,
		})
		if err != nil {
			return SessionRevocationResult{}, fmt.Errorf("revoke other Web sessions: %w", err)
		}
		result.RevokedStaffSessions, err = queries.RevokeOtherUserStaffSessions(ctx, identitydb.RevokeOtherUserStaffSessionsParams{
			RevokedAt: timestamp(command.OccurredAt), UserID: command.UserID, CurrentTokenHash: command.CurrentTokenHash,
		})
		if err != nil {
			return SessionRevocationResult{}, fmt.Errorf("revoke other staff sessions: %w", err)
		}
	}

	event, err := repository.eventBuilder.BuildSessionRevocationEvent(SessionRevocationAuditInput{Command: command, Result: result})
	if err != nil {
		return SessionRevocationResult{}, fmt.Errorf("build session revocation audit event: %w", err)
	}
	if err := repository.newAppender(tx).Append(ctx, event); err != nil {
		return SessionRevocationResult{}, fmt.Errorf("append session revocation audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return SessionRevocationResult{}, fmt.Errorf("commit session revocation: %w", err)
	}
	return result, nil
}

func validSessionRevocationCommand(command SessionRevocationCommand) error {
	decision := command.Authorization
	if command.ID == uuid.Nil || command.UserID == uuid.Nil || len(command.CurrentTokenHash) != 32 ||
		command.OccurredAt.IsZero() || !decision.Allow || decision.ID == uuid.Nil ||
		decision.PolicyVersion == "" || decision.GrantID == uuid.Nil ||
		decision.GrantVersion < 1 || decision.MandateID == uuid.Nil || decision.RoleID == "" {
		return errors.New("session revocation command is missing required evidence")
	}
	switch command.Scope {
	case SessionRevocationSingle:
		if command.TargetSessionID == uuid.Nil {
			return errors.New("single session revocation requires a target")
		}
	case SessionRevocationOthers:
		if command.TargetSessionID != uuid.Nil {
			return errors.New("other-session revocation cannot contain a target")
		}
	default:
		return errors.New("session revocation scope is invalid")
	}
	return nil
}

var _ SessionSecurityRepository = (*PostgresSessionSecurityRepository)(nil)
