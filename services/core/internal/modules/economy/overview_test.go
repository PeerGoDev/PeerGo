package economy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type economyOverviewAuthenticatorStub struct {
	session identity.WebSession
	err     error
}

func (stub economyOverviewAuthenticatorStub) CurrentSession(context.Context, string) (identity.WebSession, error) {
	return stub.session, stub.err
}

type economyOverviewAuthorizerStub struct {
	now     time.Time
	request authz.Request
}

func (stub *economyOverviewAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.request = request
	return authz.Decision{
		ID: uuid.New(), Allow: true, GrantID: uuid.New(), GrantVersion: 1,
		MandateID: uuid.New(), RoleID: "member", EffectiveUntil: stub.now.Add(time.Hour),
	}, nil
}

type economyOverviewRepositoryStub struct {
	userID uuid.UUID
	limit  int
	value  Overview
}

func (stub *economyOverviewRepositoryStub) Overview(_ context.Context, userID uuid.UUID, limit int) (Overview, error) {
	stub.userID, stub.limit = userID, limit
	return stub.value, nil
}

func TestOverviewServiceUsesAuthenticatedSubjectAndSelfCapability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 16, 2, 0, 0, 0, time.UTC)
	userID := uuid.New()
	authorizer := &economyOverviewAuthorizerStub{now: now}
	repository := &economyOverviewRepositoryStub{value: Overview{MagicBalance: 42}}
	service, err := NewOverviewService(
		economyOverviewAuthenticatorStub{session: identity.WebSession{User: identity.User{ID: userID}}},
		authorizer, repository, func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.MyOverview(context.Background(), "opaque-cookie", 30)
	if err != nil {
		t.Fatalf("MyOverview() error = %v", err)
	}
	if result.MagicBalance != 42 || repository.userID != userID || repository.limit != 30 {
		t.Fatalf("result = %+v, repository user=%s limit=%d", result, repository.userID, repository.limit)
	}
	if authorizer.request.Action != authz.ActionEconomyReadSelf ||
		authorizer.request.CredentialAudience != authz.AudienceWebSession ||
		authorizer.request.Resource.OwnerID != userID {
		t.Fatalf("authorization request = %+v", authorizer.request)
	}
}

func TestOverviewServiceRejectsLimitBeforeReadingSession(t *testing.T) {
	t.Parallel()
	service, err := NewOverviewService(
		economyOverviewAuthenticatorStub{err: errors.New("must not authenticate")},
		&economyOverviewAuthorizerStub{}, &economyOverviewRepositoryStub{}, time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.MyOverview(context.Background(), "cookie", MaximumOverviewLimit+1); !errors.Is(err, ErrInput) {
		t.Fatalf("MyOverview() error = %v, want ErrInput", err)
	}
}
