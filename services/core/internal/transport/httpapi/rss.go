package httpapi

import (
	"bytes"
	"context"
	"errors"
	"mime"
	"net/http"
	"strings"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/torrentpurchase"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/rss"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func (handler *Handler) ListMyRSSSubscriptions(ctx context.Context, _ generated.ListMyRSSSubscriptionsRequestObject) (generated.ListMyRSSSubscriptionsResponseObject, error) {
	items, err := handler.rss.List(ctx, sessionTokenFromContext(ctx))
	if errors.Is(err, identity.ErrSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "web_session_required", "需要登录", "请登录后管理 RSS 订阅。")
		return generated.ListMyRSSSubscriptions401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "rss_subscription_read_denied", "无法查看 RSS 订阅", "当前账户没有 rss.subscription.read.self。")
		return generated.ListMyRSSSubscriptions403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	dtos := make([]generated.RSSSubscription, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, rssSubscriptionDTO(item))
	}
	return generated.ListMyRSSSubscriptions200JSONResponse{Items: dtos}, nil
}

func (handler *Handler) CreateMyRSSSubscription(ctx context.Context, request generated.CreateMyRSSSubscriptionRequestObject) (generated.CreateMyRSSSubscriptionResponseObject, error) {
	if request.Body == nil {
		problem := rssInputProblem(ctx)
		return generated.CreateMyRSSSubscription400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	result, err := handler.rss.Create(ctx, sessionTokenFromContext(ctx), request.Params.XCSRFToken, rssInputFromDTO(*request.Body))
	if errors.Is(err, rss.ErrInvalidInput) {
		problem := rssInputProblem(ctx)
		return generated.CreateMyRSSSubscription400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	if errors.Is(err, identity.ErrSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "web_session_required", "需要登录", "请重新登录后创建 RSS 订阅。")
		return generated.CreateMyRSSSubscription401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrInvalidCSRF) || errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "rss_subscription_manage_denied", "无法创建 RSS 订阅", "当前会话无效或账户没有 rss.subscription.manage.self。")
		return generated.CreateMyRSSSubscription403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, rss.ErrSubscriptionLimit) {
		problem := newProblemFromContext(ctx, http.StatusConflict, "rss_subscription_limit", "RSS 订阅数量已达上限", "请先撤销不再使用的订阅，或联系管理员调整全站上限。")
		return generated.CreateMyRSSSubscription409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.CreateMyRSSSubscription201JSONResponse(issuedRSSSubscriptionDTO(result)), nil
}

func (handler *Handler) UpdateMyRSSSubscription(ctx context.Context, request generated.UpdateMyRSSSubscriptionRequestObject) (generated.UpdateMyRSSSubscriptionResponseObject, error) {
	if request.Body == nil {
		problem := rssInputProblem(ctx)
		return generated.UpdateMyRSSSubscription400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	input := rssInputFromUpdateDTO(*request.Body)
	input.ID = request.SubscriptionId
	input.ExpectedVersion = request.Body.ExpectedVersion
	result, err := handler.rss.Update(ctx, sessionTokenFromContext(ctx), request.Params.XCSRFToken, input)
	if errors.Is(err, rss.ErrInvalidInput) {
		problem := rssInputProblem(ctx)
		return generated.UpdateMyRSSSubscription400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	if errors.Is(err, identity.ErrSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "web_session_required", "需要登录", "请重新登录后修改 RSS 订阅。")
		return generated.UpdateMyRSSSubscription401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrInvalidCSRF) || errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "rss_subscription_manage_denied", "无法修改 RSS 订阅", "当前会话无效或账户没有 rss.subscription.manage.self。")
		return generated.UpdateMyRSSSubscription403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, rss.ErrSubscriptionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "rss_subscription_not_found", "RSS 订阅不存在", "该订阅不存在或已经撤销。")
		return generated.UpdateMyRSSSubscription404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, rss.ErrSubscriptionConflict) {
		problem := rssConflictProblem(ctx)
		return generated.UpdateMyRSSSubscription409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.UpdateMyRSSSubscription200JSONResponse(rssSubscriptionDTO(result)), nil
}

