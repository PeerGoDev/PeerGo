package seedingreward

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type administrationRepositoryStub struct {
	timelineRepositoryStub
	items []PublishedPolicy
	total int64
}

func (stub *administrationRepositoryStub) List(context.Context, int, int) ([]PublishedPolicy, int64, error) {
	return stub.items, stub.total, stub.err
}

type administrationAuthorizerStub struct {
	now      time.Time
	requests []authz.Request
}

func (stub *administrationAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.requests = append(stub.requests, request)
	return authz.Decision{
		ID: uuid.New(), Allow: true, GrantID: uuid.New(), GrantVersion: 1,
		MandateID: uuid.New(), RoleID: "site_admin", EffectiveUntil: stub.now.Add(time.Hour),
	}, nil
}

func TestAdministrationPreviewUsesFuturePolicyAndProductionCalculator(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 16, 3, 15, 0, 0, time.UTC)
	repository := &administrationRepositoryStub{}
	authorizer := &administrationAuthorizerStub{now: now}
	service, err := NewAdministrationService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	policy := testPolicy()
	policy.EffectiveFrom = minimumEffectiveFrom(now)
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}

	preview, err := service.Preview(context.Background(), actor, policy)
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if len(preview.Results) != 4 || preview.PolicySHA256 == ([32]byte{}) {
		t.Fatalf("preview = %+v", preview)
	}
	if authorizer.requests[0].Action != authz.ActionEconomySeedingRewardPolicyRead {
		t.Fatalf("authorization request = %+v", authorizer.requests[0])
	}
}

func TestAdministrationRejectsPolicyBeforeMinimumEffectiveTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 16, 3, 15, 0, 0, time.UTC)
	service, err := NewAdministrationService(
		&administrationRepositoryStub{}, &administrationAuthorizerStub{now: now}, func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	policy := testPolicy()
	policy.EffectiveFrom = minimumEffectiveFrom(now).Add(-time.Hour)
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}}
	if _, err := service.Preview(context.Background(), actor, policy); !errors.Is(err, ErrInput) {
		t.Fatalf("Preview() error = %v, want ErrInput", err)
	}
}
