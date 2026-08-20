package httpapi

import (
	"context"
	"errors"
	"net/http"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

func (h *Handler) ListMyTorrentBookmarks(ctx context.Context, request generated.ListMyTorrentBookmarksRequestObject) (generated.ListMyTorrentBookmarksResponseObject, error) {
	limit := catalog.DefaultTorrentBookmarkLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	offset := 0
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.torrentBookmarks.List(ctx, sessionTokenFromContext(ctx), limit, offset)
	switch {
	case errors.Is(err, catalog.ErrTorrentBookmarkInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_bookmark_query", "收藏查询无效", "每页数量必须在 1 到 50 之间，偏移量必须在 0 到 99999 之间。")
		return generated.ListMyTorrentBookmarks400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看自己的收藏。")
		return generated.ListMyTorrentBookmarks401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_bookmark_read_denied", "无法查看收藏", "当前账号暂时不能使用收藏功能。")
		return generated.ListMyTorrentBookmarks403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListMyTorrentBookmarks200JSONResponse{
		Body:    torrentBookmarkPageDTO(page),
		Headers: generated.ListMyTorrentBookmarks200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (h *Handler) ListMyTorrentBookmarkStatuses(ctx context.Context, request generated.ListMyTorrentBookmarkStatusesRequestObject) (generated.ListMyTorrentBookmarkStatusesResponseObject, error) {
	ids, err := h.torrentBookmarks.Statuses(ctx, sessionTokenFromContext(ctx), request.Params.TorrentId)
	switch {
	case errors.Is(err, catalog.ErrTorrentBookmarkInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_bookmark_status_query", "收藏状态查询无效", "请提供 1 到 100 个不重复的有效种子标识。")
		return generated.ListMyTorrentBookmarkStatuses400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看收藏状态。")
		return generated.ListMyTorrentBookmarkStatuses401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_bookmark_read_denied", "无法查看收藏状态", "当前账号暂时不能使用收藏功能。")
		return generated.ListMyTorrentBookmarkStatuses403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListMyTorrentBookmarkStatuses200JSONResponse{
		Body:    generated.TorrentBookmarkStatusList{BookmarkedIds: ids},
		Headers: generated.ListMyTorrentBookmarkStatuses200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (h *Handler) PutMyTorrentBookmark(ctx context.Context, request generated.PutMyTorrentBookmarkRequestObject) (generated.PutMyTorrentBookmarkResponseObject, error) {
	state, err := h.torrentBookmarks.Put(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), request.TorrentId)
	switch {
	case errors.Is(err, catalog.ErrTorrentBookmarkInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_bookmark", "无法收藏种子", "种子标识无效，请刷新页面后重试。")
		return generated.PutMyTorrentBookmark400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后收藏种子。")
		return generated.PutMyTorrentBookmark401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.PutMyTorrentBookmark403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_bookmark_write_denied", "无法收藏种子", "当前账号暂时不能修改收藏。")
		return generated.PutMyTorrentBookmark403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, catalog.ErrTorrentBookmarkNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不可用", "该种子不存在或已经停止公开访问。")
		return generated.PutMyTorrentBookmark404ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.PutMyTorrentBookmark200JSONResponse{
		Body: generated.TorrentBookmarkState{
			TorrentId: state.TorrentID, Bookmarked: true, BookmarkedAt: state.BookmarkedAt,
		},
		Headers: generated.PutMyTorrentBookmark200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func (h *Handler) DeleteMyTorrentBookmark(ctx context.Context, request generated.DeleteMyTorrentBookmarkRequestObject) (generated.DeleteMyTorrentBookmarkResponseObject, error) {
	err := h.torrentBookmarks.Delete(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), request.TorrentId)
	switch {
	case errors.Is(err, catalog.ErrTorrentBookmarkInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_bookmark", "无法取消收藏", "种子标识无效，请刷新页面后重试。")
		return generated.DeleteMyTorrentBookmark400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后修改收藏。")
		return generated.DeleteMyTorrentBookmark401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.DeleteMyTorrentBookmark403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_bookmark_write_denied", "无法取消收藏", "当前账号暂时不能修改收藏。")
		return generated.DeleteMyTorrentBookmark403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.DeleteMyTorrentBookmark204Response{}, nil
}

func torrentBookmarkPageDTO(page catalog.TorrentBookmarkPage) generated.TorrentBookmarkPage {
	items := make([]generated.TorrentBookmark, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, generated.TorrentBookmark{
			Torrent: torrentSummaryDTO(item.Torrent), BookmarkedAt: item.BookmarkedAt,
		})
	}
	return generated.TorrentBookmarkPage{
		Items: items, Total: int64(page.Total), Limit: page.Limit, Offset: page.Offset,
	}
}