func (handler *Handler) RotateMyRSSSubscriptionToken(ctx context.Context, request generated.RotateMyRSSSubscriptionTokenRequestObject) (generated.RotateMyRSSSubscriptionTokenResponseObject, error) {
	if request.Body == nil {
		problem := rssInputProblem(ctx)
		return generated.RotateMyRSSSubscriptionToken400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	result, err := handler.rss.Rotate(ctx, sessionTokenFromContext(ctx), request.Params.XCSRFToken, rss.SubscriptionVersionInput{ID: request.SubscriptionId, ExpectedVersion: request.Body.ExpectedVersion})
	if errors.Is(err, rss.ErrInvalidInput) {
		problem := rssInputProblem(ctx)
		return generated.RotateMyRSSSubscriptionToken400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	if errors.Is(err, identity.ErrSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "web_session_required", "需要登录", "请重新登录后轮换 RSS 令牌。")
		return generated.RotateMyRSSSubscriptionToken401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrInvalidCSRF) || errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "rss_subscription_manage_denied", "无法轮换 RSS 令牌", "当前会话无效或账户没有 rss.subscription.manage.self。")
		return generated.RotateMyRSSSubscriptionToken403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, rss.ErrSubscriptionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "rss_subscription_not_found", "RSS 订阅不存在", "该订阅不存在或已经撤销。")
		return generated.RotateMyRSSSubscriptionToken404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, rss.ErrSubscriptionConflict) {
		problem := rssConflictProblem(ctx)
		return generated.RotateMyRSSSubscriptionToken409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.RotateMyRSSSubscriptionToken201JSONResponse(issuedRSSSubscriptionDTO(result)), nil
}

func (handler *Handler) DeleteMyRSSSubscription(ctx context.Context, request generated.DeleteMyRSSSubscriptionRequestObject) (generated.DeleteMyRSSSubscriptionResponseObject, error) {
	err := handler.rss.Revoke(ctx, sessionTokenFromContext(ctx), request.Params.XCSRFToken, rss.SubscriptionVersionInput{ID: request.SubscriptionId, ExpectedVersion: request.Params.ExpectedVersion})
	if errors.Is(err, rss.ErrInvalidInput) {
		problem := rssInputProblem(ctx)
		return generated.DeleteMyRSSSubscription400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	if errors.Is(err, identity.ErrSessionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "web_session_required", "需要登录", "请重新登录后撤销 RSS 订阅。")
		return generated.DeleteMyRSSSubscription401ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, identity.ErrInvalidCSRF) || errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "rss_subscription_manage_denied", "无法撤销 RSS 订阅", "当前会话无效或账户没有 rss.subscription.manage.self。")
		return generated.DeleteMyRSSSubscription403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, rss.ErrSubscriptionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "rss_subscription_not_found", "RSS 订阅不存在", "该订阅不存在或已经撤销。")
		return generated.DeleteMyRSSSubscription404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, rss.ErrSubscriptionConflict) {
		problem := rssConflictProblem(ctx)
		return generated.DeleteMyRSSSubscription409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.DeleteMyRSSSubscription204Response{}, nil
}

