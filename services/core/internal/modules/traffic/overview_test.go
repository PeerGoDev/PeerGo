package traffic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type overviewAuthenticatorFixture struct {
	session identity.WebSession
	err     error
}

func (fixture overviewAuthenticatorFixture) CurrentSession(context.Context, string) (identity.WebSession, error) {
	return fixture.session, fixture.err
}

func (fixture overviewAuthenticatorFixture) AuthenticateWrite(context.Context, string, string) (identity.WebSession, error) {
	return fixture.session, fixture.err
}

type overviewAuthorizerFixture struct {
	request authz.Request
	now     time.Time
}

func (fixture *overviewAuthorizerFixture) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	fixture.request = request
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed,
		GrantID: uuid.New(), GrantVersion: 1, MandateID: uuid.New(), RoleID: "member",
		EffectiveUntil: fixture.now.Add(time.Hour),
	}, nil
}

type overviewRepositoryFixture struct {
	userID   uuid.UUID
	limit    int
	overview Overview
	err      error
}

func (fixture *overviewRepositoryFixture) Overview(_ context.Context, userID uuid.UUID, limit int) (Overview, error) {
	fixture.userID, fixture.limit = userID, limit
	return fixture.overview, fixture.err
}

func (fixture *overviewRepositoryFixture) ListHNR(context.Context, uuid.UUID, HNRQuery) (HNRPage, error) {
	return HNRPage{}, nil
}

func (fixture *overviewRepositoryFixture) SubmitHNRAppeal(context.Context, SubmitHNRAppealCommand) (HNRAppeal, error) {
	return HNRAppeal{}, nil
}

func (fixture *overviewRepositoryFixture) HNRAppeals(context.Context, HNRAppealQuery) (HNRAppealPage, error) {
	return HNRAppealPage{}, nil
}

func (fixture *overviewRepositoryFixture) DecideHNRAppeal(context.Context, DecideHNRAppealCommand) (HNRAppeal, error) {
	return HNRAppeal{}, nil
}

func TestMyOverviewUsesVerifiedSessionSubjectAndTypedSelfAuthorization(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	want := Overview{Totals: Totals{CreditedUploaded: 2048, EntryCount: 1}}
	authorizer := &overviewAuthorizerFixture{now: now}
	repository := &overviewRepositoryFixture{overview: want}
	service, err := NewService(
		overviewAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: userID}}},
		authorizer,
		repository,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.MyOverview(context.Background(), "opaque-cookie", 20)
	if err != nil {
		t.Fatalf("MyOverview() error = %v", err)
	}
	if result.Totals.CreditedUploaded != want.Totals.CreditedUploaded || repository.userID != userID || repository.limit != 20 {
		t.Fatalf("MyOverview() = %+v, repository user=%s limit=%d", result, repository.userID, repository.limit)
	}
	if authorizer.request.Action != authz.ActionTrafficReadSelf || authorizer.request.Resource.OwnerID != userID ||
		authorizer.request.CredentialAudience != authz.AudienceWebSession {
		t.Fatalf("authorization request = %+v", authorizer.request)
	}
}

func TestMyOverviewRejectsInvalidLimitBeforeAuthentication(t *testing.T) {
	t.Parallel()
	service, err := NewService(
		overviewAuthenticatorFixture{err: errors.New("must not authenticate")},
		&overviewAuthorizerFixture{},
		&overviewRepositoryFixture{},
		time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.MyOverview(context.Background(), "cookie", MaximumOverviewLimit+1); !errors.Is(err, ErrInput) {
		t.Fatalf("MyOverview() error = %v, want ErrInput", err)
	}
}
