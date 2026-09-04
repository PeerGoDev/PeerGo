package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/newcomer"
)

// GetMyNewcomerAssessment exposes only the authenticated member's safe
// projection. Assessment and policy UUIDs stay inside the staff boundary.
func (handler *Handler) GetMyNewcomerAssessment(ctx context.Context, _ generated.GetMyNewcomerAssessmentRequestObject) (generated.GetMyNewcomerAssessmentResponseObject, error) {
	cookieToken := sessionTokenFromContext(ctx)
	if cookieToken == "" {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请登录后查看自己的新人考核。")
		return generated.GetMyNewcomerAssessment401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	result, err := handler.newcomerAdministration.MyStatus(ctx, cookieToken)
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "会话已经失效", "请重新登录后查看自己的新人考核。")
		return generated.GetMyNewcomerAssessment401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "newcomer_assessment_read_denied", "无法查看新人考核", "当前账号不能查看这项记录。")
		return generated.GetMyNewcomerAssessment403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyNewcomerAssessment200JSONResponse{
		Body:    myNewcomerStatusDTO(result),
		Headers: generated.GetMyNewcomerAssessment200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (handler *Handler) ListNewcomerPolicyRevisions(ctx context.Context, request generated.ListNewcomerPolicyRevisionsRequestObject) (generated.ListNewcomerPolicyRevisionsResponseObject, error) {
	session, problem, err := handler.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListNewcomerPolicyRevisions401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListNewcomerPolicyRevisions403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit, offset := 20, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := handler.newcomerAdministration.Policies(ctx, staffActor(session), limit, offset)
	switch {
	case errors.Is(err, newcomer.ErrInput):
		return newcomerPolicyListBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "newcomer_policy_read_denied", "无法查看新人考核规则", "当前后台身份没有 newcomer.policy.read 权限。")
		return generated.ListNewcomerPolicyRevisions403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.NewcomerPolicyRevision, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, newcomerPolicyDTO(item))
	}
	var current *generated.NewcomerPolicyRevision
	if page.Current != nil {
		value := newcomerPolicyDTO(*page.Current)
		current = &value
	}
	return generated.ListNewcomerPolicyRevisions200JSONResponse{
		Items: items, Total: countText(page.Total), Limit: page.Limit, Offset: page.Offset,
		MinimumEffectiveFrom: page.MinimumEffectiveFrom, Current: current,
		Summary: generated.NewcomerAssessmentSummary{
			Active:             countText(page.Summary.Active),
			DownloadRestricted: countText(page.Summary.DownloadRestricted),
			Passed:             countText(page.Summary.Passed), Exempted: countText(page.Summary.Exempted),
		},
		Worker: generated.NewcomerWorkerState{
			LastStartedAt: page.Worker.LastStartedAt, LastCompletedAt: page.Worker.LastCompletedAt,
			LastErrorCode: page.Worker.LastErrorCode, LastExamined: countText(page.Worker.LastExamined),
			LastTransitioned: countText(page.Worker.LastTransitioned), RunCount: countText(page.Worker.RunCount),
		},
	}, nil
}

func (handler *Handler) IssueNewcomerPolicyRevision(ctx context.Context, request generated.IssueNewcomerPolicyRevisionRequestObject) (generated.IssueNewcomerPolicyRevisionResponseObject, error) {
	if request.Body == nil {
		return newcomerPolicyIssueBadRequest(ctx), nil
	}
	session, problem, err := handler.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.IssueNewcomerPolicyRevision401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.IssueNewcomerPolicyRevision403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	policy, err := newcomerPolicyFromDTO(request.Body.Policy)
	if err != nil {
		return newcomerPolicyIssueBadRequest(ctx), nil
	}
	result, err := handler.newcomerAdministration.Issue(ctx, staffActor(session), newcomer.IssueInput{
		RequestID: request.Params.IdempotencyKey, Policy: policy,
		EffectiveAt: request.Body.EffectiveAt, Reason: request.Body.Reason,
	})
	switch {
	case errors.Is(err, newcomer.ErrInput):
		return newcomerPolicyIssueBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "newcomer_policy_issue_denied", "无法签发新人考核规则", "当前后台身份没有 newcomer.policy.issue 权限。")
		return generated.IssueNewcomerPolicyRevision403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, newcomer.ErrNoChange):
		problem := newProblemFromContext(ctx, http.StatusConflict, "newcomer_policy_unchanged", "规则没有变化", "请修改至少一项设置后再签发新版本。")
		return generated.IssueNewcomerPolicyRevision409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, newcomer.ErrConflict), errors.Is(err, newcomer.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "newcomer_policy_conflict", "新人考核规则已变化", "请刷新当前版本后重新签发。")
		return generated.IssueNewcomerPolicyRevision409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.IssueNewcomerPolicyRevision201JSONResponse(newcomerPolicyDTO(result)), nil
}

