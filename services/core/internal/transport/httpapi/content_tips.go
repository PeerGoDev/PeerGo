package httpapi

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"

	openapi_types "github.com/oapi-codegen/runtime/types"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/contenttip"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func (h *Handler) GetMyContentTips(ctx context.Context, request generated.GetMyContentTipsRequestObject) (generated.GetMyContentTipsResponseObject, error) {
	limit := contenttip.DefaultHistoryLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	overview, err := h.contentTips.MyOverview(ctx, sessionTokenFromContext(ctx), limit)
	switch {
	case errors.Is(err, contenttip.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_content_tip_query", "打赏记录查询无效", "最近记录数量必须在 1 到 100 之间。")
		return generated.GetMyContentTips400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看内容打赏。")
		return generated.GetMyContentTips401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "content_tip_read_denied", "无法查看打赏记录", "当前账号没有内容打赏记录查看权限。")
		return generated.GetMyContentTips403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyContentTips200JSONResponse(contentTipOverviewDTO(overview)), nil
}

func (h *Handler) CreateTorrentContentTip(ctx context.Context, request generated.CreateTorrentContentTipRequestObject) (generated.CreateTorrentContentTipResponseObject, error) {
	var amount generated.UnsignedIntegerText
	if request.Body != nil {
		amount = request.Body.Amount
	}
	tip, problem, status, err := h.createContentTip(ctx, amount, request.Body != nil, string(request.Params.XCSRFToken), request.Params.IdempotencyKey, contenttip.TorrentTarget(int64(request.TorrentId)))
	if err != nil {
		return nil, err
	}
	if problem == nil {
		return generated.CreateTorrentContentTip201JSONResponse(contentTipDTO(tip)), nil
	}
	switch status {
	case http.StatusBadRequest:
		return generated.CreateTorrentContentTip400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
	case http.StatusUnauthorized:
		return generated.CreateTorrentContentTip401ApplicationProblemPlusJSONResponse(*problem), nil
	case http.StatusForbidden:
		return generated.CreateTorrentContentTip403ApplicationProblemPlusJSONResponse(*problem), nil
	case http.StatusNotFound:
		return generated.CreateTorrentContentTip404ApplicationProblemPlusJSONResponse(*problem), nil
	default:
		return generated.CreateTorrentContentTip409ApplicationProblemPlusJSONResponse(*problem), nil
	}
}

func (h *Handler) CreatePostContentTip(ctx context.Context, request generated.CreatePostContentTipRequestObject) (generated.CreatePostContentTipResponseObject, error) {
	var amount generated.UnsignedIntegerText
	if request.Body != nil {
		amount = request.Body.Amount
	}
	tip, problem, status, err := h.createContentTip(ctx, amount, request.Body != nil, string(request.Params.XCSRFToken), request.Params.IdempotencyKey, contenttip.PostTarget(request.PostId))
	if err != nil {
		return nil, err
	}
	if problem == nil {
		return generated.CreatePostContentTip201JSONResponse(contentTipDTO(tip)), nil
	}
	switch status {
	case http.StatusBadRequest:
		return generated.CreatePostContentTip400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
	case http.StatusUnauthorized:
		return generated.CreatePostContentTip401ApplicationProblemPlusJSONResponse(*problem), nil
	case http.StatusForbidden:
		return generated.CreatePostContentTip403ApplicationProblemPlusJSONResponse(*problem), nil
	case http.StatusNotFound:
		return generated.CreatePostContentTip404ApplicationProblemPlusJSONResponse(*problem), nil
	default:
		return generated.CreatePostContentTip409ApplicationProblemPlusJSONResponse(*problem), nil
	}
}

func (h *Handler) CreateCommentContentTip(ctx context.Context, request generated.CreateCommentContentTipRequestObject) (generated.CreateCommentContentTipResponseObject, error) {
	var amount generated.UnsignedIntegerText
	if request.Body != nil {
		amount = request.Body.Amount
	}
	tip, problem, status, err := h.createContentTip(ctx, amount, request.Body != nil, string(request.Params.XCSRFToken), request.Params.IdempotencyKey, contenttip.CommentTarget(request.CommentId))
	if err != nil {
		return nil, err
	}
	if problem == nil {
		return generated.CreateCommentContentTip201JSONResponse(contentTipDTO(tip)), nil
	}
	switch status {
	case http.StatusBadRequest:
		return generated.CreateCommentContentTip400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
	case http.StatusUnauthorized:
		return generated.CreateCommentContentTip401ApplicationProblemPlusJSONResponse(*problem), nil
	case http.StatusForbidden:
		return generated.CreateCommentContentTip403ApplicationProblemPlusJSONResponse(*problem), nil
	case http.StatusNotFound:
		return generated.CreateCommentContentTip404ApplicationProblemPlusJSONResponse(*problem), nil
	default:
		return generated.CreateCommentContentTip409ApplicationProblemPlusJSONResponse(*problem), nil
	}
}

