package identity

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
)

var (
	ErrPasswordRecoveryTokenInvalid       = errors.New("password recovery token is invalid or expired")
	ErrPasswordRecoveryServiceUnavailable = errors.New("password recovery service is unavailable")
	ErrPasswordRecoveryStateConflict      = errors.New("password recovery state is inconsistent")
)

type PasswordRecoveryDispatch struct {
	AcceptedAt    time.Time
	NextRequestAt time.Time
}

type VaultPasswordRecoveryConfirmation struct {
	RecoveryID        uuid.UUID
	CredentialRef     uuid.UUID
	PasswordChangedAt time.Time
}

type PasswordRecoveryCompletion struct {
	RecoveryID        uuid.UUID
	PasswordChangedAt time.Time
	RevokedSessions   int64
	Changed           bool
}

type PasswordRecoveryAuditInput struct {
	RecoveryID      uuid.UUID
	UserID          uuid.UUID
	RevokedSessions int64
	OccurredAt      time.Time
}

type PasswordRecoveryVault interface {
	RequestPasswordRecovery(context.Context, string) (PasswordRecoveryDispatch, error)
	ConfirmPasswordRecovery(context.Context, string, string) (VaultPasswordRecoveryConfirmation, error)
}

type PasswordRecoveryRepository interface {
	CompletePasswordRecovery(context.Context, VaultPasswordRecoveryConfirmation) (PasswordRecoveryCompletion, error)
}

type PasswordRecoveryEventBuilder interface {
	BuildPasswordRecoveredEvent(PasswordRecoveryAuditInput) (auditevent.Event, error)
}
