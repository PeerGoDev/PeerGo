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
)

func (h *Handler) ListManagedTorrentPurchases(ctx context.Context, request generated.ListManagedTorrentPurchasesRequestObject) (generated.ListManagedTorrentPurchasesResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ListManagedTorrentPurchases401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.ListManagedTorrentPurchases403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	query := torrentpurchase.AdminPurchaseQuery{
		Status: torrentpurchase.AdminPurchaseStatusAll,
		Source: torrentpurchase.AdminPurchaseSourceAll,
		Limit:  torrentpurchase.DefaultAdminLimit,
	}
	if request.Params.Query != nil {
		query.Query = *request.Params.Query
	}
	if request.Params.Status != nil {
		query.Status = torrentpurchase.AdminPurchaseStatus(*request.Params.Status)
	}
	if request.Params.Source != nil {
		query.Source = torrentpurchase.AdminPurchaseSource(*request.Params.Source)
	}
	if request.Params.Limit != nil {
		query.Limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		query.Offset = *request.Params.Offset
	}
	page, err := h.torrentDownload.AdminPurchaseHistory(ctx, staffActor(session), query)
	switch {
	case errors.Is(err, torrentpurchase.ErrInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_managed_torrent_purchase_query", "购买记录查询无效", "请检查关键词、状态、来源和分页范围。")
		return generated.ListManagedTorrentPurchases400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_purchase_manage_read_denied", "无法查看购买记录", "当前后台身份没有 torrent.purchase.manage.read 权限。")
		return generated.ListManagedTorrentPurchases403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.ManagedTorrentPurchase, 0, len(page.Items))
	for _, item := range page.Items {
		dto := generated.ManagedTorrentPurchase{
			BuyerNumericId: item.BuyerNumericID, BuyerUsername: item.BuyerUsername,
			BuyerDisplayName: item.BuyerDisplayName, SellerNumericId: item.SellerNumericID,
			SellerUsername: item.SellerUsername, TorrentId: item.TorrentID,
			TorrentTitle: item.TorrentTitle, CategoryName: item.CategoryName,
			Source: generated.ManagedTorrentPurchaseSource(item.Source),
			Status: generated.ManagedTorrentPurchaseStatus(item.Status),
			Price:  strconv.FormatInt(item.Price, 10), Tax: strconv.FormatInt(item.Tax, 10),
			SellerIncome: strconv.FormatInt(item.SellerIncome, 10), PurchasedAt: item.PurchasedAt,
		}
		if item.RefundedAt != nil {
			dto.RefundedAt = item.RefundedAt
			dto.RefundReason = &item.RefundReason
			if item.RefundedByNumericID != nil {
				dto.RefundedByNumericId = item.RefundedByNumericID
				dto.RefundedByUsername = &item.RefundedByUsername
			}
			if item.RefundedBalanceAfter != nil {
				value := strconv.FormatInt(*item.RefundedBalanceAfter, 10)
				dto.RefundedBalanceAfter = &value
			}
		}
		items = append(items, dto)
	}
	return generated.ListManagedTorrentPurchases200JSONResponse{
		Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset,
	}, nil
}

func (h *Handler) RefundManagedTorrentPurchase(ctx context.Context, request generated.RefundManagedTorrentPurchaseRequestObject) (generated.RefundManagedTorrentPurchaseResponseObject, error) {
	if request.Body == nil {
		return refundManagedTorrentPurchaseBadRequest(ctx), nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.RefundManagedTorrentPurchase401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.RefundManagedTorrentPurchase403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.torrentDownload.RefundPurchase(ctx, staffActor(session), torrentpurchase.RefundCommand{
		RequestID: request.Params.IdempotencyKey, BuyerNumericID: request.UserNumericId,
		TorrentID: request.TorrentId, Reason: request.Body.Reason,
	})
	switch {
	case errors.Is(err, torrentpurchase.ErrInput):
		return refundManagedTorrentPurchaseBadRequest(ctx), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_purchase_refund_denied", "没有权限退款", "当前后台身份没有 torrent.purchase.manage.refund 权限。")
		return generated.RefundManagedTorrentPurchase403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrentpurchase.ErrNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_purchase_not_found", "购买权限不存在", "没有找到该用户对该种子的有效购买权限。")
		return generated.RefundManagedTorrentPurchase404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrentpurchase.ErrAlreadyRefunded):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_purchase_already_refunded", "购买已经退款", "该购买权限已经撤销，请刷新购买记录。")
		return generated.RefundManagedTorrentPurchase409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrentpurchase.ErrIdempotencyConflict), errors.Is(err, economy.ErrIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_purchase_refund_idempotency_conflict", "请求标识已经使用", "请刷新购买记录后重新提交退款。")
		return generated.RefundManagedTorrentPurchase409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.RefundManagedTorrentPurchase201JSONResponse{
		RequestId: result.RequestID, BuyerNumericId: result.BuyerNumericID,
		TorrentId: result.TorrentID, TorrentTitle: result.TorrentTitle,
		RefundAmount: strconv.FormatInt(result.RefundAmount, 10),
		BalanceAfter: strconv.FormatInt(result.BalanceAfter, 10),
		RefundedAt:   result.RefundedAt, Replayed: result.Replayed,
	}, nil
}

func refundManagedTorrentPurchaseBadRequest(ctx context.Context) generated.RefundManagedTorrentPurchase400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_purchase_refund", "退款请求无效", "用户和种子 ID 必须有效，退款说明至少 10 个字符。")
	return generated.RefundManagedTorrentPurchase400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}
}
