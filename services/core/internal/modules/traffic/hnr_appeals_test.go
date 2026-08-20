package traffic

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type hnrAppealAuthenticatorFixture struct {
	session           identity.WebSession
	writeCookie       string
	writeCSRF         string
	authenticateCalls int
}

func (fixture *hnrAppealAuthenticatorFixture) CurrentSession(context.Context, string) (identity.WebSession, error) {
	return fixture.session, nil
}

func (fixture *hnrAppealAuthenticatorFixture) AuthenticateWrite(_ context.Context, cookie, csrf string) (identity.WebSession, error) {
	fixture.writeCookie, fixture.writeCSRF = cookie, csrf
	fixture.authenticateCalls++
	return fixture.session, nil
}

type hnrAppealRepositoryFixture struct {
	submitCommand SubmitHNRAppealCommand
	listQuery     HNRAppealQuery
	decideCommand DecideHNRAppealCommand
	appeal        HNRAppeal
	page          HNRAppealPage
}

func (*hnrAppealRepositoryFixture) Overview(context.Context, uuid.UUID, int) (Overview, error) {
	return Overview{}, nil
}

func (*hnrAppealRepositoryFixture) ListHNR(context.Context, uuid.UUID, HNRQuery) (HNRPage, error) {
	return HNRPage{}, nil
}

func (fixture *hnrAppealRepositoryFixture) SubmitHNRAppeal(_ context.Context, command SubmitHNRAppealCommand) (HNRAppeal, error) {
	fixture.submitCommand = command
	return fixture.appeal, nil
}

func (fixture *hnrAppealRepositoryFixture) HNRAppeals(_ context.Context, query HNRAppealQuery) (HNRAppealPage, error) {
	fixture.listQuery = query
	return fixture.page, nil
}

func (fixture *hnrAppealRepositoryFixture) DecideHNRAppeal(_ context.Context, command DecideHNRAppealCommand) (HNRAppeal, error) {
	fixture.decideCommand = command
	return fixture.appeal, nil
}

func TestSubmitHNRAppealUsesWriteSessionAndSelfPermission(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)
	userID, appealID, obligationID := uuid.New(), uuid.New(), uuid.New()
	authenticator := &hnrAppealAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: userID}}}
	authorizer := &overviewAuthorizerFixture{now: now}
	repository := &hnrAppealRepositoryFixture{appeal: HNRAppeal{ID: appealID}}
	service, err := NewService(authenticator, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.SubmitHNRAppeal(context.Background(), "cookie", "csrf", SubmitHNRAppealInput{
		AppealID: appealID, ObligationID: obligationID,
		Statement: "  客户端异常退出后已恢复做种，请帮助核对这条 H&R 记录。  ",
	})
	if err != nil {
		t.Fatalf("SubmitHNRAppeal() error = %v", err)
	}
	if authenticator.authenticateCalls != 1 || authenticator.writeCookie != "cookie" || authenticator.writeCSRF != "csrf" {
		t.Fatalf("write authentication = calls %d, cookie %q, csrf %q", authenticator.authenticateCalls, authenticator.writeCookie, authenticator.writeCSRF)
	}
	if authorizer.request.Action != authz.ActionHNRAppealCreateSelf ||
		authorizer.request.Resource.OwnerID != userID ||
		authorizer.request.CredentialAudience != authz.AudienceWebSession {
		t.Fatalf("authorization request = %+v", authorizer.request)
	}
	if repository.submitCommand.UserID != userID || repository.submitCommand.AppealID != appealID ||
		repository.submitCommand.ObligationID != obligationID ||
		repository.submitCommand.Statement != "客户端异常退出后已恢复做种，请帮助核对这条 H&R 记录。" ||
		repository.submitCommand.Authorization.ID == uuid.Nil {
		t.Fatalf("submit command = %+v", repository.submitCommand)
	}
}

func TestDecideHNRAppealUsesManagePermissionAndOptimisticVersion(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 11, 0, 0, 0, time.UTC)
	actorID, appealID := uuid.New(), uuid.New()
	authorizer := &overviewAuthorizerFixture{now: now}
	repository := &hnrAppealRepositoryFixture{appeal: HNRAppeal{ID: appealID}}
	service, err := NewService(
		&hnrAppealAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: uuid.New()}}},
		authorizer, repository, func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: actorID, Status: authz.SubjectActive}}
	_, err = service.DecideHNRAppeal(context.Background(), actor, DecideHNRAppealInput{
		AppealID: appealID, Decision: HNRAppealDecisionApprove,
		ExpectedObligationVersion: 4,
		Response:                  "  已核对异常汇报，批准本条义务豁免。  ",
	})
	if err != nil {
		t.Fatalf("DecideHNRAppeal() error = %v", err)
	}
	if authorizer.request.Action != authz.ActionHNRAssessmentManage ||
		authorizer.request.CredentialAudience != authz.AudienceStaffSession {
		t.Fatalf("authorization request = %+v", authorizer.request)
	}
	if repository.decideCommand.ActorID != actorID || repository.decideCommand.AppealID != appealID ||
		repository.decideCommand.ExpectedObligationVersion != 4 ||
		repository.decideCommand.Response != "已核对异常汇报，批准本条义务豁免。" {
		t.Fatalf("decision command = %+v", repository.decideCommand)
	}
}

func TestHNRAppealsUsesPolicyReadAndNormalizesQuery(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	actorID := uuid.New()
	authorizer := &overviewAuthorizerFixture{now: now}
	repository := &hnrAppealRepositoryFixture{page: HNRAppealPage{Total: 2}}
	service, err := NewService(
		&hnrAppealAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: uuid.New()}}},
		authorizer, repository, func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: actorID, Status: authz.SubjectActive}}
	page, err := service.HNRAppeals(context.Background(), actor, HNRAppealQuery{
		Query: "  username  ", Filter: HNRAppealFilterAll, Limit: 30,
	})
	if err != nil || page.Total != 2 {
		t.Fatalf("HNRAppeals() = %+v, error = %v", page, err)
	}
	if authorizer.request.Action != authz.ActionHNRPolicyRead || repository.listQuery.Query != "username" {
		t.Fatalf("authorization = %+v, query = %+v", authorizer.request, repository.listQuery)
	}
}

func TestSubmitHNRAppealRejectsShortStatementBeforeAuthentication(t *testing.T) {
	t.Parallel()
	authenticator := &hnrAppealAuthenticatorFixture{}
	service, err := NewService(authenticator, &overviewAuthorizerFixture{now: time.Now()}, &hnrAppealRepositoryFixture{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SubmitHNRAppeal(context.Background(), "cookie", "csrf", SubmitHNRAppealInput{
		AppealID: uuid.New(), ObligationID: uuid.New(), Statement: "太短",
	})
	if err != ErrInput || authenticator.authenticateCalls != 0 {
		t.Fatalf("SubmitHNRAppeal() error = %v, authenticate calls = %d", err, authenticator.authenticateCalls)
	}
}