func (handler *Handler) GetRSSFeed(ctx context.Context, request generated.GetRSSFeedRequestObject) (generated.GetRSSFeedResponseObject, error) {
	document, err := handler.rss.Feed(ctx, request.RssToken)
	if errors.Is(err, rss.ErrTokenInvalid) || errors.Is(err, rss.ErrSubscriptionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "rss_feed_not_found", "RSS 订阅不可用", "私密地址无效、已撤销或所属账户当前不可用。")
		return generated.GetRSSFeed404ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	if errors.Is(err, rss.ErrRateLimited) {
		problem := newProblemFromContext(ctx, http.StatusTooManyRequests, "rss_rate_limited", "RSS 请求过于频繁", "同一账户的所有 RSS 订阅共用请求额度，请降低刷新频率。")
		return generated.GetRSSFeed429ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	if request.Params.IfNoneMatch != nil && strings.TrimSpace(*request.Params.IfNoneMatch) == document.ETag {
		return generated.GetRSSFeed304Response{Headers: generated.GetRSSFeed304ResponseHeaders{ETag: document.ETag, CacheControl: "private, no-cache", ReferrerPolicy: "no-referrer"}}, nil
	}
	return generated.GetRSSFeed200ApplicationrssXmlResponse{Body: bytes.NewReader(document.Data), Headers: generated.GetRSSFeed200ResponseHeaders{ETag: document.ETag, LastModified: document.LastModified.Format(http.TimeFormat), CacheControl: "private, no-cache", ReferrerPolicy: "no-referrer"}, ContentLength: int64(len(document.Data))}, nil
}

func (handler *Handler) DownloadTorrentFromRSS(ctx context.Context, request generated.DownloadTorrentFromRSSRequestObject) (generated.DownloadTorrentFromRSSResponseObject, error) {
	result, err := handler.rss.Download(ctx, request.RssToken, request.TorrentId)
	if errors.Is(err, rss.ErrTokenInvalid) || errors.Is(err, rss.ErrSubscriptionNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "rss_torrent_not_found", "RSS 种子不可用", "该私密订阅当前不包含这个种子，或订阅已经撤销。")
		return generated.DownloadTorrentFromRSS404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, rss.ErrRateLimited) {
		problem := newProblemFromContext(ctx, http.StatusTooManyRequests, "rss_rate_limited", "RSS 请求过于频繁", "同一账户的所有 RSS 订阅共用请求额度，请降低刷新频率。")
		return generated.DownloadTorrentFromRSS429ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, torrentpurchase.ErrPurchaseRequired) {
		problem := newProblemFromContext(ctx, http.StatusPaymentRequired, "torrent_purchase_required", "需要购买种子", "请先在站内使用魔力值购买该种子的永久下载权限。")
		return generated.DownloadTorrentFromRSS402ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	if errors.Is(err, torrents.ErrTorrentDownloadRestricted) || errors.Is(err, torrents.ErrTorrentDownloadEmailUnverified) || errors.Is(err, torrentpurchase.ErrPurchaseDisabled) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "rss_torrent_download_denied", "当前账户无法下载", "请登录站点查看邮箱验证、下载限制或购买状态。")
		return generated.DownloadTorrentFromRSS403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, torrents.ErrTorrentDownloadNotFound) {
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不存在", "该种子不存在或当前未发布。")
		return generated.DownloadTorrentFromRSS404ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": result.Filename})
	if disposition == "" {
		return nil, errors.New("format rss torrent download filename")
	}
	return generated.DownloadTorrentFromRSS200ApplicationxBittorrentResponse{Body: bytes.NewReader(result.Data), Headers: generated.DownloadTorrentFromRSS200ResponseHeaders{CacheControl: "no-store", ContentDisposition: disposition, ReferrerPolicy: "no-referrer"}, ContentLength: int64(len(result.Data))}, nil
}

func (handler *Handler) GetRSSSettings(ctx context.Context, _ generated.GetRSSSettingsRequestObject) (generated.GetRSSSettingsResponseObject, error) {
	session, problem, err := handler.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if problem != nil {
		if problem.Status == http.StatusUnauthorized {
			return generated.GetRSSSettings401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*problem)}, nil
		}
		return generated.GetRSSSettings403ApplicationProblemPlusJSONResponse(*problem), nil
	}
	result, err := handler.rss.Settings(ctx, staffActor(session))
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "rss_settings_read_denied", "无法查看 RSS 设置", "当前后台身份没有 rss.settings.manage.read。")
		return generated.GetRSSSettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.GetRSSSettings200JSONResponse(rssSettingsDTO(result)), nil
}

