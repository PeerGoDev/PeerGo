package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/ratiowatch"
)

// GetMyRatioWatch returns only the authenticated member's safe assessment
// projection. It deliberately has no path/query user identifier and uses
// no-store because current restriction state can change on a worker tick.
func (handler *Handler) GetMyRatioWatch(ctx context.Context, _ generated.GetMyRatioWatchRequestObject) (generated.GetMyRatioWatchResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请登录后查看自己的分享率考核。")
		return generated.GetMyRatioWatch401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	result, err := handler.ratioWatchAdministration.MyStatus(ctx, cookieToken)
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后查看自己的分享率考核。")
		return generated.GetMyRatioWatch401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "ratio_assessment_read_denied", "无法查看分享率考核", "当前账号不能查看这项记录。")
		return generated.GetMyRatioWatch403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyRatioWatch200JSONResponse{
		Body:    myRatioWatchDTO(result),
		Headers: generated.GetMyRatioWatch200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (handler *Handler) SubmitMyRatioWatchAppeal(ctx context.Context, request generated.SubmitMyRatioWatchAppealRequestObject) (generated.SubmitMyRatioWatchAppealResponseObject, error) {
	if request.Body == nil {
		return ratioAppealSubmitBadRequest(ctx), nil
	}
	result, err := handler.ratioWatchAdministration.SubmitAppeal(
		ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken),
		ratiowatch.SubmitAppealInput{AppealID: request.Params.IdempotencyKey, Statement: request.Body.Statement},
	)
	switch {
	case errors.Is(err, ratiowatch.ErrInput):
		return ratioAppealSubmitBadRequest(ctx), nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后提交分享率申诉。")
		return generated.SubmitMyRatioWatchAppeal401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.SubmitMyRatioWatchAppeal403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "ratio_appeal_create_denied", "无法提交申诉", "当前账号暂时不能提交这项申诉。")
		return generated.SubmitMyRatioWatchAppeal403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, ratiowatch.ErrNoActiveAssessment):
		problem := newProblemFromContext(ctx, http.StatusConflict, "ratio_appeal_no_active_assessment", "当前没有可申诉的考核", "请刷新页面查看最新分享率状态。")
		return generated.SubmitMyRatioWatchAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, ratiowatch.ErrAppealExists):
		problem := newProblemFromContext(ctx, http.StatusConflict, "ratio_appeal_already_exists", "这期考核已提交申诉", "同一期考核只能提交一次，请等待处理。")
		return generated.SubmitMyRatioWatchAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, ratiowatch.ErrConflict), errors.Is(err, ratiowatch.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "ratio_appeal_conflict", "申诉状态已变化", "请刷新页面后重试。")
		return generated.SubmitMyRatioWatchAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.SubmitMyRatioWatchAppeal201JSONResponse{
		Body:    myRatioAppealDTO(result),
		Headers: generated.SubmitMyRatioWatchAppeal201ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (handler *Handler) ListRatioWatchPolicyRevisions(ctx context.Context, request generated.ListRatioWatchPolicyRevisionsRequestObject) (generated.ListRatioWatchPolicyRevisionsResponseObject, error) {
	session, problem, err := handler.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListRatioWatchPolicyRevisions401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListRatioWatchPolicyRevisions403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit, offset := ratiowatch.DefaultListLimit, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := handler.ratioWatchAdministration.Policies(ctx, staffActor(session), limit, offset)
	switch {
	case errors.Is(err, ratiowatch.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_ratio_policy_query", "分享率规则查询无效", "请检查分页参数。")
		return generated.ListRatioWatchPolicyRevisions400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "ratio_policy_read_denied", "无法查看分享率规则", "当前后台身份没有 ratio.policy.read 权限。")
		return generated.ListRatioWatchPolicyRevisions403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.RatioWatchPolicyRevision, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, ratioPolicyRevisionDTO(item))
	}
	var current *generated.RatioWatchPolicyRevision
	if page.Current != nil {
		value := ratioPolicyRevisionDTO(*page.Current)
		current = &value
	}
	return generated.ListRatioWatchPolicyRevisions200JSONResponse{
		Items: items, Total: countText(page.Total), Limit: page.Limit, Offset: page.Offset,
		MinimumEffectiveFrom: page.MinimumEffectiveFrom, Current: current,
		Summary: generated.RatioWatchAssessmentSummary{
			Watching: countText(page.Summary.Watching), Warning: countText(page.Summary.Warning),
			DownloadRestricted: countText(page.Summary.DownloadRestricted),
			Satisfied:          countText(page.Summary.Satisfied), ManuallyCleared: countText(page.Summary.ManuallyCleared),
			VipExempted: countText(page.Summary.VIPExempted),
		},
		Worker: ratioWorkerStateDTO(page.Worker),
	}, nil
}

