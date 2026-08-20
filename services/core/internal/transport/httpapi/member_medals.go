package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	openapi_types "github.com/oapi-codegen/runtime/types"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/medals"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func (h *Handler) GetMyMedals(ctx context.Context, _ generated.GetMyMedalsRequestObject) (generated.GetMyMedalsResponseObject, error) {
	overview, err := h.memberMedals.MyOverview(ctx, sessionTokenFromContext(ctx))
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看自己的勋章。")
		return generated.GetMyMedals401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "medal_read_denied", "无法查看勋章", "当前账号没有用户勋章查看权限。")
		return generated.GetMyMedals403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyMedals200JSONResponse(memberMedalOverviewDTO(overview)), nil
}

func (h *Handler) PurchaseMyMedal(ctx context.Context, request generated.PurchaseMyMedalRequestObject) (generated.PurchaseMyMedalResponseObject, error) {
	receipt, err := h.memberMedals.Purchase(
		ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken),
		request.Params.IdempotencyKey, request.MedalId,
	)
	switch {
	case errors.Is(err, medals.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_medal_purchase", "购买信息无效", "请刷新勋章商店后重新购买。")
		return generated.PurchaseMyMedal400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后购买勋章。")
		return generated.PurchaseMyMedal401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.PurchaseMyMedal403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "medal_purchase_denied", "暂时不能购买勋章", "当前账号没有勋章购买权限。")
		return generated.PurchaseMyMedal403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, medals.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "medal_not_found", "勋章不存在", "目标勋章已经不存在，请刷新列表。")
		return generated.PurchaseMyMedal404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, medals.ErrDisabled), errors.Is(err, medals.ErrNotPurchasable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "medal_not_purchasable", "当前无法购买", "勋章系统、销售时间或库存状态已经变化，请刷新后确认。")
		return generated.PurchaseMyMedal409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, medals.ErrAlreadyOwned):
		problem := newProblemFromContext(ctx, http.StatusConflict, "medal_already_owned", "已经拥有该勋章", "无需重复购买；过期状态请刷新页面确认。")
		return generated.PurchaseMyMedal409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, medals.ErrInsufficientMagic):
		problem := newProblemFromContext(ctx, http.StatusConflict, "medal_insufficient_magic", "魔力值余额不足", "当前可用整数魔力值不足以购买这枚勋章。")
		return generated.PurchaseMyMedal409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, medals.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "medal_purchase_idempotency_conflict", "请求标识已经使用", "请刷新勋章列表后重新操作。")
		return generated.PurchaseMyMedal409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.PurchaseMyMedal201JSONResponse(memberMedalPurchaseReceiptDTO(receipt)), nil
}

func (h *Handler) UpdateMyMedalWearing(ctx context.Context, request generated.UpdateMyMedalWearingRequestObject) (generated.UpdateMyMedalWearingResponseObject, error) {
	if request.Body == nil {
		problem := memberMedalMutationInputProblem(ctx)
		return generated.UpdateMyMedalWearing400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	holding, err := h.memberMedals.SetWearing(
		ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), request.MedalId,
		request.Body.ExpectedVersion, request.Body.Wearing,
	)
	switch {
	case errors.Is(err, medals.ErrInput):
		problem := memberMedalMutationInputProblem(ctx)
		return generated.UpdateMyMedalWearing400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后调整勋章。")
		return generated.UpdateMyMedalWearing401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF), errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "medal_wear_denied", "暂时不能调整勋章", "当前会话没有勋章佩戴权限，请刷新后重试。")
		return generated.UpdateMyMedalWearing403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, medals.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "medal_holding_not_found", "尚未拥有该勋章", "请刷新自己的勋章列表。")
		return generated.UpdateMyMedalWearing404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, medals.ErrDisabled), errors.Is(err, medals.ErrWearLimit), errors.Is(err, medals.ErrWorkgroupManaged), errors.Is(err, medals.ErrExpired), errors.Is(err, medals.ErrNoChange), errors.Is(err, medals.ErrVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "medal_wear_conflict", "佩戴状态已经变化", "可能已达到佩戴上限、勋章已过期，或其他页面刚刚修改了状态，请刷新后确认。")
		return generated.UpdateMyMedalWearing409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.UpdateMyMedalWearing200JSONResponse(memberMedalHoldingDTO(holding)), nil
}

