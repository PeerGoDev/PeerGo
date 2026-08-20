package authz

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

type decisionIDGenerator func() uuid.UUID

// Policy is a pure, default-deny evaluator. It performs no database access so
// the same rules can guard HTTP use cases, workers and CLIs.
type Policy struct {
	newDecisionID decisionIDGenerator
}

func NewPolicy(generator func() uuid.UUID) Policy {
	if generator == nil {
		generator = uuid.New
	}
	return Policy{newDecisionID: generator}
}

func (policy Policy) Evaluate(request Request, grants []Grant) Decision {
	definition, known := Lookup(request.Action)
	if !known {
		return policy.deny(ReasonActionUnknown)
	}
	if request.Subject.ID == uuid.Nil {
		return policy.deny(ReasonSubjectInvalid)
	}
	if request.Subject.Status != SubjectActive {
		return policy.deny(ReasonSubjectInactive)
	}
	if request.CredentialAudience != definition.CredentialAudience {
		return policy.deny(ReasonCredentialAudienceMismatch)
	}
	if request.Context.Now.IsZero() || !validScope(request.Resource.Scope) {
		return policy.deny(ReasonContextMissing)
	}
	requiredAuthority := request.Context.RequiredAuthority
	if !requiredAuthority.IsZero() && !requiredAuthority.IsValid() {
		return policy.deny(ReasonContextMissing)
	}
	if definition.Relationship == RelationshipSelf && request.Resource.OwnerID != request.Subject.ID {
		return policy.deny(ReasonRelationshipMismatch)
	}

	reason := ReasonGrantMissing
	if requiredAuthority.IsValid() {
		reason = ReasonAuthorityBindingMismatch
	}
	for _, grant := range grants {
		if grant.Action != request.Action {
			continue
		}
		if requiredAuthority.IsValid() && !requiredAuthority.Matches(grant.ID, grant.Version, grant.Mandate.ID) {
			continue
		}
		grantReason, effectiveUntil := evaluateGrant(request, grant)
		if grantReason == ReasonAllowed {
			return Decision{
				ID:             policy.newDecisionID(),
				Allow:          true,
				Reason:         ReasonAllowed,
				PolicyVersion:  PolicyVersion,
				GrantID:        grant.ID,
				GrantVersion:   grant.Version,
				RoleID:         grant.RoleID,
				MandateID:      grant.Mandate.ID,
				EffectiveUntil: effectiveUntil,
			}
		}
		// Once the exact bound authority row is found, its concrete failure is
		// more useful than a generic binding mismatch. An exact row is unique
		// for an action because role_permissions has a composite primary key.
		if requiredAuthority.IsValid() {
			return policy.deny(grantReason)
		}
		if reason == ReasonGrantMissing {
			reason = grantReason
		}
	}
	return policy.deny(reason)
}

func evaluateGrant(request Request, grant Grant) (ReasonCode, time.Time) {
	now := request.Context.Now
	if grant.ID == uuid.Nil || grant.Version < 1 || grant.RoleID == "" ||
		grant.SubjectID != request.Subject.ID || grant.Mandate.SubjectID != request.Subject.ID || grant.Mandate.ID == uuid.Nil {
		return ReasonGrantInvariant, time.Time{}
	}
	if grant.RevokedAt != nil {
		return ReasonGrantRevoked, time.Time{}
	}
	if now.Before(grant.ValidFrom) {
		return ReasonGrantNotStarted, time.Time{}
	}
	if !now.Before(grant.ValidUntil) {
		return ReasonGrantExpired, time.Time{}
	}
	if grant.Mandate.Status != MandateActive {
		return ReasonMandateInactive, time.Time{}
	}
	if now.Before(grant.Mandate.StartsAt) {
		return ReasonMandateNotStarted, time.Time{}
	}
	if !now.Before(grant.Mandate.EndsAt) {
		return ReasonMandateExpired, time.Time{}
	}

	// Grant scope/time must be a subset of its authority source. Failing this
	// invariant denies the request even if the requested resource happens to
	// match, preventing a malformed row from expanding a mandate.
	if grant.Scope != grant.Mandate.Scope || grant.ValidFrom.Before(grant.Mandate.StartsAt) || grant.ValidUntil.After(grant.Mandate.EndsAt) {
		return ReasonGrantInvariant, time.Time{}
	}
	if grant.Scope != request.Resource.Scope {
		return ReasonScopeMismatch, time.Time{}
	}
	if grant.Constraints.PurposeRequired && strings.TrimSpace(request.Context.Purpose) == "" {
		return ReasonContextMissing, time.Time{}
	}
	if grant.Constraints.CaseRequired && request.Context.CaseID == uuid.Nil {
		return ReasonContextMissing, time.Time{}
	}
	if grant.Constraints.MFAMaxAgeSeconds < 0 {
		return ReasonGrantInvariant, time.Time{}
	}
	if grant.Constraints.MFAMaxAgeSeconds > 0 {
		mfaAt := request.Context.MFAAuthenticatedAt
		maxAge := time.Duration(grant.Constraints.MFAMaxAgeSeconds) * time.Second
		if mfaAt.IsZero() || mfaAt.After(now) || now.Sub(mfaAt) > maxAge {
			return ReasonContextMissing, time.Time{}
		}
	}

	return ReasonAllowed, earlier(grant.ValidUntil, grant.Mandate.EndsAt)
}

func (policy Policy) deny(reason ReasonCode) Decision {
	return Decision{
		ID:            policy.newDecisionID(),
		Allow:         false,
		Reason:        reason,
		PolicyVersion: PolicyVersion,
	}
}

func validScope(scope Scope) bool {
	if scope.ID == "" || strings.Contains(scope.ID, "*") {
		return false
	}
	return scope.Type == ScopeSite || scope.Type == ScopeCategory
}

func earlier(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