func (handler *Handler) PreviewRatioWatchPolicy(ctx context.Context, request generated.PreviewRatioWatchPolicyRequestObject) (generated.PreviewRatioWatchPolicyResponseObject, error) {
	if request.Body == nil {
		return ratioPolicyPreviewBadRequest(ctx), nil
	}
	session, problem, err := handler.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.PreviewRatioWatchPolicy401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.PreviewRatioWatchPolicy403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	policy, err := ratioPolicyInputFromDTO(*request.Body)
	if err != nil {
		return ratioPolicyPreviewBadRequest(ctx), nil
	}
	preview, err := handler.ratioWatchAdministration.Preview(ctx, staffActor(session), policy)
	switch {
	case errors.Is(err, ratiowatch.ErrInput):
		return ratioPolicyPreviewBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "ratio_policy_preview_denied", "无法预览分享率规则", "当前后台身份没有 ratio.policy.read 权限。")
		return generated.PreviewRatioWatchPolicy403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.PreviewRatioWatchPolicy200JSONResponse{
		Policy: ratioPolicyInputDTO(preview.Policy), EligibleUsers: countText(preview.EligibleUsers),
		WouldEnterWatch:         countText(preview.WouldEnterWatch),
		WouldRestrictAtDeadline: countText(preview.WouldRestrictAtDeadline),
		VipExemptUsers:          countText(preview.VIPExemptUsers),
		LegacyRestrictedUsers:   countText(preview.LegacyRestrictedUsers),
	}, nil
}

func (handler *Handler) IssueRatioWatchPolicyRevision(ctx context.Context, request generated.IssueRatioWatchPolicyRevisionRequestObject) (generated.IssueRatioWatchPolicyRevisionResponseObject, error) {
	if request.Body == nil {
		return ratioPolicyIssueBadRequest(ctx), nil
	}
	session, problem, err := handler.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.IssueRatioWatchPolicyRevision401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.IssueRatioWatchPolicyRevision403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	policy, err := ratioPolicyInputFromDTO(request.Body.Policy)
	if err != nil {
		return ratioPolicyIssueBadRequest(ctx), nil
	}
	revision, err := handler.ratioWatchAdministration.Issue(ctx, staffActor(session), ratiowatch.IssueInput{
		RevisionID: request.Params.IdempotencyKey, Policy: policy,
		EffectiveAt: request.Body.EffectiveAt, Reason: request.Body.Reason,
	})
	switch {
	case errors.Is(err, ratiowatch.ErrInput):
		return ratioPolicyIssueBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "ratio_policy_issue_denied", "无法签发分享率规则", "当前后台身份没有 ratio.policy.issue 权限。")
		return generated.IssueRatioWatchPolicyRevision403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, ratiowatch.ErrNoChange):
		problem := newProblemFromContext(ctx, http.StatusConflict, "ratio_policy_unchanged", "规则没有变化", "请修改至少一项设置后再签发新版本。")
		return generated.IssueRatioWatchPolicyRevision409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, ratiowatch.ErrConflict), errors.Is(err, ratiowatch.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "ratio_policy_conflict", "分享率规则时间线已变化", "请刷新当前版本并重新预览。")
		return generated.IssueRatioWatchPolicyRevision409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.IssueRatioWatchPolicyRevision201JSONResponse(ratioPolicyRevisionDTO(revision)), nil
}

