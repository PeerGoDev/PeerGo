package ratiowatch

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type appealAuthenticatorFixture struct {
	session           identity.WebSession
	writeCookie       string
	writeCSRF         string
	authenticateCalls int
}

func (fixture *appealAuthenticatorFixture) CurrentSession(context.Context, string) (identity.WebSession, error) {
	return fixture.session, nil
}

func (fixture *appealAuthenticatorFixture) AuthenticateWrite(_ context.Context, cookie, csrf string) (identity.WebSession, error) {
	fixture.writeCookie, fixture.writeCSRF = cookie, csrf
	fixture.authenticateCalls++
	return fixture.session, nil
}

type appealRepositoryFixture struct {
	selfStatusRepositoryFixture
	submitCommand SubmitAppealCommand
	decideCommand DecideAppealCommand
	result        Appeal
}

func (fixture *appealRepositoryFixture) SubmitAppeal(_ context.Context, command SubmitAppealCommand) (Appeal, error) {
	fixture.submitCommand = command
	return fixture.result, nil
}

func (fixture *appealRepositoryFixture) DecideAppeal(_ context.Context, command DecideAppealCommand) (Appeal, error) {
	fixture.decideCommand = command
	return fixture.result, nil
}

func TestSubmitAppealUsesWriteSessionAndCurrentUserPermission(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 8, 0, 0, 0, time.UTC)
	userID, appealID := uuid.New(), uuid.New()
	authenticator := &appealAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: userID}}}
	authorizer := &selfStatusAuthorizerFixture{now: now}
	repository := &appealRepositoryFixture{result: Appeal{ID: appealID}}
	service, err := NewService(repository, authenticator, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.SubmitAppeal(context.Background(), "cookie", "csrf", SubmitAppealInput{
		AppealID:  appealID,
		Statement: "  客户端统计与站内有效流量明显不一致，请帮助核对。  ",
	})
	if err != nil {
		t.Fatalf("SubmitAppeal() error = %v", err)
	}
	if authenticator.authenticateCalls != 1 || authenticator.writeCookie != "cookie" || authenticator.writeCSRF != "csrf" {
		t.Fatalf("write authentication = calls %d, cookie %q, csrf %q", authenticator.authenticateCalls, authenticator.writeCookie, authenticator.writeCSRF)
	}
	if authorizer.request.Action != authz.ActionRatioAppealCreateSelf ||
		authorizer.request.Resource.OwnerID != userID ||
		authorizer.request.CredentialAudience != authz.AudienceWebSession {
		t.Fatalf("authorization request = %+v", authorizer.request)
	}
	if repository.submitCommand.UserID != userID || repository.submitCommand.AppealID != appealID ||
		repository.submitCommand.Statement != "客户端统计与站内有效流量明显不一致，请帮助核对。" ||
		repository.submitCommand.Authorization.ID == uuid.Nil {
		t.Fatalf("submit command = %+v", repository.submitCommand)
	}
}

func TestDecideAppealUsesAssessmentManageAndNormalizesResponse(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 9, 0, 0, 0, time.UTC)
	actorID, appealID := uuid.New(), uuid.New()
	authorizer := &selfStatusAuthorizerFixture{now: now}
	repository := &appealRepositoryFixture{result: Appeal{ID: appealID}}
	service, err := NewService(
		repository,
		&appealAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: uuid.New()}}},
		authorizer,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: actorID, Status: authz.SubjectActive}}
	_, err = service.DecideAppeal(context.Background(), actor, DecideAppealInput{
		AppealID: appealID, Decision: AppealDecisionApprove,
		ExpectedAssessmentVersion: 3,
		Response:                  "  已核对异常记录，批准申诉并解除本期考核。  ",
	})
	if err != nil {
		t.Fatalf("DecideAppeal() error = %v", err)
	}
	if authorizer.request.Action != authz.ActionRatioAssessmentManage || authorizer.request.CredentialAudience != authz.AudienceStaffSession {
		t.Fatalf("authorization request = %+v", authorizer.request)
	}
	if repository.decideCommand.ActorID != actorID || repository.decideCommand.AppealID != appealID ||
		repository.decideCommand.ExpectedAssessmentVersion != 3 ||
		repository.decideCommand.Response != "已核对异常记录，批准申诉并解除本期考核。" {
		t.Fatalf("decision command = %+v", repository.decideCommand)
	}
}

func TestSubmitAppealRejectsShortStatementBeforeAuthentication(t *testing.T) {
	t.Parallel()
	authenticator := &appealAuthenticatorFixture{}
	service, err := NewService(
		&appealRepositoryFixture{}, authenticator,
		&selfStatusAuthorizerFixture{now: time.Now()}, time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SubmitAppeal(context.Background(), "cookie", "csrf", SubmitAppealInput{
		AppealID: uuid.New(), Statement: "太短",
	})
	if err != ErrInput || authenticator.authenticateCalls != 0 {
		t.Fatalf("SubmitAppeal() error = %v, authenticate calls = %d", err, authenticator.authenticateCalls)
	}
}
