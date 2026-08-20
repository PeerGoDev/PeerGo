package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/peergo/peergo/contracts/go/hnrpolicyv1"
	"github.com/peergo/peergo/contracts/go/settlementoperationsv1"
	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/hnradmin"
)

func (h *Handler) ListHNRPolicyRevisions(ctx context.Context, request generated.ListHNRPolicyRevisionsRequestObject) (generated.ListHNRPolicyRevisionsResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListHNRPolicyRevisions401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListHNRPolicyRevisions403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit, offset := hnradmin.DefaultListLimit, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.hnrPolicyAdministration.List(ctx, staffActor(session), limit, offset)
	switch {
	case errors.Is(err, hnradmin.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_hnr_policy_query", "H&R 政策查询无效", "请检查分页参数。")
		return generated.ListHNRPolicyRevisions400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "hnr_policy_read_denied", "无法查看 H&R 政策", "当前后台身份没有 hnr.policy.read 权限。")
		return generated.ListHNRPolicyRevisions403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.HNRPolicyRevision, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, hnrPolicyRevisionDTO(item))
	}
	return generated.ListHNRPolicyRevisions200JSONResponse{
		Items: items, Total: strconv.FormatInt(page.Total, 10), Limit: page.Limit,
		Offset: page.Offset, MinimumEffectiveFrom: page.MinimumEffectiveFrom,
		Current:                   hnrPolicySettingsDTO(page.Current),
		GlobalRatioWatchConnected: page.GlobalRatioConnected,
	}, nil
}

func (h *Handler) PreviewHNRPolicy(ctx context.Context, request generated.PreviewHNRPolicyRequestObject) (generated.PreviewHNRPolicyResponseObject, error) {
	if request.Body == nil {
		return hnrPreviewBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.PreviewHNRPolicy401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.PreviewHNRPolicy403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	preview, err := h.hnrPolicyAdministration.Preview(ctx, staffActor(session), hnrPolicyInputFromDTO(*request.Body))
	switch {
	case errors.Is(err, hnradmin.ErrInput):
		return hnrPreviewBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "hnr_policy_preview_denied", "无法预览 H&R 政策", "当前后台身份没有 hnr.policy.read 权限。")
		return generated.PreviewHNRPolicy403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.PreviewHNRPolicy200JSONResponse{
		Policy: hnrPolicyInputDTO(preview.Policy), CompletionAt: preview.CompletionAt,
		AssessmentDueAt: preview.AssessmentDueAt, GraceEndsAt: preview.GraceEndsAt,
		ContinuousSeedSatisfiedAt: preview.ContinuousSeedSatisfiedAt,
	}, nil
}

func (h *Handler) IssueHNRPolicyRevision(ctx context.Context, request generated.IssueHNRPolicyRevisionRequestObject) (generated.IssueHNRPolicyRevisionResponseObject, error) {
	if request.Body == nil {
		return hnrIssueBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.IssueHNRPolicyRevision401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.IssueHNRPolicyRevision403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	revision, err := h.hnrPolicyAdministration.Issue(ctx, staffActor(session), hnradmin.IssueInput{
		RevisionID: request.Params.IdempotencyKey, Policy: hnrPolicyInputFromDTO(request.Body.Policy),
		EffectiveAt: request.Body.EffectiveAt, Reason: request.Body.Reason,
	})
	switch {
	case errors.Is(err, hnradmin.ErrInput):
		return hnrIssueBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "hnr_policy_issue_denied", "无法签发 H&R 政策", "当前后台身份没有 hnr.policy.issue 权限。")
		return generated.IssueHNRPolicyRevision403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, hnradmin.ErrNoChange):
		problem := newProblemFromContext(ctx, http.StatusConflict, "hnr_policy_unchanged", "H&R 规则没有变化", "请至少修改一项规则后再签发。")
		return generated.IssueHNRPolicyRevision409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, hnradmin.ErrConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "hnr_policy_timeline_conflict", "H&R 政策时间线发生冲突", "请刷新页面，并选择晚于已有修订的生效时间。")
		return generated.IssueHNRPolicyRevision409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, hnradmin.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "hnr_policy_idempotency_conflict", "请求标识已被使用", "请刷新页面后重新签发。")
		return generated.IssueHNRPolicyRevision409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.IssueHNRPolicyRevision201JSONResponse(hnrPolicyRevisionDTO(revision)), nil
}

