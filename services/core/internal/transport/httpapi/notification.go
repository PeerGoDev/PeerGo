package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/notifications"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func (h *Handler) ListMyNotifications(ctx context.Context, request generated.ListMyNotificationsRequestObject) (generated.ListMyNotificationsResponseObject, error) {
	limit := notifications.DefaultLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	offset := 0
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	unreadOnly := request.Params.UnreadOnly != nil && *request.Params.UnreadOnly
	page, err := h.notifications.List(ctx, sessionTokenFromContext(ctx), notifications.ListQuery{
		Limit: limit, Offset: offset, UnreadOnly: unreadOnly,
	})
	switch {
	case errors.Is(err, notifications.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_notification_query", "通知查询无效", "每页数量必须在 1 到 50 之间，偏移量必须在 0 到 99999 之间。")
		return generated.ListMyNotifications400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看消息。")
		return generated.ListMyNotifications401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "notification_read_denied", "无法查看消息", "当前账号暂时不能查看站内通知。")
		return generated.ListMyNotifications403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListMyNotifications200JSONResponse{
		Body:    notificationPageDTO(page),
		Headers: generated.ListMyNotifications200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (h *Handler) GetMyNotificationSummary(ctx context.Context, _ generated.GetMyNotificationSummaryRequestObject) (generated.GetMyNotificationSummaryResponseObject, error) {
	summary, err := h.notifications.Summary(ctx, sessionTokenFromContext(ctx))
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看消息。")
		return generated.GetMyNotificationSummary401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "notification_read_denied", "无法查看消息", "当前账号暂时不能查看站内通知。")
		return generated.GetMyNotificationSummary403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyNotificationSummary200JSONResponse{
		Body:    generated.MyNotificationSummary{UnreadCount: summary.UnreadCount},
		Headers: generated.GetMyNotificationSummary200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (h *Handler) MarkMyNotificationRead(ctx context.Context, request generated.MarkMyNotificationReadRequestObject) (generated.MarkMyNotificationReadResponseObject, error) {
	receipt, err := h.notifications.MarkRead(
		ctx,
		sessionTokenFromContext(ctx),
		string(request.Params.XCSRFToken),
		request.NotificationId,
	)
	switch {
	case errors.Is(err, notifications.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_notification", "消息标识无效", "请刷新页面后重试。")
		return generated.MarkMyNotificationRead400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后更新消息状态。")
		return generated.MarkMyNotificationRead401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.MarkMyNotificationRead403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "notification_write_denied", "无法更新消息", "当前账号暂时不能修改通知已读状态。")
		return generated.MarkMyNotificationRead403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, notifications.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "notification_not_found", "消息不存在", "该消息不存在或不属于当前账号。")
		return generated.MarkMyNotificationRead404ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.MarkMyNotificationRead200JSONResponse{
		Body: generated.MyNotificationReadReceipt{
			NotificationId: receipt.NotificationID, ReadAt: receipt.ReadAt, AlreadyRead: receipt.AlreadyRead,
		},
		Headers: generated.MarkMyNotificationRead200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (h *Handler) MarkAllMyNotificationsRead(ctx context.Context, request generated.MarkAllMyNotificationsReadRequestObject) (generated.MarkAllMyNotificationsReadResponseObject, error) {
	receipt, err := h.notifications.MarkAllRead(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken))
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后更新消息状态。")
		return generated.MarkAllMyNotificationsRead401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.MarkAllMyNotificationsRead403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "notification_write_denied", "无法更新消息", "当前账号暂时不能修改通知已读状态。")
		return generated.MarkAllMyNotificationsRead403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.MarkAllMyNotificationsRead200JSONResponse{
		Body:    generated.MyNotificationReadAllReceipt{UpdatedCount: receipt.UpdatedCount, ReadAt: receipt.ReadAt},
		Headers: generated.MarkAllMyNotificationsRead200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (h *Handler) ArchiveAllMyNotifications(ctx context.Context, request generated.ArchiveAllMyNotificationsRequestObject) (generated.ArchiveAllMyNotificationsResponseObject, error) {
	receipt, err := h.notifications.ArchiveAll(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken))
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后归档消息。")
		return generated.ArchiveAllMyNotifications401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.ArchiveAllMyNotifications403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "notification_archive_denied", "无法清空消息", "当前账号暂时不能归档站内通知。")
		return generated.ArchiveAllMyNotifications403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ArchiveAllMyNotifications200JSONResponse{
		Body:    generated.MyNotificationArchiveAllReceipt{UpdatedCount: receipt.UpdatedCount, ArchivedAt: receipt.ArchivedAt},
		Headers: generated.ArchiveAllMyNotifications200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (h *Handler) CreateMyNotificationFeedback(ctx context.Context, request generated.CreateMyNotificationFeedbackRequestObject) (generated.CreateMyNotificationFeedbackResponseObject, error) {
	receipt, err := h.notifications.CreateFeedback(
		ctx,
		sessionTokenFromContext(ctx),
		string(request.Params.XCSRFToken),
		notifications.CreateFeedbackInput{Title: request.Body.Title, Content: request.Body.Content},
	)
	switch {
	case errors.Is(err, notifications.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_notification_feedback", "反馈内容无效", "标题需为 1 到 100 个字符，内容需为 1 到 2000 个字符。")
		return generated.CreateMyNotificationFeedback400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后提交反馈。")
		return generated.CreateMyNotificationFeedback401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.CreateMyNotificationFeedback403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "notification_feedback_denied", "无法提交反馈", "当前账号暂时不能联系站点管理员。")
		return generated.CreateMyNotificationFeedback403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.CreateMyNotificationFeedback201JSONResponse{
		Body:    generated.MyNotificationFeedbackReceipt{FeedbackId: receipt.FeedbackID, CreatedAt: receipt.CreatedAt},
		Headers: generated.CreateMyNotificationFeedback201ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func notificationPageDTO(page notifications.Page) generated.MyNotificationPage {
	items := make([]generated.MyNotification, 0, len(page.Items))
	for _, item := range page.Items {
		dto := generated.MyNotification{
			Id: item.ID, Kind: generated.MyNotificationKind(item.Kind),
			CreatedAt: item.CreatedAt, ReadAt: item.ReadAt,
		}
		switch item.Kind {
		case notifications.KindTorrentReview:
			torrentID := int64(item.TorrentReview.TorrentID)
			outcome := notificationOutcomeDTO(item.TorrentReview.Outcome)
			reasonCode := generated.TorrentReviewReasonCode(item.TorrentReview.ReasonCode)
			dto.TorrentId = &torrentID
			dto.TorrentTitle = &item.TorrentReview.TorrentTitle
			dto.Outcome = &outcome
			dto.ReasonCode = &reasonCode
			dto.Reason = &item.TorrentReview.Reason
		case notifications.KindRatioWatch:
			status := generated.MyRatioWatchNotificationStatus(item.RatioWatch.Status)
			dto.RatioWatchStatus = &status
			dto.RatioBasisPoints = &item.RatioWatch.RatioBasisPoints
			dto.MinimumRatioBasisPoints = &item.RatioWatch.MinimumRatioBasisPoints
			dto.RestrictionRatioBasisPoints = &item.RatioWatch.RestrictionRatioBasisPoints
			dto.DeadlineAt = &item.RatioWatch.DeadlineAt
		case notifications.KindRatioAppeal:
			status := generated.MyRatioWatchAppealNotificationStatus(item.RatioAppeal.Status)
			dto.RatioAppealStatus = &status
			dto.RatioAppealResponse = &item.RatioAppeal.Response
		case notifications.KindHNR:
			torrentID := int64(item.HNR.TorrentID)
			status := generated.MyHNRNotificationStatus(item.HNR.Event)
			dto.TorrentId = &torrentID
			dto.TorrentTitle = &item.HNR.TorrentTitle
			dto.HnrStatus = &status
			dto.HnrGraceEndsAt = &item.HNR.GraceEndsAt
		case notifications.KindHNRAppeal:
			torrentID := int64(item.HNR.TorrentID)
			status := generated.MyHNRNotificationStatus(item.HNR.Event)
			dto.TorrentId = &torrentID
			dto.TorrentTitle = &item.HNR.TorrentTitle
			dto.HnrStatus = &status
			dto.HnrGraceEndsAt = &item.HNR.GraceEndsAt
			dto.HnrAppealResponse = &item.HNR.Response
		case notifications.KindWorkgroupContribution:
			payload := item.WorkgroupContribution
			kind := generated.WorkgroupNotificationKind(payload.GroupKind)
			metric := generated.WorkgroupNotificationMetric(payload.Metric)
			evidenceState := generated.MyNotificationWorkgroupEvidenceState(payload.EvidenceState)
			assessmentState := generated.MyNotificationWorkgroupAssessmentState(payload.AssessmentState)
			explanationCode := generated.MyNotificationWorkgroupExplanationCode(payload.ExplanationCode)
			dto.WorkgroupKind = &kind
			dto.WorkgroupMetric = &metric
			dto.WorkgroupPolicyRevision = &payload.PolicyRevision
			dto.WorkgroupPeriodStartsAt = &payload.PeriodStartsAt
			dto.WorkgroupPeriodEndsAt = &payload.PeriodEndsAt
			dto.WorkgroupObservedAt = &payload.ObservedAt
			dto.WorkgroupEvidenceState = &evidenceState
			dto.WorkgroupCurrentValue = &payload.CurrentValue
			dto.WorkgroupTargetValue = &payload.TargetValue
			dto.WorkgroupAssessmentState = &assessmentState
			dto.WorkgroupExplanationCode = &explanationCode
			dto.WorkgroupReason = &payload.Reason
		case notifications.KindMemberGift:
			payload := item.MemberGift
			senderNumericID := generated.UnsignedIntegerText(strconv.FormatInt(payload.SenderNumericID, 10))
			netAmount := generated.UnsignedIntegerText(strconv.FormatInt(payload.NetAmount, 10))
			dto.MemberGiftSenderNumericId = &senderNumericID
			dto.MemberGiftSenderUsername = &payload.SenderUsername
			dto.MemberGiftSenderDisplayName = &payload.SenderDisplayName
			dto.MemberGiftNetAmount = &netAmount
			dto.MemberGiftMessage = &payload.Message
		case notifications.KindContentTip:
			payload := item.ContentTip
			senderNumericID := generated.UnsignedIntegerText(strconv.FormatInt(payload.SenderNumericID, 10))
			netAmount := generated.UnsignedIntegerText(strconv.FormatInt(payload.NetAmount, 10))
			targetKind := generated.MyNotificationContentTipTargetKind(payload.Target.Kind)
			dto.ContentTipSenderNumericId = &senderNumericID
			dto.ContentTipSenderUsername = &payload.SenderUsername
			dto.ContentTipSenderDisplayName = &payload.SenderDisplayName
			dto.ContentTipNetAmount = &netAmount
			dto.ContentTipTargetKind = &targetKind
			dto.ContentTipTargetTitle = &payload.Target.Title
			switch payload.Target.Kind {
			case "torrent":
				dto.ContentTipTorrentId = &payload.Target.TorrentID
			case "post":
				dto.ContentTipPostId = &payload.Target.PostID
			case "comment":
				dto.ContentTipCommentId = &payload.Target.CommentID
			}
		}
		items = append(items, dto)
	}
	return generated.MyNotificationPage{
		Items: items, Total: page.Total, UnreadCount: page.UnreadCount,
		Limit: page.Limit, Offset: page.Offset,
	}
}

func notificationOutcomeDTO(outcome torrents.State) generated.MyNotificationOutcome {
	switch outcome {
	case torrents.StatePublished:
		return generated.MyNotificationOutcomePublished
	case torrents.StateRejected:
		return generated.MyNotificationOutcomeRejected
	default:
		return generated.MyNotificationOutcome(outcome)
	}
}
