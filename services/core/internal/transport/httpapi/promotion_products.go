package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/promotions"
)

func (handler *Handler) GetMyPromotionProductOffer(ctx context.Context, request generated.GetMyPromotionProductOfferRequestObject) (generated.GetMyPromotionProductOfferResponseObject, error) {
	offer, err := handler.promotionAdministration.Offer(ctx, sessionTokenFromContext(ctx), request.TorrentId)
	switch {
	case errors.Is(err, promotions.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_promotion_product_offer", "促销报价查询无效", "请检查种子编号。")
		return generated.GetMyPromotionProductOffer400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看促销报价。")
		return generated.GetMyPromotionProductOffer401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "promotion_product_denied", "无法查看促销报价", "当前账号没有付费促销权限。")
		return generated.GetMyPromotionProductOffer403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, promotions.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不存在", "该种子不存在或当前未发布。")
		return generated.GetMyPromotionProductOffer404ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyPromotionProductOffer200JSONResponse(promotionProductOfferDTO(offer)), nil
}

func (handler *Handler) PurchasePromotionProducts(ctx context.Context, request generated.PurchasePromotionProductsRequestObject) (generated.PurchasePromotionProductsResponseObject, error) {
	if request.Body == nil {
		return promotionProductPurchaseBadRequest(ctx), nil
	}
	selection := promotions.ProductSelection{}
	if request.Body.Promotion != nil {
		value := promotions.Promotion(*request.Body.Promotion)
		selection.Promotion = &value
	}
	if request.Body.PromotionDays != nil {
		selection.PromotionDays = *request.Body.PromotionDays
	}
	if request.Body.StickyDays != nil {
		selection.StickyDays = *request.Body.StickyDays
	}
	order, err := handler.promotionAdministration.Purchase(
		ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken),
		request.Params.IdempotencyKey, request.TorrentId, selection,
	)
	switch {
	case errors.Is(err, promotions.ErrInput):
		return promotionProductPurchaseBadRequest(ctx), nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后购买促销。")
		return generated.PurchasePromotionProducts401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.PurchasePromotionProducts403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, promotions.ErrEmailUnverified):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "verified_email_required", "需要先验证邮箱", "完成邮箱验证后才能使用魔力值购买促销。")
		return generated.PurchasePromotionProducts403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "promotion_product_denied", "无法购买促销", "当前账号没有付费促销权限。")
		return generated.PurchasePromotionProducts403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, promotions.ErrNotFound), errors.Is(err, promotions.ErrTorrentUnavailable):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不存在", "该种子不存在或当前未发布。")
		return generated.PurchasePromotionProducts404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, promotions.ErrInsufficientBalance):
		problem := newProblemFromContext(ctx, http.StatusConflict, "magic_balance_insufficient", "魔力值不足", "当前魔力值余额不足以完成这笔促销购买。")
		return generated.PurchasePromotionProducts409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, promotions.ErrProductDisabled):
		problem := newProblemFromContext(ctx, http.StatusConflict, "promotion_product_disabled", "暂时无法购买", "站点已暂停相应的用户付费促销产品。")
		return generated.PurchasePromotionProducts409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, promotions.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "promotion_product_idempotency_conflict", "请求标识已被使用", "请刷新购买状态后重新操作。")
		return generated.PurchasePromotionProducts409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.PurchasePromotionProducts201JSONResponse(promotionProductOrderDTO(order)), nil
}

