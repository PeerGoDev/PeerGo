package membergift

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type sessionStub struct {
	session identity.WebSession
}

func (stub *sessionStub) CurrentSession(context.Context, string) (identity.WebSession, error) {
	return stub.session, nil
}

func (stub *sessionStub) AuthenticateWrite(context.Context, string, string) (identity.WebSession, error) {
	return stub.session, nil
}

type authorizerStub struct {
	now      time.Time
	requests []authz.Request
}

func (stub *authorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.requests = append(stub.requests, request)
	return authz.Decision{
		ID: uuid.New(), Allow: true, GrantID: uuid.New(), GrantVersion: 1,
		MandateID: uuid.New(), RoleID: "member", EffectiveUntil: stub.now.Add(24 * time.Hour),
	}, nil
}

type repositoryStub struct {
	overviewUserID uuid.UUID
	overviewStart  time.Time
	overviewEnd    time.Time
	createCommand  CreateCommand
	publishCommand PublishCommand
}

func (stub *repositoryStub) Overview(_ context.Context, userID uuid.UUID, start, end time.Time, _ int) (Overview, error) {
	stub.overviewUserID, stub.overviewStart, stub.overviewEnd = userID, start, end
	return Overview{}, nil
}

func (stub *repositoryStub) Create(_ context.Context, command CreateCommand) (Gift, error) {
	stub.createCommand = command
	return Gift{GrossAmount: command.Amount}, nil
}

func (stub *repositoryStub) ListPolicies(context.Context, int, int) ([]PublishedPolicy, int64, error) {
	return nil, 0, nil
}

func (stub *repositoryStub) LatestPolicy(context.Context) (PublishedPolicy, error) {
	return PublishedPolicy{}, ErrPolicyNotFound
}

func (stub *repositoryStub) PublishPolicy(_ context.Context, command PublishCommand) (PublishedPolicy, error) {
	stub.publishCommand = command
	return PublishedPolicy{Policy: command.Policy}, nil
}

func TestServiceUsesNumericRecipientAndShanghaiDay(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 18, 30, 0, 0, time.UTC)
	verifiedAt := now.Add(-time.Hour)
	userID := uuid.New()
	repository := &repositoryStub{}
	authorizer := &authorizerStub{now: now}
	service, err := NewService(&sessionStub{session: identity.WebSession{User: identity.User{
		ID: userID, EmailVerifiedAt: &verifiedAt,
	}}}, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.MyOverview(context.Background(), "cookie", DefaultHistoryLimit); err != nil {
		t.Fatalf("MyOverview() error = %v", err)
	}
	requestID := uuid.New()
	if _, err := service.Create(context.Background(), "cookie", "csrf", requestID, 1234, 500, " 谢谢保种 "); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if repository.createCommand.SenderUserID != userID || repository.createCommand.RecipientNumericID != 1234 ||
		repository.createCommand.Message != "谢谢保种" || repository.createCommand.Amount != 500 {
		t.Fatalf("create command = %+v", repository.createCommand)
	}
	wantStart := time.Date(2026, time.August, 17, 16, 0, 0, 0, time.UTC)
	if !repository.createCommand.DayStartsAt.Equal(wantStart) ||
		!repository.createCommand.DayEndsAt.Equal(wantStart.Add(24*time.Hour)) {
		t.Fatalf("day = [%s, %s)", repository.createCommand.DayStartsAt, repository.createCommand.DayEndsAt)
	}
	if authorizer.requests[0].Action != authz.ActionEconomyMemberGiftReadSelf ||
		authorizer.requests[1].Action != authz.ActionEconomyMemberGiftCreateSelf {
		t.Fatalf("authorization = %+v", authorizer.requests)
	}
}

func TestServiceRejectsUnverifiedSenderBeforeLedger(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 8, 0, 0, 0, time.UTC)
	service, err := NewService(
		&sessionStub{session: identity.WebSession{User: identity.User{ID: uuid.New()}}},
		&repositoryStub{}, &authorizerStub{now: now}, func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(context.Background(), "cookie", "csrf", uuid.New(), 2, 10, ""); err != ErrSenderIneligible {
		t.Fatalf("Create() error = %v", err)
	}
}

func TestIssuePolicyOwnsRevisionAndAuthorizationEvidence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 8, 0, 0, 0, time.UTC)
	repository := &repositoryStub{}
	authorizer := &authorizerStub{now: now}
	service, err := NewService(&sessionStub{}, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.New()
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	_, err = service.IssuePolicy(context.Background(), actor, requestID, PolicyRevision{
		Enabled: true, MinimumAmount: 10, MaximumAmount: 10_000,
		DailyGrossLimit: 20_000, FeeBPS: 250,
	}, "根据站点运营安排开放成员赠送。")
	if err != nil {
		t.Fatalf("IssuePolicy() error = %v", err)
	}
	command := repository.publishCommand
	if command.Policy.Revision != "member-gift-"+strings.ReplaceAll(requestID.String(), "-", "") ||
		command.AuthorizationDecisionID == uuid.Nil || len(command.SnapshotJSON) == 0 {
		t.Fatalf("publish command = %+v", command)
	}
	if authorizer.requests[0].Action != authz.ActionEconomyMemberGiftPolicyIssue {
		t.Fatalf("authorization = %+v", authorizer.requests)
	}
}