func (handler *Handler) ListRatioWatchAssessments(ctx context.Context, request generated.ListRatioWatchAssessmentsRequestObject) (generated.ListRatioWatchAssessmentsResponseObject, error) {
	session, problem, err := handler.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListRatioWatchAssessments401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListRatioWatchAssessments403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	query := ratiowatch.AssessmentQuery{Filter: ratiowatch.AssessmentFilterActive, Limit: 30}
	if request.Params.Q != nil {
		query.Query = *request.Params.Q
	}
	if request.Params.Filter != nil {
		query.Filter = ratiowatch.AssessmentFilter(*request.Params.Filter)
	}
	if request.Params.Limit != nil {
		query.Limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		query.Offset = *request.Params.Offset
	}
	page, err := handler.ratioWatchAdministration.Assessments(ctx, staffActor(session), query)
	switch {
	case errors.Is(err, ratiowatch.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_ratio_assessment_query", "分享率考核查询无效", "请检查筛选与分页参数。")
		return generated.ListRatioWatchAssessments400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "ratio_assessment_read_denied", "无法查看分享率考核", "当前后台身份没有 ratio.policy.read 权限。")
		return generated.ListRatioWatchAssessments403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.RatioWatchAssessment, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, ratioAssessmentDTO(item))
	}
	return generated.ListRatioWatchAssessments200JSONResponse{
		Items: items, Total: countText(page.Total), Limit: page.Limit, Offset: page.Offset,
	}, nil
}

func (handler *Handler) ClearRatioWatchAssessment(ctx context.Context, request generated.ClearRatioWatchAssessmentRequestObject) (generated.ClearRatioWatchAssessmentResponseObject, error) {
	if request.Body == nil {
		return ratioAssessmentClearBadRequest(ctx), nil
	}
	session, problem, err := handler.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ClearRatioWatchAssessment401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ClearRatioWatchAssessment403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	result, err := handler.ratioWatchAdministration.Clear(ctx, staffActor(session), ratiowatch.ClearInput{
		AssessmentID: request.AssessmentId, ExpectedVersion: request.Body.ExpectedVersion,
		Reason: request.Body.Reason,
	})
	switch {
	case errors.Is(err, ratiowatch.ErrInput):
		return ratioAssessmentClearBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, ratiowatch.ErrSelfTarget):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "ratio_assessment_clear_denied", "无法解除分享率考核", "当前后台身份无权处置该考核。")
		return generated.ClearRatioWatchAssessment403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, ratiowatch.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "ratio_assessment_not_found", "考核不存在", "该分享率考核不存在。")
		return generated.ClearRatioWatchAssessment404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, ratiowatch.ErrNotActive), errors.Is(err, ratiowatch.ErrConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "ratio_assessment_conflict", "考核状态已变化", "请刷新列表后重新确认。")
		return generated.ClearRatioWatchAssessment409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ClearRatioWatchAssessment200JSONResponse(ratioAssessmentDTO(result)), nil
}

func (handler *Handler) ListRatioWatchAppeals(ctx context.Context, request generated.ListRatioWatchAppealsRequestObject) (generated.ListRatioWatchAppealsResponseObject, error) {
	session, problem, err := handler.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListRatioWatchAppeals401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListRatioWatchAppeals403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	query := ratiowatch.AppealQuery{Filter: ratiowatch.AppealFilterPending, Limit: 30}
	if request.Params.Q != nil {
		query.Query = *request.Params.Q
	}
	if request.Params.Filter != nil {
		query.Filter = ratiowatch.AppealFilter(*request.Params.Filter)
	}
	if request.Params.Limit != nil {
		query.Limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		query.Offset = *request.Params.Offset
	}
	page, err := handler.ratioWatchAdministration.Appeals(ctx, staffActor(session), query)
	switch {
	case errors.Is(err, ratiowatch.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_ratio_appeal_query", "分享率申诉查询无效", "请检查筛选与分页参数。")
		return generated.ListRatioWatchAppeals400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "ratio_appeal_read_denied", "无法查看分享率申诉", "当前后台身份没有 ratio.policy.read 权限。")
		return generated.ListRatioWatchAppeals403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.RatioWatchAppeal, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, ratioAppealDTO(item))
	}
	return generated.ListRatioWatchAppeals200JSONResponse{
		Items: items, Total: countText(page.Total), Limit: page.Limit, Offset: page.Offset,
	}, nil
}

