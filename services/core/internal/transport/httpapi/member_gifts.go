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
	"github.com/peergo/peergo/services/core/internal/modules/economy/membergift"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func (h *Handler) GetMyMemberGifts(ctx context.Context, request generated.GetMyMemberGiftsRequestObject) (generated.GetMyMemberGiftsResponseObject, error) {
	limit := membergift.DefaultHistoryLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	overview, err := h.memberGifts.MyOverview(ctx, sessionTokenFromContext(ctx), limit)
	switch {
	case errors.Is(err, membergift.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_member_gift_query", "赠送记录查询无效", "最近记录数量必须在 1 到 100 之间。")
		return generated.GetMyMemberGifts400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看赠送记录。")
		return generated.GetMyMemberGifts401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "member_gift_read_denied", "无法查看赠送记录", "当前账号没有成员赠送记录查看权限。")
		return generated.GetMyMemberGifts403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyMemberGifts200JSONResponse(memberGiftOverviewDTO(overview)), nil
}

func (h *Handler) CreateMyMemberGift(ctx context.Context, request generated.CreateMyMemberGiftRequestObject) (generated.CreateMyMemberGiftResponseObject, error) {
	if request.Body == nil {
		return memberGiftBadRequest(ctx), nil
	}
	recipientNumericID, recipientErr := strconv.ParseInt(string(request.Body.RecipientNumericId), 10, 64)
	amount, amountErr := strconv.ParseInt(string(request.Body.Amount), 10, 64)
	if recipientErr != nil || amountErr != nil {
		return memberGiftBadRequest(ctx), nil
	}
	gift, err := h.memberGifts.Create(
		ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken),
		request.Params.IdempotencyKey, recipientNumericID, amount, request.Body.Message,
	)
	switch {
	case errors.Is(err, membergift.ErrInput), errors.Is(err, membergift.ErrAmountOutOfRange), errors.Is(err, membergift.ErrSelf):
		return memberGiftBadRequest(ctx), nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后赠送魔力值。")
		return generated.CreateMyMemberGift401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.CreateMyMemberGift403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "member_gift_create_denied", "暂时不能赠送", "当前账号没有成员赠送权限。")
		return generated.CreateMyMemberGift403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, membergift.ErrSenderIneligible):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "member_gift_sender_ineligible", "暂时不能赠送", "请先完成邮箱验证，并确认账户当前处于正常状态。")
		return generated.CreateMyMemberGift403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, membergift.ErrRecipientUnavailable):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "member_gift_recipient_unavailable", "找不到可接收赠送的成员", "请核对成员数字 ID；对方还必须完成验证且账户状态正常。")
		return generated.CreateMyMemberGift404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, membergift.ErrDisabled), errors.Is(err, membergift.ErrPolicyNotFound):
		problem := newProblemFromContext(ctx, http.StatusConflict, "member_gift_unavailable", "成员赠送暂未开放", "管理员尚未启用成员赠送。")
		return generated.CreateMyMemberGift409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, membergift.ErrDailyLimit):
		problem := newProblemFromContext(ctx, http.StatusConflict, "member_gift_daily_limit", "已超过今天的赠送额度", "请减少赠送金额或在下一个站点自然日再试。")
		return generated.CreateMyMemberGift409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, membergift.ErrInsufficientBalance):
		problem := newProblemFromContext(ctx, http.StatusConflict, "member_gift_insufficient_balance", "魔力值余额不足", "赠送金额不能超过当前可用整数魔力值。")
		return generated.CreateMyMemberGift409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, membergift.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "member_gift_idempotency_conflict", "请求标识已被使用", "请刷新赠送记录后重新操作。")
		return generated.CreateMyMemberGift409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.CreateMyMemberGift201JSONResponse(memberGiftDTO(gift)), nil
}

func (h *Handler) ListMemberGiftPolicies(ctx context.Context, request generated.ListMemberGiftPoliciesRequestObject) (generated.ListMemberGiftPoliciesResponseObject, error) {
	session, problem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListMemberGiftPolicies401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListMemberGiftPolicies403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	limit, offset := membergift.DefaultPolicyLimit, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.memberGifts.ListPolicies(ctx, staffActor(session), limit, offset)
	switch {
	case errors.Is(err, membergift.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_member_gift_policy_query", "赠送政策查询无效", "请检查分页参数。")
		return generated.ListMemberGiftPolicies400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "member_gift_policy_read_denied", "无法查看成员赠送设置", "当前后台身份没有成员赠送政策读取权限。")
		return generated.ListMemberGiftPolicies403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.MemberGiftPolicy, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, memberGiftPolicyDTO(item))
	}
	return generated.ListMemberGiftPolicies200JSONResponse{
		Items: items, Total: strconv.FormatInt(page.Total, 10), Limit: page.Limit, Offset: page.Offset,
	}, nil
}

