package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/torrentpurchase"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func (h *Handler) ListManagedTorrents(ctx context.Context, request generated.ListManagedTorrentsRequestObject) (generated.ListManagedTorrentsResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ListManagedTorrents401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.ListManagedTorrents403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	query := torrents.ManagedTorrentQuery{Limit: torrents.DefaultManagedTorrentLimit}
	if request.Params.Query != nil {
		query.Query = *request.Params.Query
	}
	if request.Params.State != nil {
		query.State = torrents.State(*request.Params.State)
	}
	if request.Params.CategoryId != nil {
		query.CategoryID = *request.Params.CategoryId
	}
	if request.Params.Limit != nil {
		query.Limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		query.Offset = *request.Params.Offset
	}
	page, err := h.torrentRead.ListManaged(ctx, staffActor(session), query)
	if errors.Is(err, torrents.ErrTorrentAdministrationInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_management_query", "种子管理查询无效", "请检查关键词、状态、分类和分页范围。")
		return generated.ListManagedTorrents400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_management_read_denied", "无法查看种子管理", "当前后台身份没有 torrent.manage.read 权限。")
		return generated.ListManagedTorrents403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	return generated.ListManagedTorrents200JSONResponse(managedTorrentPageDTO(page)), nil
}

func (h *Handler) ListManagedTorrentPeers(ctx context.Context, request generated.ListManagedTorrentPeersRequestObject) (generated.ListManagedTorrentPeersResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ListManagedTorrentPeers401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.ListManagedTorrentPeers403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	page, err := h.torrentRead.ManagedActivePeers(ctx, staffActor(session), torrents.TorrentID(request.TorrentId))
	switch {
	case errors.Is(err, torrents.ErrTorrentAdministrationInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_peer_query", "用户列表查询无效", "种子编号无效。")
		return generated.ListManagedTorrentPeers400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_management_read_denied", "无法查看实时用户", "当前后台身份没有 torrent.manage.read 权限。")
		return generated.ListManagedTorrentPeers403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrents.ErrManagedTorrentNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不可用", "该种子不存在或当前未发布。")
		return generated.ListManagedTorrentPeers404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrents.ErrManagedTorrentPeersUnavailable):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "torrent_peers_unavailable", "实时用户暂时不可用", "Tracker 当前无法提供实时用户，请稍后重试。")
		return generated.ListManagedTorrentPeersdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	return generated.ListManagedTorrentPeers200JSONResponse{
		Body:    managedTorrentPeerListDTO(page),
		Headers: generated.ListManagedTorrentPeers200ResponseHeaders{CacheControl: "private, no-store"},
	}, nil
}

func managedTorrentPeerListDTO(page torrents.ManagedTorrentPeerList) generated.ManagedTorrentPeerList {
	items := make([]generated.ManagedTorrentPeer, 0, len(page.Items))
	for _, peer := range page.Items {
		items = append(items, generated.ManagedTorrentPeer{
			UserId: peer.UserID, UserNumericId: peer.NumericID, Username: peer.Username, DisplayName: peer.DisplayName,
			ClientFamilies: peer.ClientFamilies, ActiveConnections: peer.ActiveConnections,
			SeedingConnections: peer.SeedingConnections, LeechingConnections: peer.LeechingConnections,
			ProgressBasisPoints: peer.ProgressBasisPoints, Uploaded: strconv.FormatInt(peer.Uploaded, 10),
			Downloaded: strconv.FormatInt(peer.Downloaded, 10), LastAnnounce: peer.LastAnnounce, Uploader: peer.Uploader,
		})
	}
	return generated.ManagedTorrentPeerList{
		TorrentId: int64(page.TorrentID), Items: items, TotalConnections: page.TotalConnections,
		Truncated: page.Truncated, GeneratedAt: page.GeneratedAt,
	}
}