func hnrPreviewBadRequest(ctx context.Context) generated.PreviewHNRPolicy400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_hnr_policy_preview", "H&R 规则参数无效", "考察期不能短于最低做种时间；单次计时上限须为 1 分钟至 24 小时。")
	return generated.PreviewHNRPolicy400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func hnrIssueBadRequest(ctx context.Context) generated.IssueHNRPolicyRevision400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_hnr_policy", "H&R 政策参数无效", "规则须至少提前 5 分钟生效，并填写至少 10 个字符的签发原因。")
	return generated.IssueHNRPolicyRevision400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func hnrPolicyInputFromDTO(input generated.HNRPolicyInput) hnradmin.PolicyInput {
	return hnradmin.PolicyInput{
		Mode: hnrpolicyv1.Mode(input.Mode), RequiredSeedSeconds: input.RequiredSeedSeconds,
		RequiredRatioBasisPoints: input.RequiredRatioBasisPoints,
		AssessmentWindowSeconds:  input.AssessmentWindowSeconds,
		GracePeriodSeconds:       input.GracePeriodSeconds,
		MaxIntervalCreditSeconds: input.MaxIntervalCreditSeconds,
	}
}

func hnrPolicyInputDTO(input hnradmin.PolicyInput) generated.HNRPolicyInput {
	return generated.HNRPolicyInput{
		Mode: generated.HNRPolicyMode(input.Mode), RequiredSeedSeconds: input.RequiredSeedSeconds,
		RequiredRatioBasisPoints: input.RequiredRatioBasisPoints,
		AssessmentWindowSeconds:  input.AssessmentWindowSeconds,
		GracePeriodSeconds:       input.GracePeriodSeconds,
		MaxIntervalCreditSeconds: input.MaxIntervalCreditSeconds,
	}
}

func hnrPolicyRevisionDTO(revision hnradmin.Revision) generated.HNRPolicyRevision {
	return generated.HNRPolicyRevision{
		Id: revision.ID, RuleId: revision.Policy.Rule.ID, RuleVersion: revision.Policy.Rule.Version,
		Mode: generated.HNRPolicyMode(revision.Policy.Mode), RequiredSeedSeconds: revision.Policy.RequiredSeedSeconds,
		RequiredRatioBasisPoints: revision.Policy.RequiredRatioBasisPoints,
		AssessmentWindowSeconds:  revision.Policy.AssessmentWindowSeconds,
		GracePeriodSeconds:       revision.Policy.GracePeriodSeconds,
		MaxIntervalCreditSeconds: revision.Policy.MaxIntervalCreditSeconds,
		EffectiveAt:              revision.EffectiveAt, Reason: revision.Reason, CreatedAt: revision.CreatedAt,
		DeliveryState:    generated.HNRPolicyRevisionDeliveryState(revision.DeliveryState),
		DeliveryAttempts: int(revision.DeliveryAttempts), LastDeliveryError: revision.LastDeliveryError,
		DeliveredAt: revision.DeliveredAt, TimelineState: generated.HNRPolicyRevisionTimelineState(revision.TimelineState),
		Replayed: revision.Replayed,
	}
}

func hnrPolicySettingsDTO(policy settlementoperationsv1.HNRPolicy) generated.HNRPolicySettings {
	return generated.HNRPolicySettings{
		Configured: policy.Configured, RevisionId: policy.RevisionID,
		EffectiveAt: policy.EffectiveAt, RuleId: policy.RuleID,
		RuleVersion: policy.RuleVersion, Mode: generated.HNRPolicySettingsMode(policy.Mode),
		RequiredSeedSeconds:      policy.RequiredSeedSeconds,
		RequiredRatioBasisPoints: policy.RequiredRatioBasisPoints,
		AssessmentWindowSeconds:  policy.AssessmentWindowSeconds,
		GracePeriodSeconds:       policy.GracePeriodSeconds,
		MaxIntervalCreditSeconds: policy.MaxIntervalCreditSeconds,
	}
}
