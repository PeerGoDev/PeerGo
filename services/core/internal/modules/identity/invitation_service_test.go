package identity

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type invitationAuthenticatorStub struct {
	session WebSession
}

func (stub invitationAuthenticatorStub) CurrentSession(context.Context, string) (WebSession, error) {
	return stub.session, nil
}

func (stub invitationAuthenticatorStub) AuthenticateWrite(context.Context, string, string) (WebSession, error) {
	return stub.session, nil
}

type invitationAuthorizerStub struct {
	requests []authz.Request
}

func (stub *invitationAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.requests = append(stub.requests, request)
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed,
		GrantID: uuid.New(), GrantVersion: 1, MandateID: uuid.New(),
		RoleID: "member", PolicyVersion: "test-v1",
		EffectiveUntil: request.Context.Now.Add(time.Hour),
	}, nil
}

type invitationRepositoryStub struct {
	snapshot invitationIssuerSnapshot
	items    []MemberInvitation
	total    int
	command  IssueInvitationCommand
}

func (stub *invitationRepositoryStub) Overview(context.Context, uuid.UUID, time.Time, int, int) (invitationIssuerSnapshot, []MemberInvitation, int, InvitationNetwork, error) {
	return stub.snapshot, stub.items, stub.total, InvitationNetwork{}, nil
}

func (stub *invitationRepositoryStub) Issue(_ context.Context, command IssueInvitationCommand) (MemberInvitation, error) {
	stub.command = command
	return MemberInvitation{ID: command.ID, Source: InvitationRecordMember, Status: InvitationStatusAvailable, EmailBound: true, CreatedAt: command.OccurredAt, ExpiresAt: command.OccurredAt.Add(7 * 24 * time.Hour)}, nil
}

func (stub *invitationRepositoryStub) Revoke(context.Context, RevokeInvitationCommand) (MemberInvitation, error) {
	return MemberInvitation{}, nil
}

func TestInvitationOverviewExplainsAccountAgeBlocker(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 8, 0, 0, 0, time.UTC)
	userID := uuid.New()
	repository := &invitationRepositoryStub{snapshot: invitationIssuerSnapshot{
		MemberInvitesEnabled: true, InviteValidDays: 7, MaxInvitesPerMember: 5,
		MinimumInviteAccountAgeDays: 30, MinimumInviteLevel: 2,
		Status: "active", EmailVerified: true, CreatedAt: now.Add(-10 * 24 * time.Hour),
		CurrentLevel: 3, UsedInvites: 1, RemainingInvites: 4,
	}}
	authorizer := &invitationAuthorizerStub{}
	service, err := NewInvitationService(
		invitationAuthenticatorStub{session: WebSession{User: User{ID: userID}}},
		authorizer, repository, func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewInvitationService() error = %v", err)
	}
	overview, err := service.Overview(context.Background(), "cookie", 20, 0)
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if overview.Eligibility.Eligible || overview.Eligibility.Blocker != InvitationBlockerAccountAge ||
		overview.Eligibility.CurrentAccountAgeDays != 10 || overview.Eligibility.RemainingInvites != 4 {
		t.Fatalf("eligibility = %+v", overview.Eligibility)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionInvitationReadSelf {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}

func TestInvitationIssueReturnsOneTimeTokenAndPersistsOnlyDigest(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 8, 0, 0, 0, time.UTC)
	userID := uuid.New()
	repository := &invitationRepositoryStub{}
	authorizer := &invitationAuthorizerStub{}
	service, err := NewInvitationService(
		invitationAuthenticatorStub{session: WebSession{User: User{ID: userID}}},
		authorizer, repository, func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewInvitationService() error = %v", err)
	}
	service.random = func(target []byte) (int, error) {
		for index := range target {
			target[index] = byte(index + 1)
		}
		return len(target), nil
	}
	result, err := service.Issue(context.Background(), "cookie", "csrf", "Member@Example.COM ")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if len(result.Token) != 43 || repository.command.UserID != userID || repository.command.ID == uuid.Nil {
		t.Fatalf("result=%+v command=%+v", result, repository.command)
	}
	wantDigest := sha256.Sum256([]byte(result.Token))
	if string(repository.command.TokenSHA256) != string(wantDigest[:]) || string(repository.command.TokenSHA256) == result.Token {
		t.Fatalf("repository received a non-canonical digest")
	}
	wantBinding := invitationEmailBindingHMAC(result.Token, "member@example.com")
	if string(repository.command.EmailBindingHMAC) != string(wantBinding) ||
		string(repository.command.EmailBindingHMAC) == "member@example.com" || !result.Invitation.EmailBound {
		t.Fatalf("repository received a non-canonical email binding")
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].Action != authz.ActionInvitationIssueSelf {
		t.Fatalf("authorization requests = %+v", authorizer.requests)
	}
}
