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

func (h *Handler) CreateSocialPostReport(ctx context.Context, request generated.CreateSocialPostReportRequestObject) (generated.CreateSocialPostReportResponseObject, error) {
	if h.postModeration == nil {
		return generated.CreateSocialPostReportdefaultApplicationProblemPlusJSONResponse{Body: postModerationUnavailableProblem(ctx), StatusCode: http.StatusServiceUnavailable}, nil
	}
	if request.Body == nil {
		return invalidSocialPostReportResponse(ctx), nil
	}
	details := ""
	if request.Body.Details != nil {
		details = *request.Body.Details
	}
	receipt, err := h.postModeration.CreateReport(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), social.CreatePostReportInput{
		RequestID: request.Params.IdempotencyKey, PostID: request.PostId,
		ReasonCode: social.PostReportReasonCode(request.Body.ReasonCode), Details: details,
	})
	switch {
	case errors.Is(err, social.ErrPostReportInput):
		return invalidSocialPostReportResponse(ctx), nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后提交举报。")
		return generated.CreateSocialPostReport401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.CreateSocialPostReport403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "social_post_report_denied", "无法提交举报", "当前账号暂时不能使用动态举报功能。")
		return generated.CreateSocialPostReport403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostReportSelf):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "social_post_report_self_denied", "不能举报自己的动态", "可以编辑或删除自己的动态，无需创建审核案件。")
		return generated.CreateSocialPostReport403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostReportTargetNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "social_post_report_target_not_found", "动态不可举报", "该动态不存在、已删除或已经停止公开。")
		return generated.CreateSocialPostReport404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostAlreadyReported):
		problem := newProblemFromContext(ctx, http.StatusConflict, "social_post_already_reported", "这条动态已经举报过", "你的举报已在当前审核案件中，无需重复提交。")
		return generated.CreateSocialPostReport409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostReportIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "idempotency_conflict", "举报请求内容已经变化", "请关闭举报窗口后重新发起。")
		return generated.CreateSocialPostReport409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.CreateSocialPostReport201JSONResponse(socialPostReportReceiptDTO(receipt)), nil
}

func (h *Handler) ListOpenSocialPostModerationCases(ctx context.Context, request generated.ListOpenSocialPostModerationCasesRequestObject) (generated.ListOpenSocialPostModerationCasesResponseObject, error) {
	if h.postModeration == nil {
		return generated.ListOpenSocialPostModerationCasesdefaultApplicationProblemPlusJSONResponse{Body: postModerationUnavailableProblem(ctx), StatusCode: http.StatusServiceUnavailable}, nil
	}
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ListOpenSocialPostModerationCases401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.ListOpenSocialPostModerationCases403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	limit := social.DefaultModerationCaseLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	offset := 0
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.postModeration.ListOpenCases(ctx, staffActor(session), limit, offset)
	switch {
	case errors.Is(err, social.ErrPostReportInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_social_post_moderation_query", "审核队列参数无效", "每页数量必须在 1 到 50 之间，偏移量必须在 0 到 99999 之间。")
		return generated.ListOpenSocialPostModerationCases400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "social_post_moderation_read_denied", "无法查看动态审核队列", "当前后台身份没有 social.report.read 权限。")
		return generated.ListOpenSocialPostModerationCases403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListOpenSocialPostModerationCases200JSONResponse(socialPostModerationCasePageDTO(page)), nil
}

func (h *Handler) DecideSocialPostModerationCase(ctx context.Context, request generated.DecideSocialPostModerationCaseRequestObject) (generated.DecideSocialPostModerationCaseResponseObject, error) {
	if h.postModeration == nil {
		return generated.DecideSocialPostModerationCasedefaultApplicationProblemPlusJSONResponse{Body: postModerationUnavailableProblem(ctx), StatusCode: http.StatusServiceUnavailable}, nil
	}
	if request.Body == nil {
		return invalidSocialPostModerationDecisionResponse(ctx), nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, string(request.Params.XCSRFToken))
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.DecideSocialPostModerationCase401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.DecideSocialPostModerationCase403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.postModeration.Decide(ctx, staffActor(session), social.DecidePostModerationCaseInput{
		DecisionID: request.Params.IdempotencyKey, CaseID: request.CaseId,
		ExpectedCaseVersion: request.Body.ExpectedCaseVersion, ExpectedPostVersion: request.Body.ExpectedPostVersion,
		Decision:   social.PostModerationDecision(request.Body.Decision),
		ReasonCode: social.PostModerationReasonCode(request.Body.ReasonCode), Note: request.Body.Note,
	})
	switch {
	case errors.Is(err, social.ErrPostReportInput):
		return invalidSocialPostModerationDecisionResponse(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "social_post_moderation_denied", "无法处置动态举报", "当前后台身份没有 social.report.resolve 权限。")
		return generated.DecideSocialPostModerationCase403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostModerationConflictOfInterest):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "social_post_moderation_conflict_of_interest", "不能处理这个案件", "动态作者或举报人不能审核同一案件，请交由另一名审核员处理。")
		return generated.DecideSocialPostModerationCase403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostModerationCaseNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "social_post_moderation_case_not_found", "审核案件不存在", "目标案件不存在或已不在当前数据集中。")
		return generated.DecideSocialPostModerationCase404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostModerationDecisionIdempotency):
		problem := newProblemFromContext(ctx, http.StatusConflict, "idempotency_conflict", "处置请求内容已经变化", "请勿复用其他处置决定的 Idempotency-Key。")
		return generated.DecideSocialPostModerationCase409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostModerationCaseVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "social_post_moderation_case_version_conflict", "案件版本已经变化", "请重新载入审核队列并核对最新状态。")
		return generated.DecideSocialPostModerationCase409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostModerationPostVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "social_post_moderation_post_version_conflict", "动态内容已经变化", "请重新载入并审核当前版本的动态。")
		return generated.DecideSocialPostModerationCase409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostModerationCaseStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "social_post_moderation_case_state_conflict", "案件已不能按当前决定处理", "案件或动态状态已经变化，请刷新后重新核对。")
		return generated.DecideSocialPostModerationCase409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.DecideSocialPostModerationCase201JSONResponse(socialPostModerationDecisionDTO(result)), nil
}

