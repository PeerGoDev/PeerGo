package identity

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

type downloadRestrictionSessionsStub struct {
	session WebSession
	read    string
	write   string
	csrf    string
}

func (stub *downloadRestrictionSessionsStub) CurrentSession(_ context.Context, token string) (WebSession, error) {
	stub.read = token
	return stub.session, nil
}

func (stub *downloadRestrictionSessionsStub) AuthenticateWrite(_ context.Context, token, csrf string) (WebSession, error) {
	stub.write, stub.csrf = token, csrf
	return stub.session, nil
}

type downloadRestrictionRepositoryStub struct {
	status        DownloadRestrictionStatus
	statusUserID  uuid.UUID
	statusAt      time.Time
	submitCommand SubmitAccountAccessAppealCommand
	submitResult  AccountAccessAppeal
}

func (stub *downloadRestrictionRepositoryStub) DownloadRestrictionStatusByUserID(_ context.Context, userID uuid.UUID, asOf time.Time) (DownloadRestrictionStatus, error) {
	stub.statusUserID, stub.statusAt = userID, asOf
	return stub.status, nil
}

func (stub *downloadRestrictionRepositoryStub) SubmitDownloadRestrictionAppeal(_ context.Context, command SubmitAccountAccessAppealCommand) (AccountAccessAppeal, error) {
	stub.submitCommand = command
	return stub.submitResult, nil
}

type downloadRestrictionAuthorizerStub struct {
	request authz.Request
}

func (stub *downloadRestrictionAuthorizerStub) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	stub.request = request
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed,
		PolicyVersion: authz.PolicyVersion, GrantID: uuid.New(), GrantVersion: 1,
		RoleID: "member", MandateID: uuid.New(),
		EffectiveUntil: request.Context.Now.Add(time.Hour),
	}, nil
}

func TestDownloadRestrictionReadUsesAuthenticatedSelfPermission(t *testing.T) {
	now := time.Date(2026, time.August, 17, 5, 0, 0, 0, time.UTC)
	userID := uuid.New()
	sessions := &downloadRestrictionSessionsStub{session: WebSession{User: User{ID: userID}}}
	repository := &downloadRestrictionRepositoryStub{status: DownloadRestrictionStatus{
		Restricted: true,
		Sources:    DownloadRestrictionSources{ManualOrLegacy: true, HitAndRun: true},
	}}
	authorizer := &downloadRestrictionAuthorizerStub{}
	service, err := NewDownloadRestrictionAppealService(sessions, repository, authorizer, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.MyDownloadRestriction(context.Background(), "web-cookie")
	if err != nil || !result.Restricted || !result.Sources.ManualOrLegacy || !result.Sources.HitAndRun {
		t.Fatalf("MyDownloadRestriction() = %+v, %v", result, err)
	}
	if sessions.read != "web-cookie" || repository.statusUserID != userID || !repository.statusAt.Equal(now) ||
		authorizer.request.Action != authz.ActionUserDownloadRestrictionReadSelf || authorizer.request.Resource.OwnerID != userID {
		t.Fatalf("sessions=%+v repository=%+v authorization=%+v", sessions, repository, authorizer.request)
	}
}

func TestDownloadRestrictionAppealUsesCSRFAndKeepsSourceIndependent(t *testing.T) {
	now := time.Date(2026, time.August, 17, 5, 10, 0, 0, time.UTC)
	userID, appealID := uuid.New(), uuid.New()
	sessions := &downloadRestrictionSessionsStub{session: WebSession{User: User{ID: userID}}}
	repository := &downloadRestrictionRepositoryStub{submitResult: AccountAccessAppeal{
		ID:          appealID,
		Restriction: AccountAccessRestriction{SourceKind: AccountAccessSourceManualDownload},
	}}
	authorizer := &downloadRestrictionAuthorizerStub{}
	service, _ := NewDownloadRestrictionAppealService(sessions, repository, authorizer, func() time.Time { return now })
	result, err := service.SubmitDownloadRestrictionAppeal(
		context.Background(), "web-cookie", "csrf-token",
		SubmitDownloadRestrictionAppealInput{
			AppealID:  appealID,
			Statement: "  旧站下载限制已经不符合当前情况，请管理员单独复核该来源。  ",
		},
	)
	if err != nil || result.ID != appealID || result.Restriction.SourceKind != AccountAccessSourceManualDownload {
		t.Fatalf("SubmitDownloadRestrictionAppeal() = %+v, %v", result, err)
	}
	if sessions.write != "web-cookie" || sessions.csrf != "csrf-token" ||
		repository.submitCommand.UserID != userID || repository.submitCommand.AppealID != appealID ||
		repository.submitCommand.Statement != "旧站下载限制已经不符合当前情况，请管理员单独复核该来源。" ||
		authorizer.request.Action != authz.ActionUserDownloadRestrictionAppealCreateSelf {
		t.Fatalf("sessions=%+v command=%+v authorization=%+v", sessions, repository.submitCommand, authorizer.request)
	}
}
