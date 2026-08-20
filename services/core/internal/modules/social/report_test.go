package social

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type commentModerationRepositoryFixture struct {
	receipt       CommentReportReceipt
	page          CommentModerationCasePage
	result        CommentModerationDecisionResult
	reportCommand createCommentReportCommand
	listLimit     int
	listOffset    int
	decideCommand decideCommentModerationCaseCommand
}

func (fixture *commentModerationRepositoryFixture) CreateReport(_ context.Context, command createCommentReportCommand) (CommentReportReceipt, error) {
	fixture.reportCommand = command
	return fixture.receipt, nil
}

func (fixture *commentModerationRepositoryFixture) ListOpenCases(_ context.Context, limit, offset int) (CommentModerationCasePage, error) {
	fixture.listLimit, fixture.listOffset = limit, offset
	return fixture.page, nil
}

func (fixture *commentModerationRepositoryFixture) Decide(_ context.Context, command decideCommentModerationCaseCommand) (CommentModerationDecisionResult, error) {
	fixture.decideCommand = command
	return fixture.result, nil
}

func TestCommentModerationServiceSeparatesPublicReportingFromStaffResolution(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 9, 20, 0, 0, 0, time.UTC)
	reporterID, authorID, moderatorID := uuid.New(), uuid.New(), uuid.New()
	const torrentID int64 = 42
	commentID, reportID, caseID, decisionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	receipt := CommentReportReceipt{
		ID: reportID, CommentID: commentID, ReasonCode: CommentReportOffTopic, CreatedAt: now,
	}
	comment := Comment{
		ID: commentID, Target: TorrentCommentTarget(torrentID),
		Author: CommentAuthor{ID: authorID, DisplayName: "评论作者"},
		Body:   "需要审核的可见评论", BodyFormat: CommentBodyPlainText,
		State: CommentVisible, Version: 3, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour),
	}
	page := CommentModerationCasePage{
		Items: []CommentModerationCase{{
			ID: caseID, State: CommentModerationCaseOpen, Version: 2,
			Target: CommentModerationTarget{CommentTarget: TorrentCommentTarget(torrentID), Title: "评论审核演示种子"}, Comment: comment,
			ReportCount: 1,
			Reports:     []CommentModerationReport{{ReasonCode: CommentReportOffTopic, Details: "偏离资源讨论", CreatedAt: now}},
			OpenedAt:    now, LatestReportedAt: now,
		}},
		Total: 1, Limit: 20, Offset: 0,
	}
	result := CommentModerationDecisionResult{
		DecisionID: decisionID, CaseID: caseID, CommentID: commentID,
		Decision: CommentModerationHideComment, ReasonCode: CommentModerationOffTopic,
		CaseState: CommentModerationCaseCommentHidden, CommentState: CommentModeratorHidden,
		CaseVersion: 3, CommentVersion: 4, DecidedAt: now,
	}
	authenticator := &commentAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: reporterID}}}
	authorizer := &commentAuthorizerFixture{}
	repository := &commentModerationRepositoryFixture{receipt: receipt, page: page, result: result}
	service, err := NewCommentModerationService(authenticator, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewCommentModerationService() error = %v", err)
	}

	gotReceipt, err := service.CreateReport(context.Background(), "web-cookie", "csrf-token", CreateCommentReportInput{
		RequestID: uuid.New(), CommentID: commentID, ReasonCode: CommentReportOffTopic,
		Details: "\r\n  偏离资源讨论  \r\n",
	})
	if err != nil || gotReceipt != receipt {
		t.Fatalf("CreateReport() = %+v, %v", gotReceipt, err)
	}
	if repository.reportCommand.ReporterID != reporterID || repository.reportCommand.Details != "偏离资源讨论" ||
		authorizer.requests[0].Action != authz.ActionCommentReportCreateSelf ||
		authorizer.requests[0].CredentialAudience != authz.AudienceWebSession {
		t.Fatalf("report command=%+v authorization=%+v", repository.reportCommand, authorizer.requests[0])
	}

	actor := authz.StaffActor{
		Subject:            authz.Subject{ID: moderatorID, Status: authz.SubjectActive},
		MFAAuthenticatedAt: now.Add(-time.Minute),
	}
	gotPage, err := service.ListOpenCases(context.Background(), actor, 20, 0)
	if err != nil || gotPage.Total != 1 || repository.listLimit != 20 ||
		authorizer.requests[1].Action != authz.ActionSocialReportRead ||
		authorizer.requests[1].CredentialAudience != authz.AudienceStaffSession {
		t.Fatalf("ListOpenCases() page=%+v auth=%+v error=%v", gotPage, authorizer.requests[1], err)
	}

	gotResult, err := service.Decide(context.Background(), actor, DecideCommentModerationCaseInput{
		DecisionID: decisionID, CaseID: caseID, ExpectedCaseVersion: 2, ExpectedCommentVersion: 3,
		Decision: CommentModerationHideComment, ReasonCode: CommentModerationOffTopic,
		Note: "  已核对上下文，正文与资源讨论无关。  ",
	})
	if err != nil || gotResult != result {
		t.Fatalf("Decide() = %+v, %v", gotResult, err)
	}
	if repository.decideCommand.ModeratorID != moderatorID ||
		repository.decideCommand.Note != "已核对上下文，正文与资源讨论无关。" ||
		repository.decideCommand.Authorization.ID == uuid.Nil ||
		authorizer.requests[2].Action != authz.ActionSocialReportResolve {
		t.Fatalf("decision command=%+v authorization=%+v", repository.decideCommand, authorizer.requests[2])
	}
}

func TestCommentModerationServiceRejectsInvalidDecisionPairBeforeAuthorization(t *testing.T) {
	t.Parallel()
	repository := &commentModerationRepositoryFixture{}
	authorizer := &commentAuthorizerFixture{}
	service, err := NewCommentModerationService(&commentAuthenticatorFixture{}, authorizer, repository, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Decide(context.Background(), authz.StaffActor{}, DecideCommentModerationCaseInput{
		DecisionID: uuid.New(), CaseID: uuid.New(), ExpectedCaseVersion: 1, ExpectedCommentVersion: 1,
		Decision: CommentModerationDismiss, ReasonCode: CommentModerationSpam,
		Note: "关闭案件却使用违规原因是不合法的。",
	})
	if !errors.Is(err, ErrCommentReportInput) {
		t.Fatalf("Decide() error = %v", err)
	}
	if len(authorizer.requests) != 0 || repository.decideCommand.DecisionID != uuid.Nil {
		t.Fatal("invalid decision reached authorization or repository")
	}
}
