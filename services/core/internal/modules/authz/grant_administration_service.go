package authz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	grantRevocationRequestLifetime = 24 * time.Hour
	minGrantChangeReasonRunes      = 10
	maxGrantChangeReasonRunes      = 1000
)

type GrantAdministrationRepository interface {
	Overview(context.Context, time.Time) (GrantAdministrationOverview, error)
	CreateRevocation(context.Context, CreateGrantRevocationCommand) (GrantRevocationRequest, error)
	ReviewRevocation(context.Context, ReviewGrantRevocationCommand) (GrantRevocationRequest, error)
}

type grantAdministrationAuthorizer interface {
	Authorize(context.Context, Request) (Decision, error)
}

type GrantAdministrationService struct {
	repository GrantAdministrationRepository
	authorizer grantAdministrationAuthorizer
	now        func() time.Time
	newID      func() uuid.UUID
}

func NewGrantAdministrationService(repository GrantAdministrationRepository, authorizer grantAdministrationAuthorizer, now func() time.Time, newID func() uuid.UUID) (*GrantAdministrationService, error) {
	if repository == nil || authorizer == nil {
		return nil, errors.New("grant administration dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	if newID == nil {
		newID = uuid.New
	}
	return &GrantAdministrationService{repository: repository, authorizer: authorizer, now: now, newID: newID}, nil
}

func (service *GrantAdministrationService) Overview(ctx context.Context, actor GrantAdministrationActor) (GrantAdministrationOverview, error) {
	now := service.now().UTC()
	if _, err := service.authorize(ctx, actor, ActionGrantRead, now, uuid.Nil); err != nil {
		return GrantAdministrationOverview{}, err
	}
	result, err := service.repository.Overview(ctx, now)
	if err != nil {
		return GrantAdministrationOverview{}, fmt.Errorf("load grant administration overview: %w", err)
	}
	result.PolicyVersion = PolicyVersion
	return result, nil
}

func (service *GrantAdministrationService) ProposeRevocation(ctx context.Context, actor GrantAdministrationActor, input ProposeGrantRevocationInput) (GrantRevocationRequest, error) {
	reason, err := validateGrantChangeReason(input.Reason)
	if err != nil || input.GrantID == uuid.Nil || input.ExpectedGrantVersion < 1 {
		return GrantRevocationRequest{}, ErrGrantAdministrationInput
	}
	requestID := service.newID()
	if requestID == uuid.Nil {
		return GrantRevocationRequest{}, errors.New("grant revocation id generator returned nil")
	}
	now := service.now().UTC()
	decision, err := service.authorize(ctx, actor, ActionGrantRevokePropose, now, requestID)
	if err != nil {
		return GrantRevocationRequest{}, err
	}
	result, err := service.repository.CreateRevocation(ctx, CreateGrantRevocationCommand{
		ID:                   requestID,
		GrantID:              input.GrantID,
		ExpectedGrantVersion: input.ExpectedGrantVersion,
		ProposerID:           actor.Subject.ID,
		Reason:               reason,
		CreatedAt:            now,
		ExpiresAt:            now.Add(grantRevocationRequestLifetime),
		Authorization:        decision,
	})
	if err != nil {
		return GrantRevocationRequest{}, fmt.Errorf("create grant revocation request: %w", err)
	}
	return result, nil
}

func (service *GrantAdministrationService) ReviewRevocation(ctx context.Context, actor GrantAdministrationActor, input ReviewGrantRevocationInput) (GrantRevocationRequest, error) {
	reason, err := validateGrantChangeReason(input.Reason)
	if err != nil || input.RequestID == uuid.Nil || !validGrantReviewDecision(input.Decision) {
		return GrantRevocationRequest{}, ErrGrantAdministrationInput
	}
	action, ok := grantReviewAction(input.Domain)
	if !ok {
		return GrantRevocationRequest{}, ErrGrantAdministrationInput
	}
	reviewID := service.newID()
	if reviewID == uuid.Nil {
		return GrantRevocationRequest{}, errors.New("grant review id generator returned nil")
	}
	now := service.now().UTC()
	decision, err := service.authorize(ctx, actor, action, now, input.RequestID)
	if err != nil {
		return GrantRevocationRequest{}, err
	}
	result, err := service.repository.ReviewRevocation(ctx, ReviewGrantRevocationCommand{
		ReviewID:      reviewID,
		RequestID:     input.RequestID,
		ReviewerID:    actor.Subject.ID,
		Domain:        input.Domain,
		Decision:      input.Decision,
		Reason:        reason,
		CreatedAt:     now,
		Authorization: decision,
	})
	if err != nil {
		return GrantRevocationRequest{}, fmt.Errorf("review grant revocation request: %w", err)
	}
	return result, nil
}

func (service *GrantAdministrationService) authorize(ctx context.Context, actor GrantAdministrationActor, action Action, now time.Time, caseID uuid.UUID) (Decision, error) {
	if actor.Subject.ID == uuid.Nil || actor.Subject.Status != SubjectActive || actor.MFAAuthenticatedAt.IsZero() {
		return Decision{}, ErrForbidden
	}
	decision, err := service.authorizer.Authorize(ctx, Request{
		Subject:            actor.Subject,
		Action:             action,
		CredentialAudience: AudienceStaffSession,
		Resource:           Resource{OwnerID: actor.Subject.ID, Scope: SiteScope()},
		Context: EvaluationContext{
			Now:                now,
			Purpose:            "grant-revocation-governance",
			CaseID:             caseID,
			MFAAuthenticatedAt: actor.MFAAuthenticatedAt,
		},
	})
	if err != nil {
		return decision, err
	}
	if !decision.Allow || decision.GrantID == uuid.Nil || decision.GrantVersion < 1 || decision.MandateID == uuid.Nil || decision.RoleID == "" || !decision.EffectiveUntil.After(now) {
		return decision, ErrForbidden
	}
	return decision, nil
}

func validateGrantChangeReason(value string) (string, error) {
	value = strings.TrimSpace(value)
	count := utf8.RuneCountInString(value)
	if !utf8.ValidString(value) || count < minGrantChangeReasonRunes || count > maxGrantChangeReasonRunes {
		return "", ErrGrantAdministrationInput
	}
	return value, nil
}

func validGrantReviewDecision(decision GrantReviewDecision) bool {
	return decision == GrantReviewApprove || decision == GrantReviewReject
}

func grantReviewAction(domain GrantReviewDomain) (Action, bool) {
	switch domain {
	case GrantReviewGovernance:
		return ActionGrantRevokeGovernance, true
	case GrantReviewSecurity:
		return ActionGrantRevokeSecurity, true
	default:
		return "", false
	}
}
