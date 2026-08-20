package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
	"github.com/peergo/peergo/services/core/internal/modules/economy/torrentpurchase"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

func (handler *Handler) GetMyTorrentPurchase(ctx context.Context, request generated.GetMyTorrentPurchaseRequestObject) (generated.GetMyTorrentPurchaseResponseObject, error) {
	status, err := handler.torrentDownload.MyPurchaseStatus(ctx, sessionTokenFromContext(ctx), torrents.TorrentID(request.TorrentId))
	switch {
	case errors.Is(err, torrentpurchase.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_purchase", "购买状态查询无效", "请检查种子编号。")
		return generated.GetMyTorrentPurchase400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看购买状态。")
		return generated.GetMyTorrentPurchase401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_purchase_read_denied", "无法查看购买状态", "当前账号没有购买状态查看权限。")
		return generated.GetMyTorrentPurchase403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrentpurchase.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不存在", "该种子不存在或当前未发布。")
		return generated.GetMyTorrentPurchase404ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetMyTorrentPurchase200JSONResponse(torrentPurchaseStatusDTO(status)), nil
}

func (handler *Handler) PurchaseTorrent(ctx context.Context, request generated.PurchaseTorrentRequestObject) (generated.PurchaseTorrentResponseObject, error) {
	receipt, err := handler.torrentDownload.Purchase(
		ctx,
		sessionTokenFromContext(ctx),
		string(request.Params.XCSRFToken),
		request.Params.IdempotencyKey,
		torrents.TorrentID(request.TorrentId),
	)
	switch {
	case errors.Is(err, torrentpurchase.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_purchase", "购买请求无效", "请刷新页面后重新操作。")
		return generated.PurchaseTorrent400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后购买种子。")
		return generated.PurchaseTorrent401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.PurchaseTorrent403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_purchase_denied", "无法购买种子", "当前账号没有种子购买权限。")
		return generated.PurchaseTorrent403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrentpurchase.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不存在", "该种子不存在或当前未发布。")
		return generated.PurchaseTorrent404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, economy.ErrInsufficientBalance):
		problem := newProblemFromContext(ctx, http.StatusConflict, "magic_balance_insufficient", "魔力值不足", "当前魔力值余额不足以购买该种子。")
		return generated.PurchaseTorrent409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrentpurchase.ErrPurchaseDisabled):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_purchase_disabled", "暂时无法购买", "站点已暂停新的种子购买。")
		return generated.PurchaseTorrent409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrentpurchase.ErrPurchaseNotRequired):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_purchase_not_required", "无需购买", "该种子免费，或你是发布者。")
		return generated.PurchaseTorrent409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrentpurchase.ErrIdempotencyConflict), errors.Is(err, economy.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_purchase_idempotency_conflict", "请求标识已被使用", "请刷新购买状态后重新操作。")
		return generated.PurchaseTorrent409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.PurchaseTorrent201JSONResponse(torrentPurchaseReceiptDTO(receipt)), nil
}

func (handler *Handler) ListMyTorrentPurchases(ctx context.Context, request generated.ListMyTorrentPurchasesRequestObject) (generated.ListMyTorrentPurchasesResponseObject, error) {
	limit, offset := torrentpurchase.DefaultHistoryLimit, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := handler.torrentDownload.MyPurchaseHistory(ctx, sessionTokenFromContext(ctx), limit, offset)
	switch {
	case errors.Is(err, torrentpurchase.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_purchase_history", "购买记录查询无效", "请检查分页范围。")
		return generated.ListMyTorrentPurchases400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看购买记录。")
		return generated.ListMyTorrentPurchases401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_purchase_read_denied", "无法查看购买记录", "当前账号没有购买记录查看权限。")
		return generated.ListMyTorrentPurchases403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.TorrentPurchaseHistoryItem, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, generated.TorrentPurchaseHistoryItem{
			TorrentId: item.TorrentID, Title: item.Title, CategoryName: item.CategoryName,
			TorrentState: generated.TorrentLifecycleState(item.TorrentState), Price: strconv.FormatInt(item.Price, 10),
			PurchasedAt: item.PurchasedAt, LegacyImport: item.LegacyImport,
		})
	}
	return generated.ListMyTorrentPurchases200JSONResponse{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}, nil
}

func torrentPurchaseStatusDTO(status torrentpurchase.Status) generated.TorrentPurchaseStatus {
	return generated.TorrentPurchaseStatus{
		TorrentId:      status.TorrentID,
		Title:          status.Title,
		Price:          strconv.FormatInt(status.Price, 10),
		Tax:            strconv.FormatInt(status.Tax, 10),
		SellerIncome:   strconv.FormatInt(status.SellerIncome, 10),
		MagicBalance:   strconv.FormatInt(status.MagicBalance, 10),
		State:          generated.TorrentPurchaseAccessState(status.State),
		PolicyRevision: status.PolicyRevision,
		PurchasedAt:    status.PurchasedAt,
		LegacyImport:   status.LegacyImport,
	}
}

func torrentPurchaseReceiptDTO(receipt torrentpurchase.Receipt) generated.TorrentPurchaseReceipt {
	return generated.TorrentPurchaseReceipt{
		EntitlementId:  receipt.EntitlementID,
		RequestId:      receipt.RequestID,
		TorrentId:      receipt.TorrentID,
		Price:          strconv.FormatInt(receipt.Price, 10),
		Tax:            strconv.FormatInt(receipt.Tax, 10),
		SellerIncome:   strconv.FormatInt(receipt.SellerIncome, 10),
		BalanceAfter:   strconv.FormatInt(receipt.BalanceAfter, 10),
		PolicyRevision: receipt.PolicyRevision,
		PurchasedAt:    receipt.PurchasedAt,
		Replayed:       receipt.Replayed,
	}
}
