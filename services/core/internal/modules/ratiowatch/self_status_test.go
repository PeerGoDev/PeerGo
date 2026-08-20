package ratiowatch

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type selfStatusAuthenticatorFixture struct {
	session identity.WebSession
}

func (fixture selfStatusAuthenticatorFixture) CurrentSession(context.Context, string) (identity.WebSession, error) {
	return fixture.session, nil
}

func (fixture selfStatusAuthenticatorFixture) AuthenticateWrite(context.Context, string, string) (identity.WebSession, error) {
	return fixture.session, nil
}

type selfStatusAuthorizerFixture struct {
	request authz.Request
	now     time.Time
}

func (fixture *selfStatusAuthorizerFixture) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	fixture.request = request
	return authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed,
		GrantID: uuid.New(), GrantVersion: 1, MandateID: uuid.New(), RoleID: "member",
		EffectiveUntil: fixture.now.Add(time.Hour),
	}, nil
}

type selfStatusRepositoryFixture struct {
	userID uuid.UUID
	now    time.Time
	status MyStatus
}

func (fixture *selfStatusRepositoryFixture) MyStatus(_ context.Context, userID uuid.UUID, now time.Time) (MyStatus, error) {
	fixture.userID, fixture.now = userID, now
	return fixture.status, nil
}

func (*selfStatusRepositoryFixture) Policies(context.Context, int, int, time.Time) (PolicyPage, error) {
	return PolicyPage{}, nil
}

func (*selfStatusRepositoryFixture) Preview(context.Context, PolicyInput, time.Time) (ImpactPreview, error) {
	return ImpactPreview{}, nil
}

func (*selfStatusRepositoryFixture) Issue(context.Context, IssueCommand) (PolicyRevision, error) {
	return PolicyRevision{}, nil
}

func (*selfStatusRepositoryFixture) Assessments(context.Context, AssessmentQuery) (AssessmentPage, error) {
	return AssessmentPage{}, nil
}

func (*selfStatusRepositoryFixture) Clear(context.Context, ClearCommand) (Assessment, error) {
	return Assessment{}, nil
}

func (*selfStatusRepositoryFixture) SubmitAppeal(context.Context, SubmitAppealCommand) (Appeal, error) {
	return Appeal{}, nil
}

func (*selfStatusRepositoryFixture) Appeals(context.Context, AppealQuery) (AppealPage, error) {
	return AppealPage{}, nil
}

func (*selfStatusRepositoryFixture) DecideAppeal(context.Context, DecideAppealCommand) (Appeal, error) {
	return Appeal{}, nil
}

func TestMyStatusUsesVerifiedSessionSubjectAndSelfPermission(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 16, 15, 0, 0, 0, time.UTC)
	userID := uuid.New()
	want := MyStatus{CreditedUploaded: 42, ObservedAt: now}
	authorizer := &selfStatusAuthorizerFixture{now: now}
	repository := &selfStatusRepositoryFixture{status: want}
	service, err := NewService(
		repository,
		selfStatusAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: userID}}},
		authorizer,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.MyStatus(context.Background(), "opaque-cookie")
	if err != nil {
		t.Fatalf("MyStatus() error = %v", err)
	}
	if got.CreditedUploaded != want.CreditedUploaded || repository.userID != userID || !repository.now.Equal(now) {
		t.Fatalf("MyStatus() = %+v, repository user=%s now=%s", got, repository.userID, repository.now)
	}
	if authorizer.request.Action != authz.ActionRatioAssessmentReadSelf ||
		authorizer.request.Resource.OwnerID != userID ||
		authorizer.request.CredentialAudience != authz.AudienceWebSession {
		t.Fatalf("authorization request = %+v", authorizer.request)
	}
}
