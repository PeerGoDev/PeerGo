package httpapi

import (
	"context"
	"errors"
	"net/http"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/social"
)

func (h *Handler) CreateCommentReport(ctx context.Context, request generated.CreateCommentReportRequestObject) (generated.CreateCommentReportResponseObject, error) {
	if request.Body == nil {
		return invalidCommentReportResponse(ctx), nil
	}
	details := ""
	if request.Body.Details != nil {
		details = *request.Body.Details
	}
	receipt, err := h.commentModeration.CreateReport(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), social.CreateCommentReportInput{
		RequestID: request.Params.IdempotencyKey, CommentID: request.CommentId,
		ReasonCode: social.CommentReportReasonCode(request.Body.ReasonCode), Details: details,
	})
	switch {
	case errors.Is(err, social.ErrCommentReportInput):
		return invalidCommentReportResponse(ctx), nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后提交举报。")
		return generated.CreateCommentReport401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.CreateCommentReport403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "comment_report_denied", "无法提交举报", "当前账号暂时不能使用评论举报功能。")
		return generated.CreateCommentReport403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentReportSelf):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "comment_report_self_denied", "不能举报自己的评论", "可以编辑或删除自己的评论，无需创建审核案件。")
		return generated.CreateCommentReport403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentReportTargetNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "comment_report_target_not_found", "评论不可举报", "该评论不存在、已删除或所属内容已经停止公开。")
		return generated.CreateCommentReport404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentAlreadyReported):
		problem := newProblemFromContext(ctx, http.StatusConflict, "comment_already_reported", "这条评论已经举报过", "你的举报已在当前审核案件中，无需重复提交。")
		return generated.CreateCommentReport409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentReportIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "idempotency_conflict", "举报请求内容已经变化", "请关闭举报窗口后重新发起。")
		return generated.CreateCommentReport409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.CreateCommentReport201JSONResponse(commentReportReceiptDTO(receipt)), nil
}

func (h *Handler) ListOpenCommentModerationCases(ctx context.Context, request generated.ListOpenCommentModerationCasesRequestObject) (generated.ListOpenCommentModerationCasesResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ListOpenCommentModerationCases401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.ListOpenCommentModerationCases403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	limit := social.DefaultModerationCaseLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	offset := 0
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.commentModeration.ListOpenCases(ctx, staffActor(session), limit, offset)
	switch {
	case errors.Is(err, social.ErrCommentReportInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment_moderation_query", "审核队列参数无效", "每页数量必须在 1 到 50 之间，偏移量必须在 0 到 99999 之间。")
		return generated.ListOpenCommentModerationCases400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "comment_moderation_read_denied", "无法查看评论审核队列", "当前后台身份没有 social.report.read 权限。")
		return generated.ListOpenCommentModerationCases403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListOpenCommentModerationCases200JSONResponse(commentModerationCasePageDTO(page)), nil
}

func (h *Handler) DecideCommentModerationCase(ctx context.Context, request generated.DecideCommentModerationCaseRequestObject) (generated.DecideCommentModerationCaseResponseObject, error) {
	if request.Body == nil {
		return invalidCommentModerationDecisionResponse(ctx), nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, string(request.Params.XCSRFToken))
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.DecideCommentModerationCase401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.DecideCommentModerationCase403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.commentModeration.Decide(ctx, staffActor(session), social.DecideCommentModerationCaseInput{
		DecisionID: request.Params.IdempotencyKey, CaseID: request.CaseId,
		ExpectedCaseVersion: request.Body.ExpectedCaseVersion, ExpectedCommentVersion: request.Body.ExpectedCommentVersion,
		Decision:   social.CommentModerationDecision(request.Body.Decision),
		ReasonCode: social.CommentModerationReasonCode(request.Body.ReasonCode), Note: request.Body.Note,
	})
	switch {
	case errors.Is(err, social.ErrCommentReportInput):
		return invalidCommentModerationDecisionResponse(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "comment_moderation_denied", "无法处置评论举报", "当前后台身份没有 social.report.resolve 权限。")
		return generated.DecideCommentModerationCase403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrModerationConflictOfInterest):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "comment_moderation_conflict_of_interest", "不能处理这个案件", "评论作者或举报人不能审核同一案件，请交由另一名审核员处理。")
		return generated.DecideCommentModerationCase403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrModerationCaseNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "comment_moderation_case_not_found", "审核案件不存在", "目标案件不存在或已不在当前数据集中。")
		return generated.DecideCommentModerationCase404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrModerationDecisionIdempotency):
		problem := newProblemFromContext(ctx, http.StatusConflict, "idempotency_conflict", "处置请求内容已经变化", "请勿复用其他处置决定的 Idempotency-Key。")
		return generated.DecideCommentModerationCase409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrModerationCaseVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "comment_moderation_case_version_conflict", "案件版本已经变化", "请重新载入审核队列并核对最新状态。")
		return generated.DecideCommentModerationCase409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrModerationCommentVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "comment_moderation_comment_version_conflict", "评论内容已经变化", "请重新载入并审核当前版本的评论。")
		return generated.DecideCommentModerationCase409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrModerationCaseStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "comment_moderation_case_state_conflict", "案件已不能按当前决定处理", "案件或评论状态已经变化，请刷新后重新核对。")
		return generated.DecideCommentModerationCase409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.DecideCommentModerationCase201JSONResponse(commentModerationDecisionDTO(result)), nil
}

