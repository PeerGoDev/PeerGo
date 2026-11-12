package social

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type postModerationRepositoryFixture struct {
	receipt       PostReportReceipt
	page          PostModerationCasePage
	result        PostModerationDecisionResult
	reportCommand createPostReportCommand
	listLimit     int
	listOffset    int
	decideCommand decidePostModerationCaseCommand
}

func (fixture *postModerationRepositoryFixture) CreateReport(_ context.Context, command createPostReportCommand) (PostReportReceipt, error) {
	fixture.reportCommand = command
	return fixture.receipt, nil
}

func (fixture *postModerationRepositoryFixture) ListOpenCases(_ context.Context, limit, offset int) (PostModerationCasePage, error) {
	fixture.listLimit, fixture.listOffset = limit, offset
	return fixture.page, nil
}

func (fixture *postModerationRepositoryFixture) Decide(_ context.Context, command decidePostModerationCaseCommand) (PostModerationDecisionResult, error) {
	fixture.decideCommand = command
	return fixture.result, nil
}

func postModerationCaseFixture(now time.Time) PostModerationCase {
	return PostModerationCase{
		ID: uuid.New(), State: PostModerationCaseOpen, Version: 1,
		Post: PostModerationSubject{
			ID: uuid.New(), Author: PostModerationAuthor{ID: uuid.New(), Username: "author", DisplayName: "动态作者"},
			BoardID: "general", BoardName: "生活茶馆", Body: "需要审核的可见动态", State: PostVisible,
			Version: 2, MediaCount: 1, CreatedAt: now.Add(-time.Hour),
		},
		ReportCount: 1,
		Reports:     []PostModerationReport{{ReasonCode: PostReportSpam, Details: "重复刷屏", CreatedAt: now}},
		OpenedAt:    now, LatestReportedAt: now,
	}
}

func TestPostModerationServiceSeparatesMemberReportingFromStaffResolution(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 12, 20, 0, 0, 0, time.UTC)
	reporterID, moderatorID := uuid.New(), uuid.New()
	moderationCase := postModerationCaseFixture(now)
	postID, decisionID := moderationCase.Post.ID, uuid.New()
	receipt := PostReportReceipt{ID: uuid.New(), PostID: postID, ReasonCode: PostReportSpam, CreatedAt: now}
	result := PostModerationDecisionResult{
		DecisionID: decisionID, CaseID: moderationCase.ID, PostID: postID,
		Decision: PostModerationHidePost, ReasonCode: PostModerationSpam,
		CaseState: PostModerationCasePostHidden, PostState: PostModeratorHidden,
		CaseVersion: 2, PostVersion: 3, DecidedAt: now,
	}
	authenticator := &commentAuthenticatorFixture{session: identity.WebSession{User: identity.User{ID: reporterID}}}
	authorizer := &commentAuthorizerFixture{}
	repository := &postModerationRepositoryFixture{
		receipt: receipt, result: result,
		page: PostModerationCasePage{Items: []PostModerationCase{moderationCase}, Total: 1, Limit: 20, Offset: 0},
	}
	service, err := NewPostModerationService(authenticator, authorizer, repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewPostModerationService() error = %v", err)
	}

	requestID := uuid.New()
	gotReceipt, err := service.CreateReport(context.Background(), "web-cookie", "csrf-token", CreatePostReportInput{
		RequestID: requestID, PostID: postID, ReasonCode: PostReportSpam, Details: "\r\n  重复刷屏  \r\n",
	})
	if err != nil || gotReceipt != receipt {
		t.Fatalf("CreateReport() = %+v, %v", gotReceipt, err)
	}
	command := repository.reportCommand
	if command.ReporterID != reporterID || command.RequestID != requestID || command.Details != "重复刷屏" ||
		command.CreateInputHash != postReportInputHash(postID, PostReportSpam, "重复刷屏") ||
		authorizer.requests[0].Action != authz.ActionSocialPostReportCreateSelf ||
		authorizer.requests[0].CredentialAudience != authz.AudienceWebSession {
		t.Fatalf("report command=%+v authorization=%+v", command, authorizer.requests[0])
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

	gotResult, err := service.Decide(context.Background(), actor, DecidePostModerationCaseInput{
		DecisionID: decisionID, CaseID: moderationCase.ID, ExpectedCaseVersion: 1, ExpectedPostVersion: 2,
		Decision: PostModerationHidePost, ReasonCode: PostModerationSpam,
		Note: "  已核对动态内容，确认为重复刷屏。  ",
	})
	if err != nil || gotResult != result {
		t.Fatalf("Decide() = %+v, %v", gotResult, err)
	}
	if repository.decideCommand.ModeratorID != moderatorID ||
		repository.decideCommand.Note != "已核对动态内容，确认为重复刷屏。" ||
		repository.decideCommand.Authorization.ID == uuid.Nil ||
		authorizer.requests[2].Action != authz.ActionSocialReportResolve {
		t.Fatalf("decision command=%+v authorization=%+v", repository.decideCommand, authorizer.requests[2])
	}
}

