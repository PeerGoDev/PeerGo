package authz

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AuthorizeStaffAction applies the common invariants for an authenticated
// administrator before returning authorization evidence to an owning use
// case. MFA is enforced by an individual grant's typed constraint instead of
// being an unconditional requirement for the simple site_admin role.
func AuthorizeStaffAction(
	ctx context.Context,
	authorizer Authorizer,
	actor StaffActor,
	action Action,
	scope Scope,
	now time.Time,
	purpose string,
) (Decision, error) {
	if authorizer == nil || actor.Subject.ID == uuid.Nil ||
		actor.Subject.Status != SubjectActive {
		return Decision{}, ErrForbidden
	}
	decision, err := authorizer.Authorize(ctx, Request{
		Subject: actor.Subject, Action: action, CredentialAudience: AudienceStaffSession,
		Resource: Resource{Scope: scope},
		Context: EvaluationContext{
			Now: now, Purpose: purpose, MFAAuthenticatedAt: actor.MFAAuthenticatedAt,
		},
	})
	if err != nil {
		return decision, err
	}
	if !decision.Allow || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 ||
		decision.MandateID == uuid.Nil || decision.RoleID == "" || !decision.EffectiveUntil.After(now) {
		return decision, ErrForbidden
	}
	return decision, nil
}
