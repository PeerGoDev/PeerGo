package identity

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	// ErrStaffBootstrapTargetNotFound is intentionally operator-facing only. The
	// public enrollment API never looks users up by name and therefore cannot
	// use the bootstrap flow to enumerate accounts.
	ErrStaffBootstrapTargetNotFound = errors.New("staff bootstrap target was not found")
	// ErrStaffBootstrapTicketInvalid combines unknown, expired, revoked,
	// consumed and differently-bound tickets for the browser-facing flow.
	ErrStaffBootstrapTicketInvalid = errors.New("staff bootstrap ticket is invalid")
	// ErrStaffEnrollmentChallengeNotFound combines missing, expired, replayed
	// and differently-bound registration challenges.
	ErrStaffEnrollmentChallengeNotFound = errors.New("staff enrollment challenge was not found")
	// ErrStaffEnrollmentVerification deliberately hides the WebAuthn parsing or
	// attestation step that failed.
	ErrStaffEnrollmentVerification = errors.New("staff WebAuthn enrollment verification failed")
	// ErrStaffCredentialAlreadyEnrolled prevents a ticket or authenticator from
	// producing more than one active bootstrap result.
	ErrStaffCredentialAlreadyEnrolled = errors.New("staff WebAuthn credential is already enrolled")
)

type StaffBootstrapTransition string

const (
	StaffBootstrapTicketIssued       StaffBootstrapTransition = "ticket_issued"
	StaffBootstrapCredentialEnrolled StaffBootstrapTransition = "credential_enrolled"
)

// StaffBootstrapTicket is the persistence projection. TokenHash and the
// operator reference digest are evidence only; neither raw value is returned
// by the HTTP API.
type StaffBootstrapTicket struct {
	ID                      uuid.UUID
	UserID                  uuid.UUID
	TokenHash               []byte
	OperatorReferenceSHA256 []byte
	CreatedAt               time.Time
	ExpiresAt               time.Time
	ConsumedAt              *time.Time
	RevokedAt               *time.Time
}

// IssuedStaffBootstrapTicket is returned once to the operator CLI. RawToken
// must not be logged, persisted by Core or copied into an audit event.
type IssuedStaffBootstrapTicket struct {
	ID        uuid.UUID
	Username  string
	RawToken  string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type IssueStaffBootstrapTicketInput struct {
	Username          string
	OperatorReference string
	Lifetime          time.Duration
}

type IssueStaffBootstrapTicketCommand struct {
	Ticket            StaffBootstrapTicket
	TargetUsername    string
	OperatorReference string
}

// StaffWebAuthnEnrollmentChallenge is encrypted server-side ceremony state
// tied to the ticket and the exact ordinary Web session that began it.
type StaffWebAuthnEnrollmentChallenge struct {
	ID              uuid.UUID
	TicketID        uuid.UUID
	UserID          uuid.UUID
	ParentTokenHash []byte
	Label           string
	Protected       ProtectedRecord
	CreatedAt       time.Time
	ExpiresAt       time.Time
	ConsumedAt      *time.Time
}

type BeginStaffEnrollmentInput struct {
	BootstrapToken string
	Label          string
}

type StaffEnrollmentOptions struct {
	ChallengeID uuid.UUID
	ExpiresAt   time.Time
	PublicKey   json.RawMessage
}

type CompleteStaffEnrollmentInput struct {
	BootstrapToken string
	ChallengeID    uuid.UUID
	Credential     json.RawMessage
}

type StaffCredentialEnrollment struct {
	CredentialID []byte
	Label        string
	EnrolledAt   time.Time
}

// CreateStaffCredentialEnrollmentCommand is finalized in one PostgreSQL
// transaction: validate/consume ticket, insert the encrypted credential and
// append the audit event. The raw ticket never crosses this boundary.
type CreateStaffCredentialEnrollmentCommand struct {
	TicketID         uuid.UUID
	TokenHash        []byte
	ChallengeID      uuid.UUID
	ParentTokenHash  []byte
	UserID           uuid.UUID
	CredentialID     []byte
	CredentialRecord ProtectedRecord
	Label            string
	CreatedAt        time.Time
	Authorization    authz.Decision
}

// StaffBootstrapAuditInput is the reviewed, secret-free input owned by the
// audit module. OperatorReference is present only during issuance and is
// hashed by the builder before serialization.
type StaffBootstrapAuditInput struct {
	Transition              StaffBootstrapTransition
	OccurredAt              time.Time
	TicketID                uuid.UUID
	TargetUserID            uuid.UUID
	OperatorReference       string
	OperatorReferenceSHA256 []byte
	ExpiresAt               time.Time
	ChallengeID             uuid.UUID
	CredentialID            []byte
	Label                   string
	Authorization           authz.Decision
}

type StaffBootstrapEventBuilder interface {
	BuildStaffBootstrapEvent(StaffBootstrapAuditInput) (auditevent.Event, error)
}