func (handler *Handler) UpdateRSSSettings(ctx context.Context, request generated.UpdateRSSSettingsRequestObject) (generated.UpdateRSSSettingsResponseObject, error) {
	if request.Body == nil {
		problem := rssInputProblem(ctx)
		return generated.UpdateRSSSettings400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	session, authenticationProblem, err := handler.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.UpdateRSSSettings401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.UpdateRSSSettings403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := handler.rss.UpdateSettings(ctx, staffActor(session), rss.UpdateSettingsInput{Enabled: request.Body.Enabled, CacheTTLSeconds: request.Body.CacheTtlSeconds, MaxItemsPerFeed: request.Body.MaxItemsPerFeed, MaxSubscriptionsPerUser: request.Body.MaxSubscriptionsPerUser, RequestsPerMinute: request.Body.RequestsPerMinute, ExpectedVersion: request.Body.ExpectedVersion, Reason: request.Body.Reason})
	if errors.Is(err, rss.ErrInvalidInput) {
		problem := rssInputProblem(ctx)
		return generated.UpdateRSSSettings400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "rss_settings_update_denied", "无法修改 RSS 设置", "当前后台身份没有 rss.settings.update。")
		return generated.UpdateRSSSettings403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if errors.Is(err, rss.ErrSettingsConflict) {
		problem := newProblemFromContext(ctx, http.StatusConflict, "rss_settings_version_conflict", "RSS 设置版本已经变化", "请重新载入后再提交。")
		return generated.UpdateRSSSettings409ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.UpdateRSSSettings200JSONResponse(rssSettingsDTO(result)), nil
}

func rssInputFromDTO(input generated.RSSSubscriptionInput) rss.SubscriptionInput {
	promotions := make([]string, 0, len(input.PromotionFilters))
	for _, value := range input.PromotionFilters {
		promotions = append(promotions, string(value))
	}
	return rss.SubscriptionInput{Name: input.Name, Enabled: input.Enabled, CategoryIDs: input.CategoryIds, PromotionFilters: promotions, PriceFilter: rss.PriceFilter(input.PriceFilter), BookmarkedOnly: input.BookmarkedOnly, ItemLimit: input.ItemLimit, IncludeCategory: input.IncludeCategory, IncludeSubtitle: input.IncludeSubtitle, IncludeSize: input.IncludeSize, IncludePromotion: input.IncludePromotion}
}

func rssInputFromUpdateDTO(input generated.UpdateRSSSubscriptionRequest) rss.UpdateSubscriptionInput {
	promotions := make([]string, 0, len(input.PromotionFilters))
	for _, value := range input.PromotionFilters {
		promotions = append(promotions, string(value))
	}
	return rss.UpdateSubscriptionInput{SubscriptionInput: rss.SubscriptionInput{Name: input.Name, Enabled: input.Enabled, CategoryIDs: input.CategoryIds, PromotionFilters: promotions, PriceFilter: rss.PriceFilter(input.PriceFilter), BookmarkedOnly: input.BookmarkedOnly, ItemLimit: input.ItemLimit, IncludeCategory: input.IncludeCategory, IncludeSubtitle: input.IncludeSubtitle, IncludeSize: input.IncludeSize, IncludePromotion: input.IncludePromotion}}
}

func rssSubscriptionDTO(subscription rss.Subscription) generated.RSSSubscription {
	promotions := make([]generated.RSSPromotionFilter, 0, len(subscription.PromotionFilters))
	for _, value := range subscription.PromotionFilters {
		promotions = append(promotions, generated.RSSPromotionFilter(value))
	}
	return generated.RSSSubscription{Id: subscription.ID, Name: subscription.Name, Enabled: subscription.Enabled, TokenVersion: subscription.TokenVersion, CategoryIds: subscription.CategoryIDs, PromotionFilters: promotions, PriceFilter: generated.RSSPriceFilter(subscription.PriceFilter), BookmarkedOnly: subscription.BookmarkedOnly, ItemLimit: subscription.ItemLimit, IncludeCategory: subscription.IncludeCategory, IncludeSubtitle: subscription.IncludeSubtitle, IncludeSize: subscription.IncludeSize, IncludePromotion: subscription.IncludePromotion, Version: subscription.Version, CreatedAt: subscription.CreatedAt, UpdatedAt: subscription.UpdatedAt}
}

func issuedRSSSubscriptionDTO(result rss.IssuedSubscription) generated.IssuedRSSSubscription {
	return generated.IssuedRSSSubscription{Subscription: rssSubscriptionDTO(result.Subscription), Token: result.Token, FeedUrl: result.FeedURL}
}

func rssSettingsDTO(settings rss.Settings) generated.RSSSettings {
	return generated.RSSSettings{Enabled: settings.Enabled, CacheTtlSeconds: settings.CacheTTLSeconds, MaxItemsPerFeed: settings.MaxItemsPerFeed, MaxSubscriptionsPerUser: settings.MaxSubscriptionsPerUser, RequestsPerMinute: settings.RequestsPerMinute, Version: settings.Version, EffectiveAt: settings.EffectiveAt, UpdatedAt: settings.UpdatedAt}
}

func rssInputProblem(ctx context.Context) generated.Problem {
	return newProblemFromContext(ctx, http.StatusBadRequest, "invalid_rss_input", "RSS 设置无效", "请检查名称、筛选条件、条目数量、版本和变更理由。")
}

func rssConflictProblem(ctx context.Context) generated.Problem {
	return newProblemFromContext(ctx, http.StatusConflict, "rss_subscription_version_conflict", "RSS 订阅版本已经变化", "请重新载入后再提交。")
}
