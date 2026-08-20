package identity

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
)

var (
	ErrEmailVerificationCooldown            = errors.New("email verification request is cooling down")
	ErrEmailVerificationTokenInvalid        = errors.New("email verification token is invalid or expired")
	ErrEmailVerificationDeliveryUnavailable = errors.New("email verification delivery is unavailable")
	ErrEmailVerificationServiceUnavailable  = errors.New("email verification service is unavailable")
	ErrEmailVerificationStateConflict       = errors.New("email verification state is inconsistent")
)

type EmailVerificationCooldownError struct {
	NextRequestAt time.Time
}

func (err *EmailVerificationCooldownError) Error() string {
	return ErrEmailVerificationCooldown.Error()
}
func (err *EmailVerificationCooldownError) Unwrap() error { return ErrEmailVerificationCooldown }

type EmailVerificationDispatch struct {
	AcceptedAt      time.Time
	NextRequestAt   time.Time
	AlreadyVerified bool
}

type VaultEmailVerificationDispatch struct {
	VerificationID  uuid.UUID
	AcceptedAt      time.Time
	NextRequestAt   time.Time
	AlreadyVerified bool
	VerifiedAt      *time.Time
}

type VaultEmailVerificationConfirmation struct {
	VerificationID uuid.UUID
	CredentialRef  uuid.UUID
	VerifiedAt     time.Time
}

type EmailVerificationCompletion struct {
	VerificationID uuid.UUID
	User           User
	VerifiedAt     time.Time
	Changed        bool
}

type EmailVerificationAuditInput struct {
	VerificationID uuid.UUID
	UserID         uuid.UUID
	OccurredAt     time.Time
}

type EmailVerificationVault interface {
	RequestEmailVerification(context.Context, uuid.UUID, string) (VaultEmailVerificationDispatch, error)
	ConfirmEmailVerification(context.Context, string) (VaultEmailVerificationConfirmation, error)
}

type EmailVerificationSessionAuthenticator interface {
	AuthenticateWrite(context.Context, string, string) (WebSession, error)
}

type EmailVerificationRepository interface {
	CompleteEmailVerification(context.Context, VaultEmailVerificationConfirmation) (EmailVerificationCompletion, error)
}

type EmailVerificationEventBuilder interface {
	BuildEmailVerifiedEvent(EmailVerificationAuditInput) (auditevent.Event, error)
}