func (handler *Handler) ListMyPromotionProductOrders(ctx context.Context, request generated.ListMyPromotionProductOrdersRequestObject) (generated.ListMyPromotionProductOrdersResponseObject, error) {
	limit, offset := promotions.DefaultProductOrderLimit, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := handler.promotionAdministration.MyOrders(ctx, sessionTokenFromContext(ctx), limit, offset)
	switch {
	case errors.Is(err, promotions.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_promotion_product_orders", "促销购买记录查询无效", "请检查分页范围。")
		return generated.ListMyPromotionProductOrders400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看促销购买记录。")
		return generated.ListMyPromotionProductOrders401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "promotion_product_denied", "无法查看促销购买记录", "当前账号没有付费促销权限。")
		return generated.ListMyPromotionProductOrders403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListMyPromotionProductOrders200JSONResponse(promotionProductOrderPageDTO(page)), nil
}

func (handler *Handler) ListManagedPromotionProductOrders(ctx context.Context, request generated.ListManagedPromotionProductOrdersRequestObject) (generated.ListManagedPromotionProductOrdersResponseObject, error) {
	session, problem, err := handler.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.ListManagedPromotionProductOrders401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.ListManagedPromotionProductOrders403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	query := promotions.ProductOrderQuery{Limit: promotions.DefaultProductOrderLimit}
	if request.Params.Query != nil {
		query.Query = *request.Params.Query
	}
	if request.Params.Limit != nil {
		query.Limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		query.Offset = *request.Params.Offset
	}
	page, err := handler.promotionAdministration.AdminOrders(ctx, staffActor(session), query)
	switch {
	case errors.Is(err, promotions.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_promotion_product_orders", "促销购买记录查询无效", "请检查搜索和分页参数。")
		return generated.ListManagedPromotionProductOrders400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "promotion_product_read_denied", "无法查看付费促销记录", "当前后台身份没有 promotion.manage.read 权限。")
		return generated.ListManagedPromotionProductOrders403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListManagedPromotionProductOrders200JSONResponse(promotionProductOrderPageDTO(page)), nil
}

func (handler *Handler) GetPromotionProductPolicy(ctx context.Context, _ generated.GetPromotionProductPolicyRequestObject) (generated.GetPromotionProductPolicyResponseObject, error) {
	session, problem, err := handler.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetPromotionProductPolicy401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.GetPromotionProductPolicy403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	policy, err := handler.promotionAdministration.ProductPolicy(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "promotion_product_read_denied", "无法查看付费促销规则", "当前后台身份没有 promotion.manage.read 权限。")
		return generated.GetPromotionProductPolicy403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetPromotionProductPolicy200JSONResponse(promotionProductPolicyDTO(policy)), nil
}

func (handler *Handler) UpdatePromotionProductPolicy(ctx context.Context, request generated.UpdatePromotionProductPolicyRequestObject) (generated.UpdatePromotionProductPolicyResponseObject, error) {
	if request.Body == nil {
		return promotionProductPolicyBadRequest(ctx), nil
	}
	session, problem, err := handler.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.UpdatePromotionProductPolicy401ApplicationProblemPlusJSONResponse(*problem), nil
		}
		return generated.UpdatePromotionProductPolicy403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	body := request.Body
	policy, err := handler.promotionAdministration.UpdateProductPolicy(ctx, staffActor(session), promotions.UpdateProductPolicyCommand{
		RequestID: request.Params.IdempotencyKey, ExpectedRevision: body.ExpectedRevision,
		PromotionEnabled: body.PromotionEnabled, StickyEnabled: body.StickyEnabled,
		FreePricePerDay: body.FreePricePerDay, DoubleUploadPricePerDay: body.DoubleUploadPricePerDay,
		DoubleUploadFreePricePerDay:         body.DoubleUploadFreePricePerDay,
		HalfDownloadPricePerDay:             body.HalfDownloadPricePerDay,
		DoubleUploadHalfDownloadPricePerDay: body.DoubleUploadHalfDownloadPricePerDay,
		ThirtyPercentDownloadPricePerDay:    body.ThirtyPercentDownloadPricePerDay,
		StickyPricePerDay:                   body.StickyPricePerDay,
		MaxPromotionDays:                    body.MaxPromotionDays, MaxStickyDays: body.MaxStickyDays, Reason: body.Reason,
	})
	switch {
	case errors.Is(err, promotions.ErrInput):
		return promotionProductPolicyBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "promotion_product_update_denied", "无法修改付费促销规则", "当前后台身份没有 promotion.schedule 权限。")
		return generated.UpdatePromotionProductPolicy403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, promotions.ErrVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "promotion_product_policy_version_conflict", "规则版本已经变化", "请刷新页面后基于最新规则重新修改。")
		return generated.UpdatePromotionProductPolicy409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, promotions.ErrNoChange):
		problem := newProblemFromContext(ctx, http.StatusConflict, "promotion_product_policy_unchanged", "规则没有变化", "请至少修改一项设置后再保存。")
		return generated.UpdatePromotionProductPolicy409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, promotions.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "promotion_product_policy_idempotency_conflict", "请求标识已被使用", "请刷新规则后重新操作。")
		return generated.UpdatePromotionProductPolicy409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.UpdatePromotionProductPolicy200JSONResponse(promotionProductPolicyDTO(policy)), nil
}