func (h *Handler) ChangeManagedTorrentAvailability(ctx context.Context, request generated.ChangeManagedTorrentAvailabilityRequestObject) (generated.ChangeManagedTorrentAvailabilityResponseObject, error) {
	if request.Body == nil {
		return changeManagedTorrentAvailabilityBadRequest(ctx), nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ChangeManagedTorrentAvailability401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.ChangeManagedTorrentAvailability403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.torrentRead.ChangeAvailability(ctx, staffActor(session), torrents.ChangeTorrentAvailabilityInput{
		ChangeID: request.Params.IdempotencyKey, TorrentID: torrents.TorrentID(request.TorrentId),
		ExpectedVersion: request.Body.ExpectedVersion, Action: torrents.TorrentAvailabilityAction(request.Body.Action),
		Reason: request.Body.Reason,
	})
	if response, handled := managedTorrentAvailabilityErrorResponse(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.ChangeManagedTorrentAvailability201JSONResponse(torrentAvailabilityResultDTO(result)), nil
}

func (h *Handler) UpdateManagedTorrentPurchasePrice(ctx context.Context, request generated.UpdateManagedTorrentPurchasePriceRequestObject) (generated.UpdateManagedTorrentPurchasePriceResponseObject, error) {
	if request.Body == nil {
		return updateManagedTorrentPurchasePriceBadRequest(ctx), nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.UpdateManagedTorrentPurchasePrice401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.UpdateManagedTorrentPurchasePrice403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.torrentDownload.UpdateTorrentPrice(ctx, staffActor(session), torrentpurchase.UpdatePriceCommand{
		RequestID: request.Params.IdempotencyKey, TorrentID: request.TorrentId,
		Price: request.Body.Price, ExpectedVersion: request.Body.ExpectedVersion, Reason: request.Body.Reason,
	})
	switch {
	case errors.Is(err, torrentpurchase.ErrInput), errors.Is(err, torrentpurchase.ErrNoChange):
		return updateManagedTorrentPurchasePriceBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_purchase_manage_denied", "没有权限修改种子价格", "当前后台身份没有 torrent.purchase.manage.update 权限。")
		return generated.UpdateManagedTorrentPurchasePrice403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrentpurchase.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "managed_torrent_not_found", "种子不存在", "目标种子不存在或已经删除。")
		return generated.UpdateManagedTorrentPurchasePrice404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrentpurchase.ErrVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "managed_torrent_version_conflict", "种子版本已经变化", "请重新载入种子管理并核对最新价格。")
		return generated.UpdateManagedTorrentPurchasePrice409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrentpurchase.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_price_idempotency_conflict", "请求标识已经使用", "请刷新种子管理后重新提交。")
		return generated.UpdateManagedTorrentPurchasePrice409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.UpdateManagedTorrentPurchasePrice200JSONResponse{
		RequestId: result.RequestID, TorrentId: result.TorrentID, Title: result.Title,
		Price: strconv.FormatInt(result.Price, 10), Version: result.Version,
		ChangedAt: result.ChangedAt, Replayed: result.Replayed,
	}, nil
}

func updateManagedTorrentPurchasePriceBadRequest(ctx context.Context) generated.UpdateManagedTorrentPurchasePrice400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_purchase_price", "种子价格无效", "价格必须为 0 到 1000000 的整数，且修改说明至少 10 个字符。")
	return generated.UpdateManagedTorrentPurchasePrice400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}

func managedTorrentAvailabilityErrorResponse(ctx context.Context, err error) (generated.ChangeManagedTorrentAvailabilityResponseObject, bool) {
	switch {
	case err == nil:
		return nil, false
	case errors.Is(err, torrents.ErrTorrentAdministrationInput):
		return changeManagedTorrentAvailabilityBadRequest(ctx), true
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_lifecycle_update_denied", "无法更改种子可用性", "当前后台身份没有 torrent.lifecycle.update 权限。")
		return generated.ChangeManagedTorrentAvailability403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrManagedTorrentNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "managed_torrent_not_found", "种子不存在", "目标种子不存在或已不在当前数据集中。")
		return generated.ChangeManagedTorrentAvailability404ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrManagedTorrentIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_lifecycle_idempotency_conflict", "操作请求内容已经变化", "请勿复用其他种子操作的 Idempotency-Key。")
		return generated.ChangeManagedTorrentAvailability409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrManagedTorrentVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "managed_torrent_version_conflict", "种子版本已经变化", "请重新载入工作台并核对最新状态。")
		return generated.ChangeManagedTorrentAvailability409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrManagedTorrentStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "managed_torrent_state_conflict", "当前状态不能执行该操作", "只有已发布种子可以下架，只有已下架种子可以恢复。")
		return generated.ChangeManagedTorrentAvailability409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrManagedTorrentCategoryUnavailable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "managed_torrent_category_unavailable", "分类当前不可发布", "请先启用该分类，再恢复种子。")
		return generated.ChangeManagedTorrentAvailability409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrManagedTorrentObjectUnavailable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "managed_torrent_object_unavailable", "种子文件当前不可恢复", "没有可验证的种子文件存储位置，请先完成存储恢复。")
		return generated.ChangeManagedTorrentAvailability409ApplicationProblemPlusJSONResponse(problem), true
	default:
		return nil, false
	}
}