func (handler *Handler) DecideRatioWatchAppeal(ctx context.Context, request generated.DecideRatioWatchAppealRequestObject) (generated.DecideRatioWatchAppealResponseObject, error) {
	if request.Body == nil {
		return ratioAppealDecisionBadRequest(ctx), nil
	}
	session, problem, err := handler.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.DecideRatioWatchAppeal401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.DecideRatioWatchAppeal403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	result, err := handler.ratioWatchAdministration.DecideAppeal(ctx, staffActor(session), ratiowatch.DecideAppealInput{
		AppealID: request.AppealId, Decision: ratiowatch.AppealDecision(request.Body.Decision),
		ExpectedAssessmentVersion: request.Body.ExpectedAssessmentVersion,
		Response:                  request.Body.Response,
	})
	switch {
	case errors.Is(err, ratiowatch.ErrInput):
		return ratioAppealDecisionBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, ratiowatch.ErrSelfTarget):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "ratio_appeal_decision_denied", "无法处理分享率申诉", "当前后台身份无权处置该申诉。")
		return generated.DecideRatioWatchAppeal403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, ratiowatch.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "ratio_appeal_not_found", "申诉不存在", "该分享率申诉不存在。")
		return generated.DecideRatioWatchAppeal404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, ratiowatch.ErrAppealResolved), errors.Is(err, ratiowatch.ErrNotActive), errors.Is(err, ratiowatch.ErrConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "ratio_appeal_decision_conflict", "申诉或考核状态已变化", "请刷新列表后重新确认。")
		return generated.DecideRatioWatchAppeal409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.DecideRatioWatchAppeal200JSONResponse(ratioAppealDTO(result)), nil
}

func ratioPolicyInputFromDTO(input generated.RatioWatchPolicyInput) (ratiowatch.PolicyInput, error) {
	threshold, err := strconv.ParseInt(string(input.DownloadThresholdBytes), 10, 64)
	if err != nil {
		return ratiowatch.PolicyInput{}, ratiowatch.ErrInput
	}
	return ratiowatch.PolicyInput{
		Enabled: input.Enabled, DownloadThresholdBytes: threshold,
		MinimumRatioBasisPoints:     input.MinimumRatioBasisPoints,
		WatchPeriodSeconds:          input.WatchPeriodSeconds,
		RestrictionRatioBasisPoints: input.RestrictionRatioBasisPoints,
	}, nil
}

func ratioPolicyInputDTO(input ratiowatch.PolicyInput) generated.RatioWatchPolicyInput {
	return generated.RatioWatchPolicyInput{
		Enabled:                     input.Enabled,
		DownloadThresholdBytes:      generated.TrafficByteCount(strconv.FormatInt(input.DownloadThresholdBytes, 10)),
		MinimumRatioBasisPoints:     input.MinimumRatioBasisPoints,
		WatchPeriodSeconds:          input.WatchPeriodSeconds,
		RestrictionRatioBasisPoints: input.RestrictionRatioBasisPoints,
	}
}