func (h *Handler) UpdateMyMedalPriority(ctx context.Context, request generated.UpdateMyMedalPriorityRequestObject) (generated.UpdateMyMedalPriorityResponseObject, error) {
	if request.Body == nil {
		problem := memberMedalMutationInputProblem(ctx)
		return generated.UpdateMyMedalPriority400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	holding, err := h.memberMedals.MovePriority(
		ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), request.MedalId,
		request.Body.ExpectedVersion, medals.PriorityDirection(request.Body.Direction),
	)
	switch {
	case errors.Is(err, medals.ErrInput):
		problem := memberMedalMutationInputProblem(ctx)
		return generated.UpdateMyMedalPriority400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后调整勋章顺序。")
		return generated.UpdateMyMedalPriority401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF), errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "medal_priority_denied", "暂时不能调整顺序", "当前会话没有勋章佩戴权限，请刷新后重试。")
		return generated.UpdateMyMedalPriority403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, medals.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "wearing_medal_not_found", "没有找到已佩戴勋章", "请刷新佩戴列表。")
		return generated.UpdateMyMedalPriority404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, medals.ErrNoChange), errors.Is(err, medals.ErrVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "medal_priority_conflict", "勋章顺序没有变化", "勋章已位于边界，或其他页面刚刚修改了顺序，请刷新后确认。")
		return generated.UpdateMyMedalPriority409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.UpdateMyMedalPriority200JSONResponse(memberMedalHoldingDTO(holding)), nil
}

func memberMedalOverviewDTO(value medals.MemberOverview) generated.MemberMedalOverview {
	items := make([]generated.MemberMedal, 0, len(value.Items))
	for _, item := range value.Items {
		items = append(items, memberMedalDTO(item))
	}
	return generated.MemberMedalOverview{
		Settings: medalSettingsDTO(value.Settings), MagicBalance: strconv.FormatInt(value.MagicBalance, 10),
		Benefits: generated.MemberMedalBenefitSummary{
			UploadBonusBps: value.Benefits.UploadBonusBPS, DownloadDiscountBps: value.Benefits.DownloadDiscountBPS,
			MagicBonusBps: value.Benefits.MagicBonusBPS, InviteBonus: strconv.FormatInt(value.Benefits.InviteBonus, 10),
		},
		OwnedCount: strconv.FormatInt(value.OwnedCount, 10), WearingCount: strconv.FormatInt(value.WearingCount, 10),
		ShopCount: strconv.FormatInt(value.ShopCount, 10), Items: items,
	}
}

func memberMedalDTO(value medals.MemberMedal) generated.MemberMedal {
	result := generated.MemberMedal{
		Id: strconv.FormatInt(value.ID, 10), Name: value.Name, Description: value.Description,
		ImageLargePath: value.ImageLargePath, ImageSmallPath: value.ImageSmallPath,
		AcquisitionMethod: generated.MedalAcquisitionMethod(value.AcquisitionMethod),
		Price:             strconv.FormatInt(value.Price, 10), DurationDays: value.DurationDays,
		UploadBonusBps: value.UploadBonusBPS, DownloadDiscountBps: value.DownloadDiscountBPS,
		MagicBonusBps: value.MagicBonusBPS, InviteBonus: strconv.FormatInt(value.InviteBonus, 10),
		IsWorkgroup: value.IsWorkgroup, SaleBeginAt: value.SaleBeginAt, SaleEndAt: value.SaleEndAt,
		Purchasable: value.Purchasable, PurchaseUnavailableReason: value.PurchaseUnavailableReason,
	}
	if value.Inventory != nil {
		text := strconv.FormatInt(*value.Inventory, 10)
		result.Inventory = &text
	}
	if value.Holding != nil {
		holding := memberMedalHoldingDTO(*value.Holding)
		result.Holding = &holding
	}
	return result
}

func memberMedalHoldingDTO(value medals.Holding) generated.MemberMedalHolding {
	return generated.MemberMedalHolding{
		Id: strconv.FormatInt(value.ID, 10), State: generated.MemberMedalHoldingState(value.State),
		Priority: value.Priority, ExpiresAt: value.ExpiresAt, AcquiredAt: value.AcquiredAt,
		Version: value.Version,
	}
}

func memberMedalPurchaseReceiptDTO(value medals.PurchaseReceipt) generated.MemberMedalPurchaseReceipt {
	result := generated.MemberMedalPurchaseReceipt{
		Id: openapi_types.UUID(value.ID), MedalId: strconv.FormatInt(value.MedalID, 10),
		UserMedalId: strconv.FormatInt(value.UserMedalID, 10), Price: strconv.FormatInt(value.Price, 10),
		BalanceAfter: strconv.FormatInt(value.BalanceAfter, 10), PurchasedAt: value.PurchasedAt, Replayed: value.Replayed,
	}
	if value.MagicTransactionID != nil {
		converted := openapi_types.UUID(*value.MagicTransactionID)
		result.MagicTransactionId = &converted
	}
	return result
}

func memberMedalMutationInputProblem(ctx context.Context) generated.Problem {
	return newProblemFromContext(ctx, http.StatusBadRequest, "invalid_medal_mutation", "勋章操作无效", "请刷新勋章列表后重新操作。")
}
