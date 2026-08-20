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

type hnrReadRepositoryFixture struct {
	userID uuid.UUID
	query  HNRQuery
	page   HNRPage
}

func (fixture *hnrReadRepositoryFixture) Overview(context.Context, uuid.UUID, int) (Overview, error) {
	return Overview{}, nil
}

func (fixture *hnrReadRepositoryFixture) ListHNR(_ context.Context, userID uuid.UUID, query HNRQuery) (HNRPage, error) {
	fixture.userID, fixture.query = userID, query
	return fixture.page, nil
}

func (fixture *hnrReadRepositoryFixture) SubmitHNRAppeal(context.Context, SubmitHNRAppealCommand) (HNRAppeal, error) {
	return HNRAppeal{}, nil
}

func (fixture *hnrReadRepositoryFixture) HNRAppeals(context.Context, HNRAppealQuery) (HNRAppealPage, error) {
	return HNRAppealPage{}, nil
}

func (fixture *hnrReadRepositoryFixture) DecideHNRAppeal(context.Context, DecideHNRAppealCommand) (HNRAppeal, error) {
	return HNRAppeal{}, nil
}

func TestMyHNRUsesVerifiedSubjectAndHNRCapability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	authorizer := &overviewAuthorizerFixture{now: now}
	repository := &hnrReadRepositoryFixture{page: HNRPage{AsOf: now, Summary: HNRSummary{Total: 1}}}
	service, err := NewService(
		overviewAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: userID}}},
		authorizer, repository, func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.MyHNR(context.Background(), "opaque-cookie", HNRQuery{Filter: HNRFilterAll, Limit: 7})
	if err != nil || result.Summary.Total != 1 || repository.userID != userID || repository.query.Limit != 7 {
		t.Fatalf("MyHNR() = %+v, user=%s query=%+v error=%v", result, repository.userID, repository.query, err)
	}
	if authorizer.request.Action != authz.ActionHNRReadSelf || authorizer.request.Resource.OwnerID != userID ||
		authorizer.request.CredentialAudience != authz.AudienceWebSession {
		t.Fatalf("authorization request = %+v", authorizer.request)
	}
}

func TestMyHNRRejectsInvalidQueryBeforeAuthentication(t *testing.T) {
	t.Parallel()
	service, err := NewService(
		overviewAuthenticatorFixture{err: errors.New("must not authenticate")},
		&overviewAuthorizerFixture{}, &hnrReadRepositoryFixture{}, time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.MyHNR(context.Background(), "cookie", HNRQuery{Filter: "unknown", Limit: 20}); !errors.Is(err, ErrInput) {
		t.Fatalf("MyHNR() error = %v, want ErrInput", err)
	}
}
