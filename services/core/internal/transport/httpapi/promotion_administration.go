package httpapi

import (
	"context"
	"errors"
	"net/http"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/promotions"
)

func (h *Handler) ListPromotionCampaigns(ctx context.Context, request generated.ListPromotionCampaignsRequestObject) (generated.ListPromotionCampaignsResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ListPromotionCampaigns401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.ListPromotionCampaigns403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	limit, offset := promotions.DefaultListLimit, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.promotionAdministration.List(ctx, staffActor(session), limit, offset)
	switch {
	case errors.Is(err, promotions.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_promotion_query", "优惠政策查询无效", "请检查分页参数。")
		return generated.ListPromotionCampaigns400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "promotion_read_denied", "无法查看优惠政策", "当前后台身份没有 promotion.manage.read 权限。")
		return generated.ListPromotionCampaigns403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.PromotionCampaign, 0, len(page.Items))
	for _, campaign := range page.Items {
		items = append(items, promotionCampaignDTO(campaign))
	}
	return generated.ListPromotionCampaigns200JSONResponse{Items: items, Total: page.Total, Limit: limit, Offset: offset}, nil
}

func (h *Handler) SchedulePromotionCampaign(ctx context.Context, request generated.SchedulePromotionCampaignRequestObject) (generated.SchedulePromotionCampaignResponseObject, error) {
	if request.Body == nil {
		return schedulePromotionBadRequest(ctx), nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.SchedulePromotionCampaign401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.SchedulePromotionCampaign403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	campaign, err := h.promotionAdministration.Schedule(ctx, staffActor(session), promotions.ScheduleInput{
		CampaignID: request.Params.IdempotencyKey, Scope: promotions.Scope(request.Body.Scope),
		TorrentID: request.Body.TorrentId, Promotion: promotions.Promotion(request.Body.Promotion),
		StartsAt: request.Body.StartsAt, EndsAt: request.Body.EndsAt, Reason: request.Body.Reason,
	})
	switch {
	case errors.Is(err, promotions.ErrInput):
		return schedulePromotionBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "promotion_schedule_denied", "无法签发优惠政策", "当前后台身份没有 promotion.schedule 权限。")
		return generated.SchedulePromotionCampaign403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, promotions.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "promotion_torrent_not_found", "没有找到目标种子", "请确认数字种子 ID 后重试。")
		return generated.SchedulePromotionCampaign404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, promotions.ErrTorrentUnavailable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "promotion_torrent_unavailable", "目标种子当前不可促销", "只有已发布种子可以签发单种子优惠。")
		return generated.SchedulePromotionCampaign409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, promotions.ErrScopeOverlap):
		problem := newProblemFromContext(ctx, http.StatusConflict, "promotion_scope_overlap", "优惠时间段发生重叠", "同一作用域已有活动覆盖该时间段；请调整开始或结束时间。")
		return generated.SchedulePromotionCampaign409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, promotions.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "promotion_idempotency_conflict", "操作请求内容已经变化", "请勿复用其他优惠操作的 Idempotency-Key。")
		return generated.SchedulePromotionCampaign409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.SchedulePromotionCampaign201JSONResponse(promotionCampaignDTO(campaign)), nil
}

func schedulePromotionBadRequest(ctx context.Context) generated.SchedulePromotionCampaign400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_promotion_campaign", "优惠政策参数无效", "开始时间不能早于当前时间，持续时间须为 5 分钟至 30 天，并填写至少 10 个字符的原因。")
	return generated.SchedulePromotionCampaign400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func promotionCampaignDTO(campaign promotions.Campaign) generated.PromotionCampaign {
	return generated.PromotionCampaign{
		Id: campaign.ID, Source: generated.PromotionCampaignSource(campaign.Source), Scope: generated.PromotionScope(campaign.Scope), TorrentId: campaign.TorrentID,
		TorrentTitle: campaign.TorrentTitle, Promotion: generated.PromotionType(campaign.Promotion),
		StartsAt: campaign.StartsAt, EndsAt: campaign.EndsAt, OverrideLowerScopes: campaign.OverrideLowerScopes,
		Reason: campaign.Reason, CreatedAt: campaign.CreatedAt,
		DeliveryState:    generated.PromotionCampaignDeliveryState(campaign.DeliveryState),
		DeliveryAttempts: int(campaign.DeliveryAttempts), LastDeliveryError: campaign.LastDeliveryError,
		DeliveredAt: campaign.DeliveredAt, TimelineState: generated.PromotionCampaignTimelineState(campaign.TimelineState),
	}
}
