package attendance

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
	session    identity.WebSession
	readToken  string
	writeToken string
	csrfToken  string
}

func (stub *sessionStub) CurrentSession(_ context.Context, token string) (identity.WebSession, error) {
	stub.readToken = token
	return stub.session, nil
}

func (stub *sessionStub) AuthenticateWrite(_ context.Context, token, csrf string) (identity.WebSession, error) {
	stub.writeToken, stub.csrfToken = token, csrf
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
		MandateID: uuid.New(), RoleID: "member", EffectiveUntil: stub.now.Add(48 * time.Hour),
	}, nil
}

type repositoryStub struct {
	overviewCommand struct {
		userID uuid.UUID
		now    time.Time
		limit  int
	}
	claimCommand   ClaimCommand
	publishCommand PublishCommand
	latest         PublishedPolicy
}

func (stub *repositoryStub) Overview(_ context.Context, userID uuid.UUID, now time.Time, limit int) (Overview, error) {
	stub.overviewCommand.userID, stub.overviewCommand.now, stub.overviewCommand.limit = userID, now, limit
	return Overview{TotalDays: 3}, nil
}

func (stub *repositoryStub) Claim(_ context.Context, command ClaimCommand) (Record, error) {
	stub.claimCommand = command
	return Record{TotalReward: 5}, nil
}

func (stub *repositoryStub) ListPolicies(context.Context, int, int) ([]PublishedPolicy, int64, error) {
	return []PublishedPolicy{stub.latest}, 1, nil
}

func (stub *repositoryStub) PublishPolicy(_ context.Context, command PublishCommand) (PublishedPolicy, error) {
	stub.publishCommand = command
	return PublishedPolicy{Policy: command.Policy}, nil
}

func (stub *repositoryStub) LatestPolicy(context.Context) (PublishedPolicy, error) {
	return stub.latest, nil
}

func TestMemberServiceAuthenticatesAndUsesTypedSelfActions(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 16, 4, 30, 0, 0, time.UTC)
	userID := uuid.New()
	sessions := &sessionStub{session: identity.WebSession{User: identity.User{ID: userID}}}
	authorizer := &authorizerStub{now: now}
	repository := &repositoryStub{}
	service, err := NewMemberService(sessions, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.MyOverview(context.Background(), "read-cookie"); err != nil {
		t.Fatalf("MyOverview() error = %v", err)
	}
	requestID := uuid.New()
	if _, err := service.Claim(context.Background(), "write-cookie", "csrf", requestID, ModeRandom); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if sessions.readToken != "read-cookie" || sessions.writeToken != "write-cookie" || sessions.csrfToken != "csrf" {
		t.Fatalf("session calls = %+v", sessions)
	}
	if repository.overviewCommand.userID != userID || repository.overviewCommand.limit != DefaultHistoryLimit ||
		repository.claimCommand.UserID != userID || repository.claimCommand.RequestID != requestID {
		t.Fatalf("repository overview=%+v claim=%+v", repository.overviewCommand, repository.claimCommand)
	}
	if authorizer.requests[0].Action != authz.ActionEconomyAttendanceReadSelf ||
		authorizer.requests[1].Action != authz.ActionEconomyAttendanceClaimSelf {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestAdministrationIssueOwnsRevisionAndFutureLocalBoundary(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 16, 14, 30, 0, 0, time.UTC)
	latestEffective := time.Date(2026, time.August, 16, 16, 0, 0, 0, time.UTC)
	repository := &repositoryStub{latest: PublishedPolicy{Policy: PolicyRevision{
		EffectiveFrom: latestEffective, DayBoundaryTimezone: "Asia/Shanghai",
	}}}
	authorizer := &authorizerStub{now: now}
	service, err := NewAdministrationService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.New()
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	_, err = service.Issue(context.Background(), actor, requestID, PolicyRevision{
		Enabled: true, DayBoundaryTimezone: "Asia/Shanghai",
		FixedEnabled: true, FixedReward: 5, ExperienceReward: 5,
	}, "根据运营安排调整签到奖励。")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	command := repository.publishCommand
	if command.Policy.Revision != "attendance-"+strings.ReplaceAll(requestID.String(), "-", "") {
		t.Fatalf("revision = %q", command.Policy.Revision)
	}
	wantEffective := time.Date(2026, time.August, 17, 16, 0, 0, 0, time.UTC)
	if !command.Policy.EffectiveFrom.Equal(wantEffective) || command.AuthorizationDecisionID == uuid.Nil {
		t.Fatalf("publish command = %+v", command)
	}
	if authorizer.requests[0].Action != authz.ActionEconomyAttendancePolicyIssue {
		t.Fatalf("authorization request = %+v", authorizer.requests[0])
	}
}
