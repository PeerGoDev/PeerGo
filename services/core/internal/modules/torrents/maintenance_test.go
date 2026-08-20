package torrents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type torrentMaintenanceRepositoryStub struct {
	command                  UpdatePublishedMetadataCommand
	result                   PublishedMetadataRevision
	contentCommand           SubmitPublishedContentChangeCommand
	contentResult            PublishedContentChange
	contentQuery             PublishedContentChangeQuery
	contentPage              ManagedPublishedContentChangePage
	contentDecision          DecidePublishedContentChangeCommand
	contentDecisionResult    PublishedContentChangeDecisionResult
	withdrawalCommand        SubmitTorrentWithdrawalCommand
	withdrawalResult         TorrentWithdrawalRequest
	withdrawalQuery          TorrentWithdrawalQuery
	withdrawalPage           ManagedTorrentWithdrawalPage
	withdrawalDecision       DecideTorrentWithdrawalCommand
	withdrawalDecisionResult TorrentWithdrawalDecisionResult
	reportCommand            CreateTorrentReportCommand
	reportReceipt            TorrentReportReceipt
	reportQuery              TorrentReportCaseQuery
	reportPage               ManagedTorrentReportCasePage
	reportDecision           DecideTorrentReportCaseCommand
	reportDecisionResult     TorrentReportDecisionResult
}

func (stub *torrentMaintenanceRepositoryStub) UpdatePublishedMetadata(_ context.Context, command UpdatePublishedMetadataCommand) (PublishedMetadataRevision, error) {
	stub.command = command
	return stub.result, nil
}

func (stub *torrentMaintenanceRepositoryStub) SubmitPublishedContentChange(_ context.Context, command SubmitPublishedContentChangeCommand) (PublishedContentChange, error) {
	stub.contentCommand = command
	return stub.contentResult, nil
}

func (stub *torrentMaintenanceRepositoryStub) ListPublishedContentChanges(_ context.Context, query PublishedContentChangeQuery) (ManagedPublishedContentChangePage, error) {
	stub.contentQuery = query
	return stub.contentPage, nil
}

func (stub *torrentMaintenanceRepositoryStub) DecidePublishedContentChange(_ context.Context, command DecidePublishedContentChangeCommand) (PublishedContentChangeDecisionResult, error) {
	stub.contentDecision = command
	return stub.contentDecisionResult, nil
}

func (stub *torrentMaintenanceRepositoryStub) SubmitPublishedScreenshotChange(_ context.Context, _ SubmitPublishedScreenshotChangeCommand) (PublishedScreenshotChange, error) {
	return PublishedScreenshotChange{}, nil
}

func (stub *torrentMaintenanceRepositoryStub) ListPublishedScreenshotChanges(_ context.Context, _ PublishedScreenshotChangeQuery) (ManagedPublishedScreenshotChangePage, error) {
	return ManagedPublishedScreenshotChangePage{}, nil
}

func (stub *torrentMaintenanceRepositoryStub) DecidePublishedScreenshotChange(_ context.Context, _ DecidePublishedScreenshotChangeCommand) (PublishedScreenshotChangeDecisionResult, error) {
	return PublishedScreenshotChangeDecisionResult{}, nil
}

func (stub *torrentMaintenanceRepositoryStub) PublishedScreenshotChangeSource(_ context.Context, _ uuid.UUID, _ ScreenshotChangeSide, _ int) (PublicScreenshotSource, error) {
	return PublicScreenshotSource{}, nil
}

func (stub *torrentMaintenanceRepositoryStub) SubmitTorrentWithdrawal(_ context.Context, command SubmitTorrentWithdrawalCommand) (TorrentWithdrawalRequest, error) {
	stub.withdrawalCommand = command
	return stub.withdrawalResult, nil
}

func (stub *torrentMaintenanceRepositoryStub) ListTorrentWithdrawals(_ context.Context, query TorrentWithdrawalQuery) (ManagedTorrentWithdrawalPage, error) {
	stub.withdrawalQuery = query
	return stub.withdrawalPage, nil
}

func (stub *torrentMaintenanceRepositoryStub) DecideTorrentWithdrawal(_ context.Context, command DecideTorrentWithdrawalCommand) (TorrentWithdrawalDecisionResult, error) {
	stub.withdrawalDecision = command
	return stub.withdrawalDecisionResult, nil
}