func myRatioWatchDTO(input ratiowatch.MyStatus) generated.MyRatioWatch {
	result := generated.MyRatioWatch{
		ObservedAt: input.ObservedAt,
		CreditedUploadedBytes: generated.TrafficByteCount(
			strconv.FormatInt(input.CreditedUploaded, 10),
		),
		ChargedDownloadedBytes: generated.TrafficByteCount(
			strconv.FormatInt(input.ChargedDownloaded, 10),
		),
		CurrentRatioBasisPoints: input.CurrentRatioBasisPoints,
		VipActive:               input.VIPActive,
		DownloadRestricted:      input.DownloadRestricted,
		RestrictionSource:       generated.MyRatioWatchRestrictionSource(input.RestrictionSource),
		ThresholdReached:        input.ThresholdReached,
		MinimumRatioReached:     input.MinimumRatioReached,
		RecoveryUploadedBytes: generated.TrafficByteCount(
			strconv.FormatInt(input.RecoveryUploadedBytes, 10),
		),
		CanAppeal: input.CanAppeal,
	}
	if input.Policy != nil {
		result.Policy = &generated.MyRatioWatchPolicy{
			RuleVersion:                 input.Policy.RuleVersion,
			Enabled:                     input.Policy.Enabled,
			DownloadThresholdBytes:      generated.TrafficByteCount(strconv.FormatInt(input.Policy.DownloadThresholdBytes, 10)),
			MinimumRatioBasisPoints:     input.Policy.MinimumRatioBasisPoints,
			WatchPeriodSeconds:          input.Policy.WatchPeriodSeconds,
			RestrictionRatioBasisPoints: input.Policy.RestrictionRatioBasisPoints,
			VipExempt:                   input.Policy.VIPExempt,
			EffectiveAt:                 input.Policy.EffectiveAt,
			BoundToAssessment:           input.Policy.BoundToAssessment,
		}
	}
	if input.Assessment != nil {
		result.Assessment = &generated.MyRatioWatchAssessment{
			Status:               generated.MyRatioWatchAssessmentStatus(input.Assessment.Status),
			StartedAt:            input.Assessment.StartedAt,
			DeadlineAt:           input.Assessment.DeadlineAt,
			RestrictionStartedAt: input.Assessment.RestrictionStartedAt,
			UpdatedAt:            input.Assessment.UpdatedAt,
		}
	}
	if input.Appeal != nil {
		value := myRatioAppealFromProjectionDTO(*input.Appeal)
		result.Appeal = &value
	}
	return result
}

func myRatioAppealDTO(input ratiowatch.Appeal) generated.MyRatioWatchAppeal {
	return generated.MyRatioWatchAppeal{
		Status: generated.RatioWatchAppealStatus(input.Status), Statement: input.Statement,
		SubmittedAt: input.CreatedAt, ResolvedAt: input.ResolvedAt, Response: input.Response,
	}
}

func myRatioAppealFromProjectionDTO(input ratiowatch.MyAppeal) generated.MyRatioWatchAppeal {
	return generated.MyRatioWatchAppeal{
		Status: generated.RatioWatchAppealStatus(input.Status), Statement: input.Statement,
		SubmittedAt: input.SubmittedAt, ResolvedAt: input.ResolvedAt, Response: input.Response,
	}
}

func ratioAppealDTO(input ratiowatch.Appeal) generated.RatioWatchAppeal {
	return generated.RatioWatchAppeal{
		Id: input.ID, AssessmentId: input.AssessmentID, UserId: input.UserID,
		UserNumericId: input.UserNumericID, Username: input.Username,
		Statement: input.Statement, CreatedAt: input.CreatedAt,
		Status: generated.RatioWatchAppealStatus(input.Status), Response: input.Response,
		ResolvedAt: input.ResolvedAt, AssessmentStatus: generated.RatioWatchAssessmentStatus(input.AssessmentStatus),
		AssessmentVersion:             input.AssessmentVersion,
		CurrentCreditedUploadedBytes:  generated.TrafficByteCount(strconv.FormatInt(input.CurrentCreditedUploaded, 10)),
		CurrentChargedDownloadedBytes: generated.TrafficByteCount(strconv.FormatInt(input.CurrentChargedDownloaded, 10)),
		CurrentRatioBasisPoints:       input.CurrentRatioBasisPoints, DeadlineAt: input.DeadlineAt,
		RestrictionStartedAt:     input.RestrictionStartedAt,
		LegacyDownloadRestricted: input.LegacyDownloadRestricted, Replayed: input.Replayed,
	}
}