func changeManagedTorrentAvailabilityBadRequest(ctx context.Context) generated.ChangeManagedTorrentAvailability400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_availability_change", "种子可用性操作无效", "请检查版本、操作类型和至少 10 个字符的操作原因。")
	return generated.ChangeManagedTorrentAvailability400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func managedTorrentPageDTO(page torrents.ManagedTorrentPage) generated.ManagedTorrentPage {
	items := make([]generated.ManagedTorrent, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, generated.ManagedTorrent{
			Id: int64(item.ID), UploaderNumericId: item.UploaderNumericID, UploaderUsername: item.UploaderUsername,
			UploaderDisplayName: item.UploaderDisplayName, CategoryId: item.CategoryID, CategoryName: item.CategoryName,
			Title: item.Title, Subtitle: item.Subtitle, TotalSizeBytes: item.TotalSizeBytes, PurchasePrice: strconv.FormatInt(item.PurchasePrice, 10),
			State: generated.TorrentLifecycleState(item.State), Version: item.Version,
			Promotion: generated.ManagedTorrentPromotion(item.Promotion), PromotionEndsAt: item.PromotionEndsAt,
			Seeders: item.Seeders, Leechers: item.Leechers, Completed: item.Completed,
			SubmittedAt: item.SubmittedAt, PublishedAt: item.PublishedAt, StateChangedAt: item.StateChangedAt, UpdatedAt: item.UpdatedAt,
		})
	}
	categories := make([]generated.ManagedTorrentCategory, 0, len(page.Categories))
	for _, category := range page.Categories {
		categories = append(categories, generated.ManagedTorrentCategory{Id: category.ID, Name: category.Name, Enabled: category.Enabled})
	}
	return generated.ManagedTorrentPage{
		Items: items, Categories: categories, Total: page.Total, Limit: page.Limit, Offset: page.Offset,
		StateCounts: generated.ManagedTorrentStateCounts{
			PendingReview: page.StateCounts.PendingReview, Published: page.StateCounts.Published,
			Rejected: page.StateCounts.Rejected, Disabled: page.StateCounts.Disabled, Deleted: page.StateCounts.Deleted,
		},
	}
}

func torrentAvailabilityResultDTO(result torrents.TorrentAvailabilityResult) generated.TorrentAvailabilityResult {
	return generated.TorrentAvailabilityResult{
		ChangeId: result.ChangeID, TorrentId: int64(result.TorrentID),
		Action: generated.TorrentAvailabilityResultAction(result.Action), State: generated.TorrentAvailabilityResultState(result.State),
		Version: result.Version, ChangedAt: result.ChangedAt,
	}
}