func (h *Handler) createContentTip(ctx context.Context, amountText generated.UnsignedIntegerText, bodyPresent bool, csrf string, requestID openapi_types.UUID, target contenttip.Target) (contenttip.Tip, *generated.Problem, int, error) {
	if !bodyPresent {
		problem := contentTipProblem(ctx, http.StatusBadRequest, contenttip.ErrInput)
		return contenttip.Tip{}, &problem, http.StatusBadRequest, nil
	}
	amount, err := strconv.ParseInt(string(amountText), 10, 64)
	if err != nil {
		problem := contentTipProblem(ctx, http.StatusBadRequest, contenttip.ErrInput)
		return contenttip.Tip{}, &problem, http.StatusBadRequest, nil
	}
	tip, err := h.contentTips.Create(ctx, sessionTokenFromContext(ctx), csrf, requestID, target, amount)
	if err == nil {
		return tip, nil, 0, nil
	}
	status := http.StatusConflict
	switch {
	case errors.Is(err, contenttip.ErrInput), errors.Is(err, contenttip.ErrAmountOutOfRange), errors.Is(err, contenttip.ErrSelf):
		status = http.StatusBadRequest
	case errors.Is(err, identity.ErrSessionNotFound):
		status = http.StatusUnauthorized
	case errors.Is(err, identity.ErrInvalidCSRF), errors.Is(err, authz.ErrForbidden), errors.Is(err, contenttip.ErrTipperIneligible):
		status = http.StatusForbidden
	case errors.Is(err, contenttip.ErrTargetUnavailable), errors.Is(err, contenttip.ErrRecipientUnavailable):
		status = http.StatusNotFound
	case errors.Is(err, contenttip.ErrDisabled), errors.Is(err, contenttip.ErrPolicyNotFound), errors.Is(err, contenttip.ErrDailyLimit), errors.Is(err, contenttip.ErrInsufficientBalance), errors.Is(err, contenttip.ErrIdempotencyConflict):
		status = http.StatusConflict
	default:
		return contenttip.Tip{}, nil, 0, err
	}
	problem := contentTipProblem(ctx, status, err)
	return contenttip.Tip{}, &problem, status, nil
}

func contentTipProblem(ctx context.Context, status int, err error) generated.Problem {
	switch {
	case status == http.StatusBadRequest:
		return newProblemFromContext(ctx, status, "invalid_content_tip", "打赏信息无效", "请选择他人的公开内容，并填写政策范围内的整数金额。")
	case status == http.StatusUnauthorized:
		return newProblemFromContext(ctx, status, "session_required", "需要登录", "请重新登录后打赏。")
	case errors.Is(err, identity.ErrInvalidCSRF):
		return newProblemFromContext(ctx, status, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
	case errors.Is(err, authz.ErrForbidden):
		return newProblemFromContext(ctx, status, "content_tip_create_denied", "暂时不能打赏", "当前账号没有内容打赏权限。")
	case errors.Is(err, contenttip.ErrTipperIneligible):
		return newProblemFromContext(ctx, status, "content_tip_tipper_ineligible", "暂时不能打赏", "请先完成邮箱验证，并确认账户当前处于正常状态。")
	case errors.Is(err, contenttip.ErrRecipientUnavailable):
		return newProblemFromContext(ctx, status, "content_tip_recipient_unavailable", "作者暂时不能接收打赏", "目标作者尚未完成验证或账户当前不可用。")
	case errors.Is(err, contenttip.ErrTargetUnavailable):
		return newProblemFromContext(ctx, status, "content_tip_target_unavailable", "内容暂时不能打赏", "目标内容不存在、已删除或当前不可见。")
	case errors.Is(err, contenttip.ErrDisabled), errors.Is(err, contenttip.ErrPolicyNotFound):
		return newProblemFromContext(ctx, status, "content_tip_unavailable", "内容打赏暂未开放", "管理员尚未启用内容打赏。")
	case errors.Is(err, contenttip.ErrDailyLimit):
		return newProblemFromContext(ctx, status, "content_tip_daily_limit", "已超过今天的打赏额度", "请减少金额或在下一个站点自然日再试。")
	case errors.Is(err, contenttip.ErrInsufficientBalance):
		return newProblemFromContext(ctx, status, "content_tip_insufficient_balance", "魔力值余额不足", "打赏金额不能超过当前可用整数魔力值。")
	default:
		return newProblemFromContext(ctx, status, "content_tip_idempotency_conflict", "请求标识已被使用", "请刷新打赏记录后重新操作。")
	}
}

func (h *Handler) ListContentTipPolicies(ctx context.Context, request generated.ListContentTipPoliciesRequestObject) (generated.ListContentTipPoliciesResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListContentTipPolicies401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListContentTipPolicies403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit, offset := contenttip.DefaultPolicyLimit, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.contentTips.ListPolicies(ctx, staffActor(session), limit, offset)
	switch {
	case errors.Is(err, contenttip.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_content_tip_policy_query", "打赏政策查询无效", "请检查分页参数。")
		return generated.ListContentTipPolicies400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "content_tip_policy_read_denied", "无法查看打赏设置", "当前后台身份没有内容打赏政策读取权限。")
		return generated.ListContentTipPolicies403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.ContentTipPolicy, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, contentTipPolicyDTO(item))
	}
	return generated.ListContentTipPolicies200JSONResponse{Items: items, Total: strconv.FormatInt(page.Total, 10), Limit: page.Limit, Offset: page.Offset}, nil
}