func (h *Handler) IssueMemberGiftPolicy(ctx context.Context, request generated.IssueMemberGiftPolicyRequestObject) (generated.IssueMemberGiftPolicyResponseObject, error) {
	if request.Body == nil {
		return memberGiftPolicyBadRequest(ctx), nil
	}
	minimum, minimumErr := strconv.ParseInt(string(request.Body.Settings.MinimumAmount), 10, 64)
	maximum, maximumErr := strconv.ParseInt(string(request.Body.Settings.MaximumAmount), 10, 64)
	daily, dailyErr := strconv.ParseInt(string(request.Body.Settings.DailyGrossLimit), 10, 64)
	if minimumErr != nil || maximumErr != nil || dailyErr != nil {
		return memberGiftPolicyBadRequest(ctx), nil
	}
	session, problem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.IssueMemberGiftPolicy401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.IssueMemberGiftPolicy403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	published, err := h.memberGifts.IssuePolicy(ctx, staffActor(session), request.Params.IdempotencyKey, membergift.PolicyRevision{
		Enabled: request.Body.Settings.Enabled, MinimumAmount: minimum,
		MaximumAmount: maximum, DailyGrossLimit: daily, FeeBPS: int32(request.Body.Settings.FeeBps),
	}, request.Body.Reason)
	switch {
	case errors.Is(err, membergift.ErrInput):
		return memberGiftPolicyBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "member_gift_policy_issue_denied", "无法保存成员赠送设置", "当前后台身份没有成员赠送政策签发权限。")
		return generated.IssueMemberGiftPolicy403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, membergift.ErrPolicyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "member_gift_policy_conflict", "成员赠送设置发生冲突", "请刷新页面后重新保存；既有赠送记录不会被覆盖。")
		return generated.IssueMemberGiftPolicy409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.IssueMemberGiftPolicy201JSONResponse(memberGiftPolicyDTO(published)), nil
}

func memberGiftBadRequest(ctx context.Context) generated.CreateMyMemberGift400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_member_gift", "赠送信息无效", "请填写其他成员的数字 ID、政策范围内的整数金额，以及不超过 200 字的留言。")
	return generated.CreateMyMemberGift400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func memberGiftPolicyBadRequest(ctx context.Context) generated.IssueMemberGiftPolicy400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_member_gift_policy", "成员赠送设置无效", "请检查单笔上下限、每日上限、手续费和至少 10 个字符的变更原因。")
	return generated.IssueMemberGiftPolicy400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func memberGiftOverviewDTO(overview membergift.Overview) generated.MemberGiftOverview {
	history := make([]generated.MemberGiftRecord, 0, len(overview.History))
	for _, gift := range overview.History {
		history = append(history, memberGiftDTO(gift))
	}
	return generated.MemberGiftOverview{
		Policy:         memberGiftPolicyDTO(overview.Policy),
		MyNumericId:    strconv.FormatInt(overview.MyNumericID, 10),
		OutgoingToday:  strconv.FormatInt(overview.OutgoingToday, 10),
		RemainingToday: strconv.FormatInt(overview.RemainingToday, 10), History: history,
	}
}

func memberGiftDTO(gift membergift.Gift) generated.MemberGiftRecord {
	return generated.MemberGiftRecord{
		Id: openapi_types.UUID(gift.ID), Direction: generated.MemberGiftRecordDirection(gift.Direction),
		Counterparty: generated.MemberGiftCounterparty{
			NumericId: strconv.FormatInt(gift.Counterparty.NumericID, 10),
			Username:  gift.Counterparty.Username, DisplayName: gift.Counterparty.DisplayName,
		},
		GrossAmount: strconv.FormatInt(gift.GrossAmount, 10), FeeAmount: strconv.FormatInt(gift.FeeAmount, 10),
		NetAmount: strconv.FormatInt(gift.NetAmount, 10), Message: gift.Message,
		PolicyRevision: gift.PolicyRevision, OccurredAt: gift.OccurredAt,
	}
}

func memberGiftPolicyDTO(published membergift.PublishedPolicy) generated.MemberGiftPolicy {
	policy := published.Policy
	result := generated.MemberGiftPolicy{
		Revision: policy.Revision, CreatedAt: policy.CreatedAt,
		Settings: generated.MemberGiftPolicySettings{
			Enabled: policy.Enabled, MinimumAmount: strconv.FormatInt(policy.MinimumAmount, 10),
			MaximumAmount:   strconv.FormatInt(policy.MaximumAmount, 10),
			DailyGrossLimit: strconv.FormatInt(policy.DailyGrossLimit, 10), FeeBps: int(policy.FeeBPS),
		},
		SnapshotSha256: hex.EncodeToString(policy.SnapshotSHA256[:]), Reason: published.Reason,
	}
	if published.IssuedBy != nil {
		value := openapi_types.UUID(*published.IssuedBy)
		result.IssuedBy = &value
	}
	return result
}