func TestPostModerationServiceRejectsInvalidInputBeforeAuthorization(t *testing.T) {
	t.Parallel()
	repository := &postModerationRepositoryFixture{}
	authenticator := &commentAuthenticatorFixture{}
	authorizer := &commentAuthorizerFixture{}
	service, err := NewPostModerationService(authenticator, authorizer, repository, time.Now)
	if err != nil {
		t.Fatal(err)
	}

	for name, input := range map[string]CreatePostReportInput{
		"unknown reason":   {RequestID: uuid.New(), PostID: uuid.New(), ReasonCode: "illegal"},
		"details too long": {RequestID: uuid.New(), PostID: uuid.New(), ReasonCode: PostReportOther, Details: strings.Repeat("长", MaxReportDetailsRunes+1)},
		"missing post":     {RequestID: uuid.New(), ReasonCode: PostReportSpam},
	} {
		if _, err := service.CreateReport(context.Background(), "web-cookie", "csrf-token", input); !errors.Is(err, ErrPostReportInput) {
			t.Fatalf("CreateReport(%s) error = %v", name, err)
		}
	}

	for name, input := range map[string]DecidePostModerationCaseInput{
		"dismiss with violation": {Decision: PostModerationDismiss, ReasonCode: PostModerationSpam, Note: "关闭案件却使用违规原因是不合法的。"},
		"hide without violation": {Decision: PostModerationHidePost, ReasonCode: PostModerationNoViolation, Note: "隐藏动态必须给出具体违规原因。"},
		"note too long":          {Decision: PostModerationHidePost, ReasonCode: PostModerationSpam, Note: strings.Repeat("长", MaxPostModerationNoteRunes+1)},
		"note too short":         {Decision: PostModerationHidePost, ReasonCode: PostModerationSpam, Note: "太短"},
	} {
		input.DecisionID, input.CaseID, input.ExpectedCaseVersion, input.ExpectedPostVersion = uuid.New(), uuid.New(), 1, 1
		if _, err := service.Decide(context.Background(), authz.StaffActor{}, input); !errors.Is(err, ErrPostReportInput) {
			t.Fatalf("Decide(%s) error = %v", name, err)
		}
	}
	if len(authorizer.requests) != 0 || repository.reportCommand.RequestID != uuid.Nil || repository.decideCommand.DecisionID != uuid.Nil {
		t.Fatal("invalid input reached authorization or repository")
	}
}

func TestPostModerationServiceRejectsInconsistentCaseProjection(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 12, 20, 0, 0, 0, time.UTC)
	deletedWithBody := postModerationCaseFixture(now)
	deletedWithBody.Post.State = PostAuthorDeleted
	reportBeforeOpening := postModerationCaseFixture(now)
	reportBeforeOpening.Reports[0].CreatedAt = now.Add(-time.Second)
	actor := authz.StaffActor{Subject: authz.Subject{ID: uuid.New(), Status: authz.SubjectActive}, MFAAuthenticatedAt: now}

	for name, item := range map[string]PostModerationCase{
		"author deleted post keeps body": deletedWithBody,
		"report predates its case":       reportBeforeOpening,
	} {
		repository := &postModerationRepositoryFixture{
			page: PostModerationCasePage{Items: []PostModerationCase{item}, Total: 1, Limit: 20, Offset: 0},
		}
		service, err := NewPostModerationService(&commentAuthenticatorFixture{}, &commentAuthorizerFixture{}, repository, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.ListOpenCases(context.Background(), actor, 20, 0); !errors.Is(err, ErrPostModerationInvariant) {
			t.Fatalf("ListOpenCases(%s) error = %v", name, err)
		}
	}
}