func postModerationUnavailableProblem(ctx context.Context) generated.Problem {
	return newProblemFromContext(ctx, http.StatusServiceUnavailable, "social_post_moderation_unavailable", "动态举报暂不可用", "请稍后重试。")
}

func invalidSocialPostReportResponse(ctx context.Context) generated.CreateSocialPostReport400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_social_post_report", "举报内容无效", "请选择举报类别，补充说明不能超过 500 个字符。")
	return generated.CreateSocialPostReport400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func invalidSocialPostModerationDecisionResponse(ctx context.Context) generated.DecideSocialPostModerationCase400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_social_post_moderation_decision", "处置决定无效", "请核对案件版本、动态版本、决定类别和 10 到 500 个字符的内部说明。")
	return generated.DecideSocialPostModerationCase400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func socialPostReportReceiptDTO(receipt social.PostReportReceipt) generated.SocialPostReportReceipt {
	return generated.SocialPostReportReceipt{
		Id: receipt.ID, PostId: receipt.PostID,
		ReasonCode: generated.SocialPostReportReasonCode(receipt.ReasonCode), CreatedAt: receipt.CreatedAt,
	}
}

func socialPostModerationCasePageDTO(page social.PostModerationCasePage) generated.SocialPostModerationCasePage {
	items := make([]generated.SocialPostModerationCase, 0, len(page.Items))
	for _, item := range page.Items {
		reports := make([]generated.SocialPostModerationReport, 0, len(item.Reports))
		for _, report := range item.Reports {
			reports = append(reports, generated.SocialPostModerationReport{
				ReasonCode: generated.SocialPostReportReasonCode(report.ReasonCode), Details: report.Details, CreatedAt: report.CreatedAt,
			})
		}
		items = append(items, generated.SocialPostModerationCase{
			Id: item.ID, State: generated.SocialPostModerationCaseState(item.State), Version: item.Version,
			Post: generated.SocialPostModerationSubject{
				Id: item.Post.ID,
				Author: generated.SocialPostModerationAuthor{
					Id: item.Post.Author.ID, Username: item.Post.Author.Username, DisplayName: item.Post.Author.DisplayName,
				},
				BoardId: item.Post.BoardID, BoardName: item.Post.BoardName, Content: item.Post.Body,
				State: generated.SocialPostModerationPostState(item.Post.State), Version: item.Post.Version,
				MediaCount: item.Post.MediaCount, CreatedAt: item.Post.CreatedAt, EditedAt: item.Post.EditedAt,
			},
			ReportCount: item.ReportCount, Reports: reports,
			OpenedAt: item.OpenedAt, LatestReportedAt: item.LatestReportedAt,
		})
	}
	return generated.SocialPostModerationCasePage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}

func socialPostModerationDecisionDTO(result social.PostModerationDecisionResult) generated.SocialPostModerationDecisionResult {
	return generated.SocialPostModerationDecisionResult{
		DecisionId: result.DecisionID, CaseId: result.CaseID, PostId: result.PostID,
		Decision:    generated.SocialPostModerationDecision(result.Decision),
		ReasonCode:  generated.SocialPostModerationDecisionReasonCode(result.ReasonCode),
		CaseState:   generated.SocialPostModerationCaseState(result.CaseState),
		PostState:   generated.SocialPostModerationPostState(result.PostState),
		CaseVersion: result.CaseVersion, PostVersion: result.PostVersion, DecidedAt: result.DecidedAt,
	}
}
