package identity

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
)

// RegistrationMode is the identity-owned admission policy exposed publicly.
// It is shared by registration use cases and the site-info transport mapping.
type RegistrationMode string

const (
	RegistrationModeOpen   RegistrationMode = "open"
	RegistrationModeInvite RegistrationMode = "invite"
	RegistrationModeClosed RegistrationMode = "closed"
)

type RegistrationState string

const (
	RegistrationStateReserved              RegistrationState = "reserved"
	RegistrationStateCredentialProvisioned RegistrationState = "credential_provisioned"
	RegistrationStateCompleted             RegistrationState = "completed"
)

var (
	ErrRegistrationClosed              = errors.New("registration is closed")
	ErrRegistrationInvitationInvalid   = errors.New("registration invitation is invalid")
	ErrRegistrationUnavailable         = errors.New("registration identifiers are unavailable")
	ErrRegistrationIdempotencyConflict = errors.New("registration idempotency key was reused")
	ErrRegistrationStateConflict       = errors.New("registration state is inconsistent")
	ErrRegistrationServiceUnavailable  = errors.New("registration service is unavailable")
)

// RegistrationInput is the public, transport-independent command. Email and
// password exist only in process memory and are never passed to Core storage.
type RegistrationInput struct {
	ID              uuid.UUID
	Username        string
	DisplayName     string
	Email           string
	Password        string
	InvitationToken string
}

type RegistrationResult struct {
	UserID                    uuid.UUID
	Username                  string
	DisplayName               string
	RegistrationMode          RegistrationMode
	EmailVerificationRequired bool
	CompletedAt               time.Time
}

// RegistrationPublicPolicy exposes only constraints needed to render the
// anonymous registration form. Reserved names and configured domain lists stay
// server-side so the public endpoint cannot become an account-policy dump.
type RegistrationPublicPolicy struct {
	Mode                                     RegistrationMode
	UsernameMinCharacters                    int
	UsernameMaxCharacters                    int
	EmailDomainMode                          EmailDomainMode
	HumanVerificationProvider                HumanVerificationProvider
	HumanVerificationSiteKey                 string
	HumanVerificationRegistrationEnabled     bool
	HumanVerificationLoginEnabled            bool
	HumanVerificationPasswordRecoveryEnabled bool
}

type RegistrationRecord struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	Username      string
	DisplayName   string
	Mode          RegistrationMode
	InvitationID  *uuid.UUID
	CredentialRef *uuid.UUID
	State         RegistrationState
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CompletedAt   *time.Time
}

type PrepareRegistrationCommand struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	Username         string
	DisplayName      string
	EmailDomain      string
	InvitationDigest []byte
	OccurredAt       time.Time
}

// RegistrationAuditInput is deliberately free of usernames, display names,
// email addresses and invitation token digests.
type RegistrationAuditInput struct {
	RegistrationID uuid.UUID
	UserID         uuid.UUID
	Mode           RegistrationMode
	InvitationID   *uuid.UUID
	OccurredAt     time.Time
}

type RegistrationEventBuilder interface {
	BuildRegistrationCompletedEvent(RegistrationAuditInput) (auditevent.Event, error)
}
