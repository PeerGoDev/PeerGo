package authz

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
)

var (
	ErrGrantAdministrationInput = errors.New("grant administration input is invalid")
	ErrGrantNotFound            = errors.New("grant was not found")
	ErrGrantAlreadyRevoked      = errors.New("grant is already revoked")
	ErrGrantVersionConflict     = errors.New("grant version changed")
	ErrGrantRevocationPending   = errors.New("grant already has a pending revocation")
	ErrGrantRevocationNotFound  = errors.New("grant revocation request was not found")
	ErrGrantRevocationClosed    = errors.New("grant revocation request is closed")
	ErrGrantReviewExists        = errors.New("grant revocation review already exists")
	ErrSeparationOfDuties       = errors.New("separation of duties requirement failed")
)

type GrantRevocationStatus string
type GrantReviewDomain string
type GrantReviewDecision string
type GrantRevocationTransition string

const (
	GrantRevocationPendingStatus    GrantRevocationStatus = "pending"
	GrantRevocationRejectedStatus   GrantRevocationStatus = "rejected"
	GrantRevocationAppliedStatus    GrantRevocationStatus = "applied"
	GrantRevocationConflictedStatus GrantRevocationStatus = "conflicted"
	GrantRevocationExpiredStatus    GrantRevocationStatus = "expired"

	GrantReviewGovernance GrantReviewDomain = "governance"
	GrantReviewSecurity   GrantReviewDomain = "security"

	GrantReviewApprove GrantReviewDecision = "approve"
	GrantReviewReject  GrantReviewDecision = "reject"

	GrantTransitionProposed           GrantRevocationTransition = "proposed"
	GrantTransitionGovernanceApproved GrantRevocationTransition = "governance_approved"
	GrantTransitionSecurityApproved   GrantRevocationTransition = "security_approved"
	GrantTransitionRejected           GrantRevocationTransition = "rejected"
	GrantTransitionApplied            GrantRevocationTransition = "applied"
	GrantTransitionConflicted         GrantRevocationTransition = "conflicted"
	GrantTransitionExpired            GrantRevocationTransition = "expired"
)

// GrantAdministrationActor remains a domain-specific alias at the public
// boundary while sharing the single verified staff actor representation.
type GrantAdministrationActor = StaffActor

type GrantAdministrationGrant struct {
	ID                 uuid.UUID
	SubjectID          uuid.UUID
	SubjectUsername    string
	SubjectDisplayName string
	RoleID             string
	RoleName           string
	MandateID          uuid.UUID
	MandateStatus      MandateStatus
	Scope              Scope
	ValidFrom          time.Time
	ValidUntil         time.Time
	Version            int64
	RevokedAt          *time.Time
}

type GrantRevocationReview struct {
	ID         uuid.UUID
	ReviewerID uuid.UUID
	Domain     GrantReviewDomain
	Decision   GrantReviewDecision
	Reason     string
	CreatedAt  time.Time
}

type GrantRevocationRequest struct {
	ID                    uuid.UUID
	GrantID               uuid.UUID
	ExpectedGrantVersion  int64
	ResultingGrantVersion int64
	TargetSubjectID       uuid.UUID
	ProposerID            uuid.UUID
	Reason                string
	Status                GrantRevocationStatus
	CreatedAt             time.Time
	ExpiresAt             time.Time
	ResolvedAt            *time.Time
	Reviews               []GrantRevocationReview
}

type GrantAdministrationOverview struct {
	PolicyVersion string
	Grants        []GrantAdministrationGrant
	Requests      []GrantRevocationRequest
}

type ProposeGrantRevocationInput struct {
	GrantID              uuid.UUID
	ExpectedGrantVersion int64
	Reason               string
}

type ReviewGrantRevocationInput struct {
	RequestID uuid.UUID
	Domain    GrantReviewDomain
	Decision  GrantReviewDecision
	Reason    string
}

type CreateGrantRevocationCommand struct {
	ID                   uuid.UUID
	GrantID              uuid.UUID
	ExpectedGrantVersion int64
	ProposerID           uuid.UUID
	Reason               string
	CreatedAt            time.Time
	ExpiresAt            time.Time
	Authorization        Decision
}

type ReviewGrantRevocationCommand struct {
	ReviewID      uuid.UUID
	RequestID     uuid.UUID
	ReviewerID    uuid.UUID
	Domain        GrantReviewDomain
	Decision      GrantReviewDecision
	Reason        string
	CreatedAt     time.Time
	Authorization Decision
}

// GrantRevocationAuditState is the canonical security-relevant projection used
// to hash before/after state. Free-form reasons are never included in it.
type GrantRevocationAuditState struct {
	Status             GrantRevocationStatus `json:"status"`
	GrantVersion       int64                 `json:"grant_version"`
	GrantRevoked       bool                  `json:"grant_revoked"`
	GovernanceDecision GrantReviewDecision   `json:"governance_decision,omitempty"`
	SecurityDecision   GrantReviewDecision   `json:"security_decision,omitempty"`
}

type GrantRevocationAuditInput struct {
	Transition            GrantRevocationTransition
	OccurredAt            time.Time
	RequestID             uuid.UUID
	GrantID               uuid.UUID
	ExpectedGrantVersion  int64
	ResultingGrantVersion int64
	ActorID               uuid.UUID
	TargetSubjectID       uuid.UUID
	Reason                string
	Authorization         Decision
	ReviewID              uuid.UUID
	ReviewDomain          GrantReviewDomain
	ReviewDecision        GrantReviewDecision
	Before                GrantRevocationAuditState
	After                 GrantRevocationAuditState
}

// GrantRevocationEventBuilder is implemented by audit. Keeping construction
// behind this interface prevents the authz transaction from knowing audit
// pseudonym keys or JSON contract details.
type GrantRevocationEventBuilder interface {
	BuildGrantRevocationEvent(GrantRevocationAuditInput) (auditevent.Event, error)
}