func (handler *Handler) ListNewcomerAssessments(ctx context.Context, request generated.ListNewcomerAssessmentsRequestObject) (generated.ListNewcomerAssessmentsResponseObject, error) {
	session, problem, err := handler.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListNewcomerAssessments401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListNewcomerAssessments403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	query := newcomer.AssessmentQuery{Filter: newcomer.AssessmentFilterActive, Limit: newcomer.DefaultListLimit}
	if request.Params.Q != nil {
		query.Query = *request.Params.Q
	}
	if request.Params.Filter != nil {
		query.Filter = newcomer.AssessmentFilter(*request.Params.Filter)
	}
	if request.Params.Limit != nil {
		query.Limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		query.Offset = *request.Params.Offset
	}
	page, err := handler.newcomerAdministration.Assessments(ctx, staffActor(session), query)
	switch {
	case errors.Is(err, newcomer.ErrInput):
		return newcomerAssessmentListBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "newcomer_assessment_read_denied", "无法查看新人考核", "当前后台身份没有 newcomer.assessment.read 权限。")
		return generated.ListNewcomerAssessments403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.NewcomerAssessment, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, newcomerAssessmentDTO(item))
	}
	return generated.ListNewcomerAssessments200JSONResponse{
		Items: items, Total: countText(page.Total), Limit: page.Limit, Offset: page.Offset,
	}, nil
}

func (handler *Handler) AssignNewcomerAssessment(ctx context.Context, request generated.AssignNewcomerAssessmentRequestObject) (generated.AssignNewcomerAssessmentResponseObject, error) {
	if request.Body == nil {
		return newcomerAssessmentAssignBadRequest(ctx), nil
	}
	session, problem, err := handler.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.AssignNewcomerAssessment401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.AssignNewcomerAssessment403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	reason := ""
	if request.Body.Reason != nil {
		reason = *request.Body.Reason
	}
	result, err := handler.newcomerAdministration.Assign(ctx, staffActor(session), newcomer.AssignInput{
		AssignmentID: request.Params.IdempotencyKey, UserID: request.Body.UserId, Reason: reason,
	})
	switch {
	case errors.Is(err, newcomer.ErrInput):
		return newcomerAssessmentAssignBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, newcomer.ErrSelfTarget):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "newcomer_assessment_assign_denied", "无法分配新人考核", "当前后台身份无权为该账户分配考核。")
		return generated.AssignNewcomerAssessment403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, newcomer.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "managed_user_not_found", "账户不存在", "目标账户不存在或已被移除。")
		return generated.AssignNewcomerAssessment404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, newcomer.ErrConflict), errors.Is(err, newcomer.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "newcomer_assignment_conflict", "无法重复分配考核", "账户不是正常状态、已经有考核，或当前没有启用的新人考核规则。")
		return generated.AssignNewcomerAssessment409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.AssignNewcomerAssessment201JSONResponse(newcomerAssessmentDTO(result)), nil
}

func (handler *Handler) ExemptNewcomerAssessment(ctx context.Context, request generated.ExemptNewcomerAssessmentRequestObject) (generated.ExemptNewcomerAssessmentResponseObject, error) {
	if request.Body == nil {
		return newcomerAssessmentExemptBadRequest(ctx), nil
	}
	session, problem, err := handler.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ExemptNewcomerAssessment401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ExemptNewcomerAssessment403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	result, err := handler.newcomerAdministration.Exempt(ctx, staffActor(session), newcomer.ExemptInput{
		ExemptionID: request.Params.IdempotencyKey, AssessmentID: request.AssessmentId,
		ExpectedVersion: request.Body.ExpectedVersion, Reason: request.Body.Reason,
	})
	switch {
	case errors.Is(err, newcomer.ErrInput):
		return newcomerAssessmentExemptBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden), errors.Is(err, newcomer.ErrSelfTarget):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "newcomer_assessment_exempt_denied", "无法豁免新人考核", "当前后台身份无权处置该考核。")
		return generated.ExemptNewcomerAssessment403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, newcomer.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "newcomer_assessment_not_found", "考核不存在", "该新人考核不存在。")
		return generated.ExemptNewcomerAssessment404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, newcomer.ErrConflict), errors.Is(err, newcomer.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "newcomer_assessment_conflict", "考核状态已变化", "请刷新列表后重新确认。")
		return generated.ExemptNewcomerAssessment409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ExemptNewcomerAssessment200JSONResponse(newcomerAssessmentDTO(result)), nil
}

func newcomerPolicyFromDTO(input generated.NewcomerPolicyInput) (newcomer.PolicyInput, error) {
	upload, err := strconv.ParseInt(string(input.MinimumCreditedUploadBytes), 10, 64)
	if err != nil {
		return newcomer.PolicyInput{}, newcomer.ErrInput
	}
	return newcomer.PolicyInput{
		Enabled: input.Enabled, DurationSeconds: input.DurationSeconds,
		MinimumCreditedUploadBytes:  upload,
		MinimumSeedingActiveSeconds: input.MinimumSeedingActiveSeconds,
	}, nil
}

