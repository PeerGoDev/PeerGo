package review

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

type resubmissionAuthenticatorFixture struct {
	session     identity.WebSession
	calls       int
	cookieToken string
	csrfToken   string
}

func (fixture *resubmissionAuthenticatorFixture) AuthenticateWrite(
	_ context.Context,
	cookieToken string,
	csrfToken string,
) (identity.WebSession, error) {
	fixture.calls++
	fixture.cookieToken = cookieToken
	fixture.csrfToken = csrfToken
	return fixture.session, nil
}

type resubmissionRepositoryFixture struct {
	command ResubmitCommand
	result  ResubmissionResult
	err     error
}

func (fixture *resubmissionRepositoryFixture) Resubmit(
	_ context.Context,
	command ResubmitCommand,
) (ResubmissionResult, error) {
	fixture.command = command
	return fixture.result, fixture.err
}

func TestResubmissionServiceUsesVerifiedWebWriteAndTypedSelfAuthorization(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 10, 15, 0, 0, 0, time.UTC)
	userID, torrentID, commandID := uuid.New(), torrents.TorrentID(44), uuid.New()
	verifiedAt := now.Add(-time.Hour)
	authenticator := &resubmissionAuthenticatorFixture{session: identity.WebSession{
		User: identity.User{ID: userID, EmailVerifiedAt: &verifiedAt},
	}}
	authorizer := &recordingAuthorizer{decision: authz.Decision{
		ID: uuid.New(), Allow: true, Reason: authz.ReasonAllowed, PolicyVersion: authz.PolicyVersion,
		GrantID: uuid.New(), GrantVersion: 1, RoleID: "member", MandateID: uuid.New(),
		EffectiveUntil: now.Add(time.Hour),
	}}
	want := ResubmissionResult{
		ID: commandID, TorrentID: torrentID, State: torrents.StatePendingReview,
		Version: 3, Metadata: torrents.EditableMetadata{
			CategoryID: "movies", Title: "Corrected release", Subtitle: "Updated subtitle",
		}, ReviewRequestedAt: now,
	}
	repository := &resubmissionRepositoryFixture{result: want}
	service, err := NewResubmissionService(authenticator, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.Resubmit(context.Background(), "cookie", "csrf", ResubmitInput{
		ID: commandID, TorrentID: torrentID, ExpectedVersion: 2,
		CategoryID: " movies ", Title: " Corrected release ", Subtitle: " Updated subtitle ",
		CorrectionNote: " 已补充并修正审核指出的发布信息，请重新核对。 ",
	})
	if err != nil || got != want {
		t.Fatalf("Resubmit() = %+v, %v", got, err)
	}
	if authenticator.calls != 1 || authenticator.cookieToken != "cookie" || authenticator.csrfToken != "csrf" {
		t.Fatalf("authenticator = %+v", authenticator)
	}
	if authorizer.request.Action != authz.ActionTorrentSubmissionResubmitSelf ||
		authorizer.request.CredentialAudience != authz.AudienceWebSession ||
		authorizer.request.Resource.OwnerID != userID {
		t.Fatalf("authorization request = %+v", authorizer.request)
	}
	if repository.command.UploaderID != userID || repository.command.ExpectedVersion != 2 ||
		repository.command.Metadata != want.Metadata ||
		repository.command.CorrectionNote != "已补充并修正审核指出的发布信息，请重新核对。" ||
		repository.command.Authorization.ID == uuid.Nil || !repository.command.OccurredAt.Equal(now) {
		t.Fatalf("repository command = %+v", repository.command)
	}
}

func TestResubmissionServiceRejectsInvalidInputBeforeAuthentication(t *testing.T) {
	t.Parallel()

	authenticator := &resubmissionAuthenticatorFixture{}
	repository := &resubmissionRepositoryFixture{}
	service, err := NewResubmissionService(authenticator, &recordingAuthorizer{}, repository, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Resubmit(context.Background(), "cookie", "csrf", ResubmitInput{
		ID: uuid.New(), TorrentID: 44, ExpectedVersion: 2,
		CategoryID: "movies", Title: "Corrected release", CorrectionNote: "太短",
	})
	if !errors.Is(err, ErrTorrentResubmissionInput) {
		t.Fatalf("Resubmit() error = %v", err)
	}
	if authenticator.calls != 0 || repository.command.ID != uuid.Nil {
		t.Fatal("invalid input reached authentication or repository")
	}
}

func TestResubmissionServiceRequiresVerifiedEmail(t *testing.T) {
	t.Parallel()

	authenticator := &resubmissionAuthenticatorFixture{session: identity.WebSession{
		User: identity.User{ID: uuid.New()},
	}}
	authorizer := &recordingAuthorizer{}
	repository := &resubmissionRepositoryFixture{}
	service, err := NewResubmissionService(authenticator, authorizer, repository, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Resubmit(context.Background(), "cookie", "csrf", ResubmitInput{
		ID: uuid.New(), TorrentID: 44, ExpectedVersion: 2,
		CategoryID: "movies", Title: "Corrected release",
		CorrectionNote: "已补充并修正审核指出的发布信息，请重新核对。",
	})
	if !errors.Is(err, ErrTorrentResubmissionEmailUnverified) {
		t.Fatalf("Resubmit() error = %v", err)
	}
	if authorizer.request.Action != "" || repository.command.ID != uuid.Nil {
		t.Fatal("unverified account reached authorization or repository")
	}
}

func TestMetadataResubmissionAllowedOnlyForMetadataRejection(t *testing.T) {
	t.Parallel()

	if !MetadataResubmissionAllowed(torrents.StateRejected, ReasonMetadataIncomplete) {
		t.Fatal("metadata rejection should allow resubmission")
	}
	for _, test := range []struct {
		state  torrents.State
		reason ReasonCode
	}{
		{torrents.StatePendingReview, ReasonMetadataIncomplete},
		{torrents.StateRejected, ReasonDuplicateOrSuperseded},
		{torrents.StateRejected, ReasonUploaderActionRequired},
	} {
		if MetadataResubmissionAllowed(test.state, test.reason) {
			t.Fatalf("unexpected resubmission allowance for state=%s reason=%s", test.state, test.reason)
		}
	}
}
