package promotions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/promotioncontrolv1"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type promotionRepositoryStub struct {
	page          Page
	command       ScheduleCommand
	listCalls     int
	scheduleCalls int
}

func (stub *promotionRepositoryStub) List(_ context.Context, _, _ int, _ time.Time) (Page, error) {
	stub.listCalls++
	return stub.page, nil
}

func (stub *promotionRepositoryStub) Schedule(_ context.Context, command ScheduleCommand) (Campaign, error) {
	stub.scheduleCalls++
	stub.command = command
	return Campaign{ID: command.CampaignID}, nil
}

type promotionAuthorizerStub struct {
	decision authz.Decision
	err      error
	requests []authz.Request
}

func (stub *promotionAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.requests = append(stub.requests, request)
	return stub.decision, stub.err
}

func TestPromotionScheduleProducesCanonicalSettlementCommand(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	campaignID := uuid.MustParse("0198f20a-6da8-7e51-9c64-515151515151")
	repository := &promotionRepositoryStub{}
	authorizer := &promotionAuthorizerStub{decision: promotionAllowedDecision(now)}
	service, err := NewService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	campaign, err := service.Schedule(context.Background(), promotionTestActor(), ScheduleInput{
		CampaignID: campaignID, Scope: ScopeGlobal, Promotion: PromotionDoubleUploadFree,
		StartsAt: now.Add(time.Hour), EndsAt: now.Add(25 * time.Hour),
		Reason: "  周末全站做种活动，鼓励成员补种。  ",
	})
	if err != nil || campaign.ID != campaignID {
		t.Fatalf("Schedule() = %+v, %v", campaign, err)
	}
	if repository.scheduleCalls != 1 || len(authorizer.requests) != 1 ||
		authorizer.requests[0].Action != authz.ActionPromotionSchedule ||
		authorizer.requests[0].CredentialAudience != authz.AudienceStaffSession {
		t.Fatalf("dependencies = repository %d authorization %+v", repository.scheduleCalls, authorizer.requests)
	}
	if repository.command.Reason != "周末全站做种活动，鼓励成员补种。" ||
		repository.command.Authorization.ID != authorizer.decision.ID {
		t.Fatalf("schedule command = %+v", repository.command)
	}
	decoded, err := promotioncontrolv1.Decode(repository.command.CommandJSON)
	if err != nil {
		t.Fatalf("Decode(command) error = %v", err)
	}
	if decoded.CampaignID != campaignID.String() || decoded.Scope != promotioncontrolv1.ScopeGlobal ||
		decoded.Promotion != promotioncontrolv1.PromotionDoubleUploadFree || !decoded.OverrideLowerScopes {
		t.Fatalf("settlement command = %+v", decoded)
	}
	digest, err := promotioncontrolv1.SHA256(repository.command.CommandJSON)
	if err != nil || digest != repository.command.CommandSHA256 {
		t.Fatalf("command digest = %x, %v", digest, err)
	}
}

func TestPromotionScheduleRejectsInvalidInputBeforeAuthorization(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	repository := &promotionRepositoryStub{}
	authorizer := &promotionAuthorizerStub{err: errors.New("must not be called")}
	service, err := NewService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = service.Schedule(context.Background(), promotionTestActor(), ScheduleInput{
		CampaignID: uuid.New(), Scope: ScopeTorrent, Promotion: PromotionFree,
		StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour), Reason: "缺少目标种子编号，不能继续签发。",
	})
	if !errors.Is(err, ErrInput) {
		t.Fatalf("Schedule() error = %v, want ErrInput", err)
	}
	if repository.scheduleCalls != 0 || len(authorizer.requests) != 0 {
		t.Fatalf("invalid input reached dependencies: repository=%d authorization=%d", repository.scheduleCalls, len(authorizer.requests))
	}
}

func TestPromotionListUsesDedicatedReadCapability(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	repository := &promotionRepositoryStub{page: Page{Total: 2}}
	authorizer := &promotionAuthorizerStub{decision: promotionAllowedDecision(now)}
	service, err := NewService(repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	page, err := service.List(context.Background(), promotionTestActor(), 25, 0)
	if err != nil || page.Total != 2 {
		t.Fatalf("List() = %+v, %v", page, err)
	}
	if repository.listCalls != 1 || len(authorizer.requests) != 1 ||
		authorizer.requests[0].Action != authz.ActionPromotionManageRead {
		t.Fatalf("dependencies = repository %d authorization %+v", repository.listCalls, authorizer.requests)
	}
}

func promotionTestActor() authz.StaffActor {
	return authz.StaffActor{Subject: authz.Subject{
		ID: uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"), Status: authz.SubjectActive,
	}}
}

func promotionAllowedDecision(now time.Time) authz.Decision {
	return authz.Decision{
		ID: uuid.MustParse("0198f20a-6da8-7e51-9c64-121212121212"), Allow: true,
		Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.MustParse("0198f20a-6da8-7e51-9c64-131313131313"), GrantVersion: 1,
		RoleID: "site_admin", MandateID: uuid.MustParse("0198f20a-6da8-7e51-9c64-141414141414"),
		EffectiveUntil: now.Add(time.Hour),
	}
}