func promotionProductOfferDTO(offer promotions.ProductOffer) generated.PromotionProductOffer {
	dto := generated.PromotionProductOffer{
		TorrentId: offer.TorrentID, TorrentTitle: offer.TorrentTitle,
		MagicBalance: strconv.FormatInt(offer.MagicBalance, 10), Policy: promotionProductPolicyDTO(offer.Policy),
	}
	if offer.ActivePromotion != nil {
		value := generated.PromotionProductOfferCurrentPromotion(*offer.ActivePromotion)
		dto.CurrentPromotion = &value
	}
	if offer.PromotionWindow != nil {
		dto.PromotionStartsAt = &offer.PromotionWindow.StartsAt
		dto.PromotionEndsAt = &offer.PromotionWindow.EndsAt
	}
	if offer.StickyWindow != nil {
		dto.StickyStartsAt = &offer.StickyWindow.StartsAt
		dto.StickyEndsAt = &offer.StickyWindow.EndsAt
	}
	return dto
}

func promotionProductPolicyDTO(policy promotions.ProductPolicy) generated.PromotionProductPolicy {
	return generated.PromotionProductPolicy{
		Revision: policy.Revision, EffectiveFrom: policy.EffectiveFrom,
		PromotionEnabled: policy.PromotionEnabled, StickyEnabled: policy.StickyEnabled,
		FreePricePerDay:                     strconv.FormatInt(policy.FreePricePerDay, 10),
		DoubleUploadPricePerDay:             strconv.FormatInt(policy.DoubleUploadPricePerDay, 10),
		DoubleUploadFreePricePerDay:         strconv.FormatInt(policy.DoubleUploadFreePricePerDay, 10),
		HalfDownloadPricePerDay:             strconv.FormatInt(policy.HalfDownloadPricePerDay, 10),
		DoubleUploadHalfDownloadPricePerDay: strconv.FormatInt(policy.DoubleUploadHalfDownloadPricePerDay, 10),
		ThirtyPercentDownloadPricePerDay:    strconv.FormatInt(policy.ThirtyPercentDownloadPricePerDay, 10),
		StickyPricePerDay:                   strconv.FormatInt(policy.StickyPricePerDay, 10),
		MaxPromotionDays:                    policy.MaxPromotionDays, MaxStickyDays: policy.MaxStickyDays,
	}
}

func promotionProductOrderDTO(order promotions.ProductOrder) generated.PromotionProductOrder {
	dto := generated.PromotionProductOrder{
		Id: order.ID, BuyerNumericId: order.BuyerNumericID, BuyerUsername: order.BuyerUsername,
		TorrentId: order.TorrentID, TorrentTitle: order.TorrentTitle,
		TotalPrice: strconv.FormatInt(order.TotalPrice, 10), PolicyRevision: order.PolicyRevision,
		BalanceAfter: strconv.FormatInt(order.BalanceAfter, 10), PurchasedAt: order.PurchasedAt, Replayed: order.Replayed,
	}
	if order.CampaignID != nil {
		dto.CampaignId = order.CampaignID
	}
	if order.Promotion != nil && order.PromotionWindow != nil {
		value := generated.PromotionProductOrderPromotion(*order.Promotion)
		dto.Promotion = &value
		dto.PromotionDays = &order.PromotionDays
		price := strconv.FormatInt(order.PromotionUnitPrice, 10)
		dto.PromotionUnitPrice = &price
		dto.PromotionStartsAt = &order.PromotionWindow.StartsAt
		dto.PromotionEndsAt = &order.PromotionWindow.EndsAt
	}
	if order.StickyDays > 0 && order.StickyWindow != nil {
		dto.StickyDays = &order.StickyDays
		price := strconv.FormatInt(order.StickyUnitPrice, 10)
		dto.StickyUnitPrice = &price
		dto.StickyStartsAt = &order.StickyWindow.StartsAt
		dto.StickyEndsAt = &order.StickyWindow.EndsAt
	}
	return dto
}

func promotionProductOrderPageDTO(page promotions.ProductOrderPage) generated.PromotionProductOrderPage {
	items := make([]generated.PromotionProductOrder, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, promotionProductOrderDTO(item))
	}
	return generated.PromotionProductOrderPage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}

func promotionProductPurchaseBadRequest(ctx context.Context) generated.PurchasePromotionProducts400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_promotion_product_purchase", "促销购买请求无效", "请选择优惠、置顶或二者，并填写规则允许的天数。")
	return generated.PurchasePromotionProducts400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func promotionProductPolicyBadRequest(ctx context.Context) generated.UpdatePromotionProductPolicy400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_promotion_product_policy", "付费促销规则无效", "单价须为零至一百万整数魔力值，天数须为 1 至 30，并填写至少 10 个字符的原因。")
	return generated.UpdatePromotionProductPolicy400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}