func newcomerPolicyDTO(input newcomer.PolicyRevision) generated.NewcomerPolicyRevision {
	return generated.NewcomerPolicyRevision{
		Id: input.ID, Revision: input.Revision,
		SourceKind: generated.NewcomerPolicyRevisionSourceKind(input.SourceKind),
		Enabled:    input.Enabled, DurationSeconds: input.DurationSeconds,
		MinimumCreditedUploadBytes:  generated.TrafficByteCount(strconv.FormatInt(input.MinimumCreditedUploadBytes, 10)),
		MinimumSeedingActiveSeconds: input.MinimumSeedingActiveSeconds,
		EffectiveAt:                 input.EffectiveAt, Reason: input.Reason, CreatedAt: input.CreatedAt,
		TimelineState: generated.NewcomerPolicyRevisionTimelineState(input.TimelineState), Replayed: input.Replayed,
	}
}

func newcomerAssessmentDTO(input newcomer.Assessment) generated.NewcomerAssessment {
	return generated.NewcomerAssessment{
		Id: input.ID, UserId: input.UserID, UserNumericId: input.UserNumericID,
		Username: input.Username, DisplayName: input.DisplayName,
		PolicyRevisionId: input.PolicyRevisionID, PolicyRevision: input.PolicyRevision,
		Status: generated.NewcomerAssessmentStatus(input.Status), StartedAt: input.StartedAt,
		DeadlineAt:                  input.DeadlineAt,
		MinimumCreditedUploadBytes:  generated.TrafficByteCount(strconv.FormatInt(input.MinimumCreditedUploadBytes, 10)),
		MinimumSeedingActiveSeconds: input.MinimumSeedingActiveSeconds,
		CurrentCreditedUploadBytes:  generated.TrafficByteCount(strconv.FormatInt(input.CurrentCreditedUploadBytes, 10)),
		CurrentSeedingActiveSeconds: input.CurrentSeedingActiveSeconds,
		RestrictionStartedAt:        input.RestrictionStartedAt, ResolvedAt: input.ResolvedAt,
		ResolutionCode: input.ResolutionCode, Version: input.Version, UpdatedAt: input.UpdatedAt,
	}
}

func myNewcomerStatusDTO(input newcomer.MyStatus) generated.MyNewcomerAssessmentStatus {
	result := generated.MyNewcomerAssessmentStatus{ObservedAt: input.ObservedAt}
	if input.Assessment != nil {
		item := input.Assessment
		result.Assessment = &generated.MyNewcomerAssessment{
			Status: generated.NewcomerAssessmentStatus(item.Status), StartedAt: item.StartedAt,
			DeadlineAt:                  item.DeadlineAt,
			MinimumCreditedUploadBytes:  generated.TrafficByteCount(strconv.FormatInt(item.MinimumCreditedUploadBytes, 10)),
			MinimumSeedingActiveSeconds: item.MinimumSeedingActiveSeconds,
			CurrentCreditedUploadBytes:  generated.TrafficByteCount(strconv.FormatInt(item.CurrentCreditedUploadBytes, 10)),
			CurrentSeedingActiveSeconds: item.CurrentSeedingActiveSeconds,
			RestrictionStartedAt:        item.RestrictionStartedAt, ResolvedAt: item.ResolvedAt, UpdatedAt: item.UpdatedAt,
		}
	}
	return result
}

func newcomerPolicyListBadRequest(ctx context.Context) generated.ListNewcomerPolicyRevisions400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_newcomer_policy_query", "新人考核规则查询无效", "请检查分页参数。")
	return generated.ListNewcomerPolicyRevisions400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func newcomerAssessmentListBadRequest(ctx context.Context) generated.ListNewcomerAssessments400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_newcomer_assessment_query", "新人考核查询无效", "请检查筛选与分页参数。")
	return generated.ListNewcomerAssessments400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func newcomerPolicyIssueBadRequest(ctx context.Context) generated.IssueNewcomerPolicyRevision400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_newcomer_policy", "新人考核规则参数无效", "请检查考核天数、上传量、做种时长、生效时间和原因。")
	return generated.IssueNewcomerPolicyRevision400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func newcomerAssessmentExemptBadRequest(ctx context.Context) generated.ExemptNewcomerAssessment400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_newcomer_exemption", "豁免参数无效", "请刷新考核版本并填写至少 10 个字符的原因。")
	return generated.ExemptNewcomerAssessment400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func newcomerAssessmentAssignBadRequest(ctx context.Context) generated.AssignNewcomerAssessment400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_newcomer_assignment", "分配参数无效", "请刷新用户详情后重试；原因可留空，由系统自动记录。")
	return generated.AssignNewcomerAssessment400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}
