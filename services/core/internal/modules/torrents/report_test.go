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

func TestCreateTorrentReportUsesVerifiedMemberAndCanonicalEvidence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	verifiedAt := now.Add(-time.Hour)
	reporterID := uuid.New()
	reportID, caseID := uuid.New(), uuid.New()
	repository := &torrentMaintenanceRepositoryStub{reportReceipt: TorrentReportReceipt{
		ID: reportID, CaseID: caseID, TorrentID: 1234,
		ReasonCode: TorrentReportMalicious, CreatedAt: now,
	}}
	authorizer := &recordingTorrentUploadAuthorizer{now: now}
	service, err := NewTorrentMaintenanceService(staticTorrentUploadAuthenticator{session: identity.WebSession{
		User: identity.User{ID: reporterID, EmailVerifiedAt: &verifiedAt},
	}}, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.New()
	result, err := service.CreateTorrentReport(context.Background(), "cookie", "csrf", CreateTorrentReportInput{
		RequestID: requestID, TorrentID: 1234, ReasonCode: TorrentReportMalicious,
		Details: "  解压后的可执行文件与发布说明不符，请管理员复核。  ",
	})
	if err != nil || result.ID != reportID {
		t.Fatalf("CreateTorrentReport()=%+v error=%v", result, err)
	}
	if repository.reportCommand.RequestID != requestID || repository.reportCommand.ReporterID != reporterID ||
		repository.reportCommand.Details != "解压后的可执行文件与发布说明不符，请管理员复核。" ||
		repository.reportCommand.InputSHA256 == ([32]byte{}) {
		t.Fatalf("report command=%+v", repository.reportCommand)
	}
	if authorizer.request.Action != authz.ActionTorrentReportCreateSelf || authorizer.request.Subject.ID != reporterID {
		t.Fatalf("authorization=%+v", authorizer.request)
	}
}

func TestCreateTorrentReportRequiresDetailsForOtherReasonBeforeAuthentication(t *testing.T) {
	t.Parallel()
	repository := &torrentMaintenanceRepositoryStub{}
	authorizer := &recordingTorrentUploadAuthorizer{now: time.Now()}
	service, err := NewTorrentMaintenanceService(staticTorrentUploadAuthenticator{}, authorizer, repository, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateTorrentReport(context.Background(), "", "", CreateTorrentReportInput{
		RequestID: uuid.New(), TorrentID: 42, ReasonCode: TorrentReportOther, Details: "太短",
	})
	if !errors.Is(err, ErrTorrentReportInput) || authorizer.calls != 0 {
		t.Fatalf("error=%v authorization calls=%d", err, authorizer.calls)
	}
}

func TestDecideTorrentReportCaseUsesDedicatedStaffCapability(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 18, 13, 0, 0, 0, time.UTC)
	reviewerID := uuid.New()
	decisionID, caseID := uuid.New(), uuid.New()
	repository := &torrentMaintenanceRepositoryStub{reportDecisionResult: TorrentReportDecisionResult{
		DecisionID: decisionID, CaseID: caseID, TorrentID: 1234,
		Decision:  TorrentReportDisableTorrent,
		CaseState: TorrentReportCaseTorrentDisabled, CaseVersion: 2,
		TorrentState: StateDisabled, TorrentVersion: 9, DecidedAt: now,
	}}
	authorizer := &recordingTorrentUploadAuthorizer{now: now}
	service, err := NewTorrentMaintenanceService(staticTorrentUploadAuthenticator{}, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: reviewerID, Status: authz.SubjectActive}}
	result, err := service.DecideTorrentReportCase(context.Background(), actor, DecideTorrentReportCaseInput{
		DecisionID: decisionID, CaseID: caseID,
		ExpectedCaseVersion: 1, ExpectedTorrentVersion: 8,
		Decision: TorrentReportDisableTorrent, ReasonCode: TorrentReportDecisionMalicious,
		Note: "已复核文件内容存在明确安全风险，先行临时下架并保留证据。",
	})
	if err != nil || result.TorrentState != StateDisabled {
		t.Fatalf("DecideTorrentReportCase()=%+v error=%v", result, err)
	}
	if repository.reportDecision.ReviewerID != reviewerID || repository.reportDecision.DecisionID != decisionID ||
		authorizer.request.Action != authz.ActionTorrentReportReview {
		t.Fatalf("decision=%+v authorization=%+v", repository.reportDecision, authorizer.request)
	}
}