func (stub *torrentMaintenanceRepositoryStub) CreateTorrentReport(_ context.Context, command CreateTorrentReportCommand) (TorrentReportReceipt, error) {
	stub.reportCommand = command
	return stub.reportReceipt, nil
}

func (stub *torrentMaintenanceRepositoryStub) ListTorrentReportCases(_ context.Context, query TorrentReportCaseQuery) (ManagedTorrentReportCasePage, error) {
	stub.reportQuery = query
	return stub.reportPage, nil
}

func (stub *torrentMaintenanceRepositoryStub) DecideTorrentReportCase(_ context.Context, command DecideTorrentReportCaseCommand) (TorrentReportDecisionResult, error) {
	stub.reportDecision = command
	return stub.reportDecisionResult, nil
}

func TestTorrentMaintenanceUsesVerifiedSelfAuthorizationAndNormalizedMetadata(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 18, 4, 0, 0, 0, time.UTC)
	verifiedAt := now.Add(-time.Hour)
	userID := uuid.New()
	repository := &torrentMaintenanceRepositoryStub{result: PublishedMetadataRevision{TorrentID: 1234, Version: 8}}
	authorizer := &recordingTorrentUploadAuthorizer{now: now}
	service, err := NewTorrentMaintenanceService(staticTorrentUploadAuthenticator{session: identity.WebSession{
		User: identity.User{ID: userID, EmailVerifiedAt: &verifiedAt},
	}}, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.New()
	result, err := service.UpdatePublishedMetadata(context.Background(), "cookie", "csrf", UpdatePublishedMetadataInput{
		RequestID: requestID, TorrentID: 1234, ExpectedVersion: 7,
		CategoryID: " movies ", Title: " Corrected title ", Subtitle: " New subtitle ",
		Reason: " 修正发布后的标题与分类说明。 ",
	})
	if err != nil || result.Version != 8 {
		t.Fatalf("UpdatePublishedMetadata()=%+v error=%v", result, err)
	}
	if repository.command.Metadata != (EditableMetadata{CategoryID: "movies", Title: "Corrected title", Subtitle: "New subtitle"}) ||
		repository.command.Reason != "修正发布后的标题与分类说明。" || repository.command.UploaderID != userID {
		t.Fatalf("command = %+v", repository.command)
	}
	if authorizer.calls != 1 || authorizer.request.Action != authz.ActionTorrentMetadataUpdateSelf || authorizer.request.Subject.ID != userID {
		t.Fatalf("authorization = %+v", authorizer.request)
	}
}

func TestTorrentMaintenanceRejectsInvalidInputBeforeAuthenticationAndRequiresVerifiedEmail(t *testing.T) {
	t.Parallel()
	repository := &torrentMaintenanceRepositoryStub{}
	authorizer := &recordingTorrentUploadAuthorizer{now: time.Now()}
	service, err := NewTorrentMaintenanceService(staticTorrentUploadAuthenticator{}, authorizer, repository, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.UpdatePublishedMetadata(context.Background(), "", "", UpdatePublishedMetadataInput{
		RequestID: uuid.New(), TorrentID: 1, ExpectedVersion: 1,
		CategoryID: "movies", Title: "title", Reason: "too short",
	})
	if !errors.Is(err, ErrTorrentMetadataUpdateInput) || authorizer.calls != 0 {
		t.Fatalf("invalid input error=%v calls=%d", err, authorizer.calls)
	}

	unverifiedID := uuid.New()
	service, err = NewTorrentMaintenanceService(staticTorrentUploadAuthenticator{session: identity.WebSession{
		User: identity.User{ID: unverifiedID},
	}}, authorizer, repository, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.UpdatePublishedMetadata(context.Background(), "cookie", "csrf", UpdatePublishedMetadataInput{
		RequestID: uuid.New(), TorrentID: 1, ExpectedVersion: 1,
		CategoryID: "movies", Title: "title", Reason: "这是一个满足长度要求的修改说明。",
	})
	if !errors.Is(err, ErrTorrentMetadataUpdateEmailUnverified) || authorizer.calls != 0 {
		t.Fatalf("unverified error=%v calls=%d", err, authorizer.calls)
	}
}
