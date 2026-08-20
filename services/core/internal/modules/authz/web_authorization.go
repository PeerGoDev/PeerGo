package authz

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AuthorizeWebSelfAction is the shared enforcement boundary for an already
// verified ordinary Web session. Authentication establishes who owns the
// cookie; this decision still proves that the active subject may perform the
// requested self action under the current policy and records that decision.
func AuthorizeWebSelfAction(
	ctx context.Context,
	authorizer Authorizer,
	userID uuid.UUID,
	action Action,
	now time.Time,
) (Decision, error) {
	definition, known := Lookup(action)
	if authorizer == nil || userID == uuid.Nil || now.IsZero() || !known ||
		definition.Relationship != RelationshipSelf ||
		definition.CredentialAudience != AudienceWebSession {
		return Decision{}, ErrForbidden
	}
	decision, err := authorizer.Authorize(ctx, Request{
		Subject:            Subject{ID: userID, Status: SubjectActive},
		Action:             action,
		CredentialAudience: AudienceWebSession,
		Resource:           Resource{OwnerID: userID, Scope: SiteScope()},
		Context:            EvaluationContext{Now: now},
	})
	if err != nil {
		return decision, err
	}
	if !decision.Allow || decision.ID == uuid.Nil || decision.GrantID == uuid.Nil ||
		decision.GrantVersion < 1 || decision.MandateID == uuid.Nil ||
		decision.RoleID == "" || !decision.EffectiveUntil.After(now) {
		return decision, ErrForbidden
	}
	return decision, nil
}

// AuthorizeWebMemberAction proves an active member's relationship-independent
// Web permission. Member-directory reads target another account, so treating
// them as a self relationship would make the policy assertion inaccurate.
func AuthorizeWebMemberAction(
	ctx context.Context,
	authorizer Authorizer,
	userID uuid.UUID,
	action Action,
	now time.Time,
) (Decision, error) {
	definition, known := Lookup(action)
	if authorizer == nil || userID == uuid.Nil || now.IsZero() || !known ||
		definition.Relationship != RelationshipNone ||
		definition.CredentialAudience != AudienceWebSession {
		return Decision{}, ErrForbidden
	}
	decision, err := authorizer.Authorize(ctx, Request{
		Subject:            Subject{ID: userID, Status: SubjectActive},
		Action:             action,
		CredentialAudience: AudienceWebSession,
		Resource:           Resource{Scope: SiteScope()},
		Context:            EvaluationContext{Now: now},
	})
	if err != nil {
		return decision, err
	}
	if !decision.Allow || decision.ID == uuid.Nil || decision.GrantID == uuid.Nil ||
		decision.GrantVersion < 1 || decision.MandateID == uuid.Nil ||
		decision.RoleID == "" || !decision.EffectiveUntil.After(now) {
		return decision, ErrForbidden
	}
	return decision, nil
}
