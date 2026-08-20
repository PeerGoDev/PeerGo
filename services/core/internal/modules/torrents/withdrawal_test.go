package torrents

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func TestSubmitTorrentWithdrawalUsesVerifiedOwnerCapability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 18, 7, 0, 0, 0, time.UTC)
	verifiedAt := now.Add(-time.Hour)
	uploaderID := uuid.New()
	repository := &torrentMaintenanceRepositoryStub{withdrawalResult: TorrentWithdrawalRequest{ID: uuid.New(), Status: TorrentWithdrawalPending}}
	authorizer := &recordingTorrentUploadAuthorizer{now: now}
	service, err := NewTorrentMaintenanceService(staticTorrentUploadAuthenticator{session: identity.WebSession{
		User: identity.User{ID: uploaderID, EmailVerifiedAt: &verifiedAt},
	}}, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.New()
	_, err = service.SubmitTorrentWithdrawal(context.Background(), "cookie", "csrf", SubmitTorrentWithdrawalInput{
		RequestID: requestID, TorrentID: 1234, ExpectedVersion: 8,
		Reason: "  原发布内容已经有更完整的替代版本，因此申请撤回。  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.withdrawalCommand.RequestID != requestID || repository.withdrawalCommand.UploaderID != uploaderID ||
		repository.withdrawalCommand.Reason != "原发布内容已经有更完整的替代版本，因此申请撤回。" {
		t.Fatalf("withdrawal command=%+v", repository.withdrawalCommand)
	}
	if authorizer.request.Action != authz.ActionTorrentWithdrawRequestSelf || authorizer.request.Subject.ID != uploaderID {
		t.Fatalf("authorization=%+v", authorizer.request)
	}
}

func TestDecideTorrentWithdrawalUsesDedicatedStaffCapability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 18, 8, 0, 0, 0, time.UTC)
	repository := &torrentMaintenanceRepositoryStub{withdrawalDecisionResult: TorrentWithdrawalDecisionResult{Decision: TorrentWithdrawalApprove}}
	authorizer := &recordingTorrentUploadAuthorizer{now: now}
	service, err := NewTorrentMaintenanceService(staticTorrentUploadAuthenticator{}, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	reviewerID := uuid.New()
	actor := authz.StaffActor{Subject: authz.Subject{ID: reviewerID, Status: authz.SubjectActive}}
	decisionID, requestID := uuid.New(), uuid.New()
	_, err = service.DecideTorrentWithdrawal(context.Background(), actor, DecideTorrentWithdrawalInput{
		DecisionID: decisionID, RequestID: requestID, ExpectedRequestVersion: 1,
		Decision: TorrentWithdrawalApprove, Reason: "已确认没有有效购买权益，可以保留证据后完成撤回。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.withdrawalDecision.ReviewerID != reviewerID || repository.withdrawalDecision.DecisionID != decisionID ||
		repository.withdrawalDecision.RequestID != requestID || authorizer.request.Action != authz.ActionTorrentWithdrawReview {
		t.Fatalf("decision=%+v authorization=%+v", repository.withdrawalDecision, authorizer.request)
	}
}