func (h *Handler) IssueContentTipPolicy(ctx context.Context, request generated.IssueContentTipPolicyRequestObject) (generated.IssueContentTipPolicyResponseObject, error) {
	if request.Body == nil {
		return contentTipPolicyBadRequest(ctx), nil
	}
	minimum, minimumErr := strconv.ParseInt(string(request.Body.Settings.MinimumAmount), 10, 64)
	maximum, maximumErr := strconv.ParseInt(string(request.Body.Settings.MaximumAmount), 10, 64)
	daily, dailyErr := strconv.ParseInt(string(request.Body.Settings.DailyGrossLimit), 10, 64)
	if minimumErr != nil || maximumErr != nil || dailyErr != nil {
		return contentTipPolicyBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.IssueContentTipPolicy401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.IssueContentTipPolicy403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	published, err := h.contentTips.IssuePolicy(ctx, staffActor(session), request.Params.IdempotencyKey, contenttip.PolicyRevision{
		Enabled: request.Body.Settings.Enabled, MinimumAmount: minimum, MaximumAmount: maximum,
		DailyGrossLimit: daily, FeeBPS: int32(request.Body.Settings.FeeBps),
	}, request.Body.Reason)
	switch {
	case errors.Is(err, contenttip.ErrInput):
		return contentTipPolicyBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "content_tip_policy_issue_denied", "无法保存打赏设置", "当前后台身份没有内容打赏政策签发权限。")
		return generated.IssueContentTipPolicy403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, contenttip.ErrPolicyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "content_tip_policy_conflict", "打赏设置发生冲突", "请刷新页面后重新保存；既有打赏记录不会被覆盖。")
		return generated.IssueContentTipPolicy409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.IssueContentTipPolicy201JSONResponse(contentTipPolicyDTO(published)), nil
}

func contentTipPolicyBadRequest(ctx context.Context) generated.IssueContentTipPolicy400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_content_tip_policy", "内容打赏设置无效", "请检查单笔上下限、每日上限、手续费和至少 10 个字符的变更原因。")
	return generated.IssueContentTipPolicy400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func contentTipOverviewDTO(overview contenttip.Overview) generated.ContentTipOverview {
	history := make([]generated.ContentTipRecord, 0, len(overview.History))
	for _, tip := range overview.History {
		history = append(history, contentTipDTO(tip))
	}
	return generated.ContentTipOverview{Policy: contentTipPolicyDTO(overview.Policy), OutgoingToday: strconv.FormatInt(overview.OutgoingToday, 10), RemainingToday: strconv.FormatInt(overview.RemainingToday, 10), History: history}
}

func contentTipDTO(tip contenttip.Tip) generated.ContentTipRecord {
	target := generated.ContentTipTarget{Kind: generated.ContentTipTargetKind(tip.Target.Kind), Title: tip.Target.Title}
	switch tip.Target.Kind {
	case contenttip.TargetTorrent:
		value := tip.Target.TorrentID
		target.TorrentId = &value
	case contenttip.TargetPost:
		value := openapi_types.UUID(tip.Target.PostID)
		target.PostId = &value
	case contenttip.TargetComment:
		value := openapi_types.UUID(tip.Target.CommentID)
		target.CommentId = &value
	}
	return generated.ContentTipRecord{
		Id: openapi_types.UUID(tip.ID), Direction: generated.ContentTipRecordDirection(tip.Direction),
		Counterparty: generated.MemberGiftCounterparty{NumericId: strconv.FormatInt(tip.Counterparty.NumericID, 10), Username: tip.Counterparty.Username, DisplayName: tip.Counterparty.DisplayName},
		Target:       target, GrossAmount: strconv.FormatInt(tip.GrossAmount, 10), FeeAmount: strconv.FormatInt(tip.FeeAmount, 10), NetAmount: strconv.FormatInt(tip.NetAmount, 10),
		PolicyRevision: tip.PolicyRevision, OccurredAt: tip.OccurredAt,
	}
}

func contentTipPolicyDTO(published contenttip.PublishedPolicy) generated.ContentTipPolicy {
	policy := published.Policy
	result := generated.ContentTipPolicy{
		Revision: policy.Revision, CreatedAt: policy.CreatedAt, Reason: published.Reason,
		Settings:       generated.ContentTipPolicySettings{Enabled: policy.Enabled, MinimumAmount: strconv.FormatInt(policy.MinimumAmount, 10), MaximumAmount: strconv.FormatInt(policy.MaximumAmount, 10), DailyGrossLimit: strconv.FormatInt(policy.DailyGrossLimit, 10), FeeBps: int(policy.FeeBPS)},
		SnapshotSha256: hex.EncodeToString(policy.SnapshotSHA256[:]),
	}
	if published.IssuedBy != nil {
		value := openapi_types.UUID(*published.IssuedBy)
		result.IssuedBy = &value
	}
	return result
}