func ratioAppealSubmitBadRequest(ctx context.Context) generated.SubmitMyRatioWatchAppeal400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_ratio_appeal", "申诉内容无效", "请用 20 到 1000 个字说明情况和申诉理由。")
	return generated.SubmitMyRatioWatchAppeal400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func ratioAppealDecisionBadRequest(ctx context.Context) generated.DecideRatioWatchAppeal400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_ratio_appeal_decision", "申诉处理内容无效", "请选择批准或驳回，并填写 10 到 1000 个字的处理意见。")
	return generated.DecideRatioWatchAppeal400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func ratioPolicyRevisionDTO(input ratiowatch.PolicyRevision) generated.RatioWatchPolicyRevision {
	policy := ratioPolicyInputDTO(input.PolicyInput)
	return generated.RatioWatchPolicyRevision{
		Id: input.ID, RuleId: input.RuleID, RuleVersion: input.RuleVersion,
		Enabled: policy.Enabled, DownloadThresholdBytes: policy.DownloadThresholdBytes,
		MinimumRatioBasisPoints:     policy.MinimumRatioBasisPoints,
		WatchPeriodSeconds:          policy.WatchPeriodSeconds,
		RestrictionRatioBasisPoints: policy.RestrictionRatioBasisPoints,
		VipExempt:                   input.VIPExempt, EffectiveAt: input.EffectiveAt, Reason: input.Reason,
		CreatedAt:     input.CreatedAt,
		TimelineState: generated.RatioWatchPolicyRevisionTimelineState(input.TimelineState),
		Replayed:      input.Replayed,
	}
}

func ratioAssessmentDTO(input ratiowatch.Assessment) generated.RatioWatchAssessment {
	return generated.RatioWatchAssessment{
		Id: input.ID, UserId: input.UserID, UserNumericId: input.UserNumericID,
		Username: input.Username, PolicyRevisionId: input.PolicyRevisionID,
		PolicyVersion: input.PolicyVersion, Status: generated.RatioWatchAssessmentStatus(input.Status),
		StartedAt: input.StartedAt, DeadlineAt: input.DeadlineAt,
		OpeningCreditedUploadedBytes:  generated.TrafficByteCount(strconv.FormatInt(input.OpeningCreditedUploaded, 10)),
		OpeningChargedDownloadedBytes: generated.TrafficByteCount(strconv.FormatInt(input.OpeningChargedDownloaded, 10)),
		OpeningRatioBasisPoints:       input.OpeningRatioBasisPoints,
		CurrentCreditedUploadedBytes:  generated.TrafficByteCount(strconv.FormatInt(input.CurrentCreditedUploaded, 10)),
		CurrentChargedDownloadedBytes: generated.TrafficByteCount(strconv.FormatInt(input.CurrentChargedDownloaded, 10)),
		CurrentRatioBasisPoints:       input.CurrentRatioBasisPoints,
		RestrictionStartedAt:          input.RestrictionStartedAt, ResolvedAt: input.ResolvedAt,
		ResolutionCode: input.ResolutionCode, ResolutionReason: input.ResolutionReason,
		Version: input.Version, UpdatedAt: input.UpdatedAt,
		LegacyDownloadRestricted: input.LegacyDownloadRestricted,
	}
}

func ratioWorkerStateDTO(input ratiowatch.WorkerState) generated.RatioWatchWorkerState {
	return generated.RatioWatchWorkerState{
		LastStartedAt: input.LastStartedAt, LastCompletedAt: input.LastCompletedAt,
		LastErrorCode: input.LastErrorCode, LastExamined: countText(input.LastExamined),
		LastCreated: countText(input.LastCreated), LastTransitioned: countText(input.LastTransitioned),
		RunCount: countText(input.RunCount),
	}
}

func countText(value int64) generated.OperationsCount {
	return generated.OperationsCount(strconv.FormatInt(value, 10))
}

func ratioPolicyPreviewBadRequest(ctx context.Context) generated.PreviewRatioWatchPolicyResponseObject {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_ratio_policy", "分享率规则参数无效", "请检查下载量阈值、两级分享率阈值和观察天数。")
	return generated.PreviewRatioWatchPolicy400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func ratioPolicyIssueBadRequest(ctx context.Context) generated.IssueRatioWatchPolicyRevisionResponseObject {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_ratio_policy", "分享率规则参数无效", "请检查规则、生效时间和调整原因。")
	return generated.IssueRatioWatchPolicyRevision400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func ratioAssessmentClearBadRequest(ctx context.Context) generated.ClearRatioWatchAssessmentResponseObject {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_ratio_assessment_clear", "解除考核参数无效", "请填写至少 10 个字符的原因并刷新考核版本。")
	return generated.ClearRatioWatchAssessment400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}