func invalidCommentReportResponse(ctx context.Context) generated.CreateCommentReport400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment_report", "举报内容无效", "请选择举报类别，补充说明不能超过 500 个字符。")
	return generated.CreateCommentReport400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func invalidCommentModerationDecisionResponse(ctx context.Context) generated.DecideCommentModerationCase400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment_moderation_decision", "处置决定无效", "请核对案件版本、评论版本、决定类别和至少 10 个字符的内部说明。")
	return generated.DecideCommentModerationCase400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func commentReportReceiptDTO(receipt social.CommentReportReceipt) generated.CommentReportReceipt {
	return generated.CommentReportReceipt{
		Id: receipt.ID, CommentId: receipt.CommentID,
		ReasonCode: generated.CommentReportReasonCode(receipt.ReasonCode), CreatedAt: receipt.CreatedAt,
	}
}

func commentModerationCasePageDTO(page social.CommentModerationCasePage) generated.CommentModerationCasePage {
	items := make([]generated.CommentModerationCase, 0, len(page.Items))
	for _, item := range page.Items {
		reports := make([]generated.CommentModerationReport, 0, len(item.Reports))
		for _, report := range item.Reports {
			reports = append(reports, generated.CommentModerationReport{
				ReasonCode: generated.CommentReportReasonCode(report.ReasonCode), Details: report.Details, CreatedAt: report.CreatedAt,
			})
		}
		target := generated.CommentModerationTarget{
			Kind: generated.CommentModerationTargetKind(item.Target.Kind), Title: item.Target.Title,
		}
		switch item.Target.Kind {
		case social.CommentTargetTorrent:
			value := item.Target.TorrentID
			target.TorrentId = &value
		case social.CommentTargetAnnouncement:
			value := item.Target.AnnouncementID
			target.AnnouncementId = &value
		case social.CommentTargetPost:
			value := item.Target.PostPublicID
			target.PostId = &value
		}
		items = append(items, generated.CommentModerationCase{
			Id: item.ID, State: generated.CommentModerationCaseState(item.State), Version: item.Version,
			Target: target, Comment: commentDTO(item.Comment), ReportCount: item.ReportCount, Reports: reports,
			OpenedAt: item.OpenedAt, LatestReportedAt: item.LatestReportedAt,
		})
	}
	return generated.CommentModerationCasePage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}

func commentModerationDecisionDTO(result social.CommentModerationDecisionResult) generated.CommentModerationDecisionResult {
	return generated.CommentModerationDecisionResult{
		DecisionId: result.DecisionID, CaseId: result.CaseID, CommentId: result.CommentID,
		Decision:   generated.CommentModerationDecision(result.Decision),
		ReasonCode: generated.CommentModerationDecisionReasonCode(result.ReasonCode),
		CaseState:  generated.CommentModerationCaseState(result.CaseState), CommentState: generated.CommentState(result.CommentState),
		CaseVersion: result.CaseVersion, CommentVersion: result.CommentVersion, DecidedAt: result.DecidedAt,
	}
}
