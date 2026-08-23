// Package authz owns PeerGo's typed authorization catalog and default-deny
// policy kernel. Positions remain display/governance concepts and never enter
// authorization decisions directly.
package authz

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const PolicyVersion = "2026-08-23.1"

var ErrForbidden = errors.New("authorization denied")

type Action string
type RiskLevel string
type Relationship string
type CredentialAudience string
type ScopeType string
type SubjectStatus string
type MandateStatus string
type ReasonCode string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"

	RelationshipNone Relationship = "none"
	RelationshipSelf Relationship = "self"

	AudienceAnonymous    CredentialAudience = "anonymous"
	AudienceWebSession   CredentialAudience = "web-session"
	AudienceStaffSession CredentialAudience = "staff-session"
	AudienceService      CredentialAudience = "service"
	AudienceRSSToken     CredentialAudience = "rss-token"

	ScopeSite     ScopeType = "site"
	ScopeCategory ScopeType = "category"

	SubjectActive   SubjectStatus = "active"
	SubjectDisabled SubjectStatus = "disabled"
	SubjectFrozen   SubjectStatus = "frozen"

	MandateActive    MandateStatus = "active"
	MandateSuspended MandateStatus = "suspended"
	MandateExpired   MandateStatus = "expired"
	MandateRevoked   MandateStatus = "revoked"

	ReasonAllowed                    ReasonCode = "allowed"
	ReasonActionUnknown              ReasonCode = "action_unknown"
	ReasonSubjectInvalid             ReasonCode = "subject_invalid"
	ReasonSubjectInactive            ReasonCode = "subject_inactive"
	ReasonCredentialAudienceMismatch ReasonCode = "credential_audience_mismatch"
	ReasonRelationshipMismatch       ReasonCode = "relationship_mismatch"
	ReasonAuthorityBindingMismatch   ReasonCode = "authority_binding_mismatch"
	ReasonGrantMissing               ReasonCode = "grant_missing"
	ReasonGrantRevoked               ReasonCode = "grant_revoked"
	ReasonGrantNotStarted            ReasonCode = "grant_not_started"
	ReasonGrantExpired               ReasonCode = "grant_expired"
	ReasonGrantInvariant             ReasonCode = "grant_invariant_failed"
	ReasonMandateInactive            ReasonCode = "mandate_inactive"
	ReasonMandateNotStarted          ReasonCode = "mandate_not_started"
	ReasonMandateExpired             ReasonCode = "mandate_expired"
	ReasonScopeMismatch              ReasonCode = "scope_mismatch"
	ReasonContextMissing             ReasonCode = "context_missing"
)

// PermissionDefinition is the code-reviewed metadata for one stable action.
// Grantable actions may appear in roles; discoverable actions may be returned
// by the self capabilities endpoint.
type PermissionDefinition struct {
	Action             Action
	Description        string
	Risk               RiskLevel
	Relationship       Relationship
	CredentialAudience CredentialAudience
	Grantable          bool
	Discoverable       bool
}

// PersistedPermission is the database projection checked against the typed
// catalog during startup. It is intentionally not reused as an API DTO.
type PersistedPermission struct {
	Action             Action
	Description        string
	Risk               RiskLevel
	Relationship       Relationship
	CredentialAudience CredentialAudience
	Grantable          bool
	Discoverable       bool
}

type Subject struct {
	ID     uuid.UUID
	Status SubjectStatus
}

// StaffActor contains administrator identity evidence verified by either the
// account session or the optional passkey session. MFAAuthenticatedAt remains
// zero for the simple account-session path, so grants that explicitly require
// recent MFA still fail unless the optional elevation was completed.
type StaffActor struct {
	Subject            Subject
	MFAAuthenticatedAt time.Time
}

// Authorizer is the shared use-case boundary for typed authorization. Domain
// modules depend on this narrow port instead of restating the same interface.
type Authorizer interface {
	Authorize(context.Context, Request) (Decision, error)
}

type Scope struct {
	Type ScopeType
	ID   string
}

type Resource struct {
	OwnerID uuid.UUID
	Scope   Scope
}

// Constraints are deliberately small and typed. Unknown JSON fields fail
// repository decoding so a newer policy cannot be silently ignored by an
// older binary.
type Constraints struct {
	PurposeRequired  bool  `json:"purpose_required,omitempty"`
	CaseRequired     bool  `json:"case_required,omitempty"`
	MFAMaxAgeSeconds int64 `json:"mfa_max_age_seconds,omitempty"`
}

type Mandate struct {
	ID        uuid.UUID
	SubjectID uuid.UUID
	Scope     Scope
	StartsAt  time.Time
	EndsAt    time.Time
	Status    MandateStatus
}

type Grant struct {
	ID          uuid.UUID
	SubjectID   uuid.UUID
	RoleID      string
	Action      Action
	Scope       Scope
	ValidFrom   time.Time
	ValidUntil  time.Time
	Constraints Constraints
	Version     int64
	RevokedAt   *time.Time
	Mandate     Mandate
}

// AuthorityBinding is an immutable snapshot of the authority source that
// created a privileged credential. Requiring the same grant row, version and
// mandate on later requests prevents a replacement grant from silently
// inheriting an already-issued staff session.
type AuthorityBinding struct {
	GrantID      uuid.UUID
	GrantVersion int64
	MandateID    uuid.UUID
}

func (binding AuthorityBinding) IsZero() bool {
	return binding.GrantID == uuid.Nil && binding.GrantVersion == 0 && binding.MandateID == uuid.Nil
}

func (binding AuthorityBinding) IsValid() bool {
	return binding.GrantID != uuid.Nil && binding.GrantVersion > 0 && binding.MandateID != uuid.Nil
}

func (binding AuthorityBinding) Matches(grantID uuid.UUID, grantVersion int64, mandateID uuid.UUID) bool {
	return binding.IsValid() && binding.GrantID == grantID && binding.GrantVersion == grantVersion && binding.MandateID == mandateID
}

type EvaluationContext struct {
	Now                time.Time
	Purpose            string
	CaseID             uuid.UUID
	MFAAuthenticatedAt time.Time
	RequiredAuthority  AuthorityBinding
}

type Request struct {
	Subject            Subject
	Action             Action
	CredentialAudience CredentialAudience
	Resource           Resource
	Context            EvaluationContext
}

// Decision carries a stable reason and unique identifier even for denials.
// Authority-source fields are populated only for an allowed decision; a denied
// request must not make an ineffective or malformed grant appear authoritative.
type Decision struct {
	ID             uuid.UUID
	Allow          bool
	Reason         ReasonCode
	PolicyVersion  string
	GrantID        uuid.UUID
	GrantVersion   int64
	RoleID         string
	MandateID      uuid.UUID
	EffectiveUntil time.Time
}

func (decision Decision) AuthorityBinding() AuthorityBinding {
	return AuthorityBinding{
		GrantID:      decision.GrantID,
		GrantVersion: decision.GrantVersion,
		MandateID:    decision.MandateID,
	}
}

type Capability struct {
	Action      Action
	Description string
	Scope       Scope
	ExpiresAt   time.Time
}

type CapabilitySet struct {
	PolicyVersion string
	Items         []Capability
}

type DeniedError struct {
	Decision Decision
}

func (err DeniedError) Error() string {
	return "authorization denied: " + string(err.Decision.Reason)
}

func (DeniedError) Unwrap() error {
	return ErrForbidden
}
