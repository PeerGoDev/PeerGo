package authz

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type memoryGrantAdministrationRepository struct {
	overview        GrantAdministrationOverview
	createdCommand  CreateGrantRevocationCommand
	reviewedCommand ReviewGrantRevocationCommand
	result          GrantRevocationRequest
	err             error
}

func (repository *memoryGrantAdministrationRepository) Overview(context.Context, time.Time) (GrantAdministrationOverview, error) {
	return repository.overview, repository.err
}

func (repository *memoryGrantAdministrationRepository) CreateRevocation(_ context.Context, command CreateGrantRevocationCommand) (GrantRevocationRequest, error) {
	repository.createdCommand = command
	return repository.result, repository.err
}

func (repository *memoryGrantAdministrationRepository) ReviewRevocation(_ context.Context, command ReviewGrantRevocationCommand) (GrantRevocationRequest, error) {
	repository.reviewedCommand = command
	return repository.result, repository.err
}

type recordingGrantAdministrationAuthorizer struct {
	requests []Request
	decision Decision
	err      error
}

func (authorizer *recordingGrantAdministrationAuthorizer) Authorize(_ context.Context, request Request) (Decision, error) {
	authorizer.requests = append(authorizer.requests, request)
	return authorizer.decision, authorizer.err
}

func TestGrantAdministrationUsesSeparateStaffBusinessAuthority(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	actorID := uuid.New()
	requestID := uuid.New()
	decision := allowedGrantAdministrationDecision(now)
	authorizer := &recordingGrantAdministrationAuthorizer{decision: decision}
	repository := &memoryGrantAdministrationRepository{overview: GrantAdministrationOverview{}}
	service, err := NewGrantAdministrationService(repository, authorizer, func() time.Time { return now }, func() uuid.UUID { return requestID })
	if err != nil {
		t.Fatalf("NewGrantAdministrationService() error = %v", err)
	}

	result, err := service.Overview(context.Background(), GrantAdministrationActor{
		Subject:            Subject{ID: actorID, Status: SubjectActive},
		MFAAuthenticatedAt: now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if result.PolicyVersion != PolicyVersion || len(authorizer.requests) != 1 {
		t.Fatalf("result=%+v requests=%+v", result, authorizer.requests)
	}
	request := authorizer.requests[0]
	if request.Action != ActionGrantRead || request.CredentialAudience != AudienceStaffSession || !request.Context.RequiredAuthority.IsZero() {
		t.Fatalf("authorization request = %+v", request)
	}
	if request.Context.MFAAuthenticatedAt != now.Add(-time.Minute) {
		t.Fatalf("MFA time = %s", request.Context.MFAAuthenticatedAt)
	}
}

func TestGrantRevocationProposalValidatesAndCarriesDecisionEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	actorID := uuid.New()
	requestID := uuid.New()
	grantID := uuid.New()
	decision := allowedGrantAdministrationDecision(now)
	authorizer := &recordingGrantAdministrationAuthorizer{decision: decision}
	repository := &memoryGrantAdministrationRepository{result: GrantRevocationRequest{ID: requestID}}
	service, err := NewGrantAdministrationService(repository, authorizer, func() time.Time { return now }, func() uuid.UUID { return requestID })
	if err != nil {
		t.Fatalf("NewGrantAdministrationService() error = %v", err)
	}
	actor := GrantAdministrationActor{Subject: Subject{ID: actorID, Status: SubjectActive}, MFAAuthenticatedAt: now}

	if _, err := service.ProposeRevocation(context.Background(), actor, ProposeGrantRevocationInput{GrantID: grantID, ExpectedGrantVersion: 3, Reason: "short"}); !errors.Is(err, ErrGrantAdministrationInput) {
		t.Fatalf("short proposal error = %v", err)
	}
	if len(authorizer.requests) != 0 {
		t.Fatalf("authorization called for invalid input: %+v", authorizer.requests)
	}

	_, err = service.ProposeRevocation(context.Background(), actor, ProposeGrantRevocationInput{
		GrantID: grantID, ExpectedGrantVersion: 3, Reason: "  任期已经结束，需要按治理规则收回权限。  ",
	})
	if err != nil {
		t.Fatalf("ProposeRevocation() error = %v", err)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != ActionGrantRevokePropose || authorizer.requests[0].Context.CaseID != requestID {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
	command := repository.createdCommand
	if command.ID != requestID || command.ProposerID != actorID || command.Authorization != decision || command.Reason != "任期已经结束，需要按治理规则收回权限。" || command.ExpiresAt.Sub(command.CreatedAt) != grantRevocationRequestLifetime {
		t.Fatalf("created command = %+v", command)
	}
}

func TestGrantRevocationReviewMapsDutyDomainToTypedAction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	actorID := uuid.New()
	requestID := uuid.New()
	reviewID := uuid.New()
	authorizer := &recordingGrantAdministrationAuthorizer{decision: allowedGrantAdministrationDecision(now)}
	repository := &memoryGrantAdministrationRepository{result: GrantRevocationRequest{ID: requestID}}
	service, err := NewGrantAdministrationService(repository, authorizer, func() time.Time { return now }, func() uuid.UUID { return reviewID })
	if err != nil {
		t.Fatalf("NewGrantAdministrationService() error = %v", err)
	}
	actor := GrantAdministrationActor{Subject: Subject{ID: actorID, Status: SubjectActive}, MFAAuthenticatedAt: now}

	_, err = service.ReviewRevocation(context.Background(), actor, ReviewGrantRevocationInput{
		RequestID: requestID,
		Domain:    GrantReviewSecurity,
		Decision:  GrantReviewApprove,
		Reason:    "安全职责确认该授权应当按申请立即撤销。",
	})
	if err != nil {
		t.Fatalf("ReviewRevocation() error = %v", err)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != ActionGrantRevokeSecurity || authorizer.requests[0].Context.CaseID != requestID {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
	command := repository.reviewedCommand
	if command.ReviewID != reviewID || command.RequestID != requestID || command.ReviewerID != actorID || command.Domain != GrantReviewSecurity || command.Decision != GrantReviewApprove {
		t.Fatalf("review command = %+v", command)
	}
}

func allowedGrantAdministrationDecision(now time.Time) Decision {
	return Decision{
		ID:             uuid.New(),
		Allow:          true,
		Reason:         ReasonAllowed,
		PolicyVersion:  PolicyVersion,
		GrantID:        uuid.New(),
		GrantVersion:   2,
		RoleID:         "grant_proposer",
		MandateID:      uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}
}
