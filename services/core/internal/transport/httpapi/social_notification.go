package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/social"
)

func (h *Handler) ListSocialNotifications(ctx context.Context, request api.ListSocialNotificationsRequestObject) (api.ListSocialNotificationsResponseObject, error) {
	limit := social.DefaultSocialNotificationLimit
	offset := 0
	category := social.SocialNotificationAll
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	if request.Params.Category != nil {
		category = social.SocialNotificationCategory(*request.Params.Category)
	}
	page, err := h.socialPosts.ListNotifications(ctx, sessionTokenFromContext(ctx), social.SocialNotificationQuery{
		Category: category, Limit: limit, Offset: offset,
	})
	switch {
	case errors.Is(err, social.ErrSocialNotificationInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_social_notification_query", "通知查询无效", "请检查通知分类和分页参数。")
		return api.ListSocialNotifications400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: api.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		return api.ListSocialNotifications401ApplicationProblemPlusJSONResponse(newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请登录后查看动态圈通知。")), nil
	case errors.Is(err, authz.ErrForbidden):
		return api.ListSocialNotifications403ApplicationProblemPlusJSONResponse(newProblemFromContext(ctx, http.StatusForbidden, "social_notification_read_denied", "无法查看动态圈通知", "当前账号暂时不能查看动态圈互动通知。")), nil
	case err != nil:
		return nil, err
	}
	return api.ListSocialNotifications200JSONResponse(socialNotificationPageDTO(page)), nil
}

func (h *Handler) GetSocialNotificationSummary(ctx context.Context, _ api.GetSocialNotificationSummaryRequestObject) (api.GetSocialNotificationSummaryResponseObject, error) {
	summary, err := h.socialPosts.NotificationSummary(ctx, sessionTokenFromContext(ctx))
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请登录后查看动态圈通知。")
		return api.GetSocialNotificationSummary401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: api.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		return api.GetSocialNotificationSummary403ApplicationProblemPlusJSONResponse(newProblemFromContext(ctx, http.StatusForbidden, "social_notification_read_denied", "无法查看动态圈通知", "当前账号暂时不能查看动态圈互动通知。")), nil
	case err != nil:
		return nil, err
	}
	return api.GetSocialNotificationSummary200JSONResponse{UnreadCount: summary.UnreadCount}, nil
}

func (h *Handler) MarkSocialNotificationRead(ctx context.Context, request api.MarkSocialNotificationReadRequestObject) (api.MarkSocialNotificationReadResponseObject, error) {
	receipt, err := h.socialPosts.MarkNotificationRead(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), request.NotificationId)
	switch {
	case errors.Is(err, social.ErrSocialNotificationInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_social_notification", "通知无效", "通知标识无效。")
		return api.MarkSocialNotificationRead400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: api.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		return api.MarkSocialNotificationRead401ApplicationProblemPlusJSONResponse(newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后重试。")), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		return api.MarkSocialNotificationRead403ApplicationProblemPlusJSONResponse(newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")), nil
	case errors.Is(err, authz.ErrForbidden):
		return api.MarkSocialNotificationRead403ApplicationProblemPlusJSONResponse(newProblemFromContext(ctx, http.StatusForbidden, "social_notification_write_denied", "无法更新通知", "当前账号暂时不能更新动态圈通知。")), nil
	case errors.Is(err, social.ErrSocialNotificationNotFound):
		return api.MarkSocialNotificationRead404ApplicationProblemPlusJSONResponse(newProblemFromContext(ctx, http.StatusNotFound, "social_notification_not_found", "通知不存在", "该通知不存在或不属于当前账号。")), nil
	case err != nil:
		return nil, err
	}
	return api.MarkSocialNotificationRead200JSONResponse{
		NotificationId: receipt.NotificationID, ReadAt: receipt.ReadAt, AlreadyRead: receipt.AlreadyRead,
	}, nil
}

func (h *Handler) MarkAllSocialNotificationsRead(ctx context.Context, request api.MarkAllSocialNotificationsReadRequestObject) (api.MarkAllSocialNotificationsReadResponseObject, error) {
	receipt, err := h.socialPosts.MarkAllNotificationsRead(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken))
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后重试。")
		return api.MarkAllSocialNotificationsRead401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: api.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		return api.MarkAllSocialNotificationsRead403ApplicationProblemPlusJSONResponse(newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")), nil
	case errors.Is(err, authz.ErrForbidden):
		return api.MarkAllSocialNotificationsRead403ApplicationProblemPlusJSONResponse(newProblemFromContext(ctx, http.StatusForbidden, "social_notification_write_denied", "无法更新通知", "当前账号暂时不能更新动态圈通知。")), nil
	case err != nil:
		return nil, err
	}
	return api.MarkAllSocialNotificationsRead200JSONResponse{UpdatedCount: receipt.UpdatedCount, ReadAt: receipt.ReadAt}, nil
}

func socialNotificationPageDTO(page social.SocialNotificationPage) api.SocialNotificationPage {
	items := make([]api.SocialNotification, 0, len(page.Items))
	for _, item := range page.Items {
		var postPreview, commentPreview *string
		if item.PostPreview != "" {
			value := item.PostPreview
			postPreview = &value
		}
		if item.CommentPreview != "" {
			value := item.CommentPreview
			commentPreview = &value
		}
		items = append(items, api.SocialNotification{
			Id: item.ID, Kind: api.SocialNotificationKind(item.Kind), Actor: socialPostAuthorDTO(item.Actor),
			PostId: item.PostID, CommentId: item.CommentID, PostPreview: postPreview, CommentPreview: commentPreview,
			CreatedAt: item.CreatedAt, ReadAt: item.ReadAt,
		})
	}
	return api.SocialNotificationPage{Items: items, Total: page.Total, UnreadCount: page.UnreadCount, Limit: page.Limit, Offset: page.Offset}
}
