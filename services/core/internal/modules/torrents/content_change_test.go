package torrents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestSubmitPublishedContentChangeNormalizesCandidateAndUsesSelfCapability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 18, 5, 0, 0, 0, time.UTC)
	verifiedAt := now.Add(-time.Hour)
	userID := uuid.New()
	repository := &torrentMaintenanceRepositoryStub{contentResult: PublishedContentChange{ID: uuid.New(), Status: PublishedContentChangePending}}
	authorizer := &recordingTorrentUploadAuthorizer{now: now}
	service, err := NewTorrentMaintenanceService(staticTorrentUploadAuthenticator{session: identity.WebSession{
		User: identity.User{ID: userID, EmailVerifiedAt: &verifiedAt},
	}}, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.New()
	_, err = service.SubmitPublishedContentChange(context.Background(), "cookie", "csrf", SubmitPublishedContentChangeInput{
		RequestID: requestID, TorrentID: 1234, ExpectedVersion: 8,
		Description: "  新的发布说明  ", MediaInfo: "  General  ", Reason: " 修正已发布种子的内容资料与外部编号。 ",
		ExternalIdentifiers: []ExternalIdentifier{{Provider: "TMDB", ExternalID: "123"}, {Provider: "imdb", ExternalID: "tt1234567"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.contentCommand.UploaderID != userID || repository.contentCommand.RequestID != requestID ||
		repository.contentCommand.Candidate.Description != "新的发布说明" || repository.contentCommand.Candidate.MediaInfo != "General" ||
		len(repository.contentCommand.Candidate.ExternalIdentifiers) != 2 ||
		repository.contentCommand.Candidate.ExternalIdentifiers[0].Provider != "imdb" ||
		repository.contentCommand.Candidate.ExternalIdentifiers[1].Provider != "tmdb" {
		t.Fatalf("content command=%+v", repository.contentCommand)
	}
	if authorizer.request.Action != authz.ActionTorrentContentChangeSubmitSelf || authorizer.request.Subject.ID != userID {
		t.Fatalf("authorization=%+v", authorizer.request)
	}
}

func TestDecidePublishedContentChangeUsesDedicatedStaffCapability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 18, 6, 0, 0, 0, time.UTC)
	repository := &torrentMaintenanceRepositoryStub{contentDecisionResult: PublishedContentChangeDecisionResult{Decision: PublishedContentChangeApprove}}
	authorizer := &recordingTorrentUploadAuthorizer{now: now}
	service, err := NewTorrentMaintenanceService(staticTorrentUploadAuthenticator{}, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	actorID := uuid.New()
	actor := authz.StaffActor{Subject: authz.Subject{ID: actorID, Status: authz.SubjectActive}}
	decisionID, requestID := uuid.New(), uuid.New()
	_, err = service.DecidePublishedContentChange(context.Background(), actor, DecidePublishedContentChangeInput{
		DecisionID: decisionID, RequestID: requestID, ExpectedRequestVersion: 1,
		Decision: PublishedContentChangeApprove, Reason: "内容资料已经核对，可以安全更新公开版本。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.contentDecision.ReviewerID != actorID || repository.contentDecision.DecisionID != decisionID ||
		repository.contentDecision.RequestID != requestID || authorizer.request.Action != authz.ActionTorrentContentChangeReview {
		t.Fatalf("decision=%+v authorization=%+v", repository.contentDecision, authorizer.request)
	}
}
