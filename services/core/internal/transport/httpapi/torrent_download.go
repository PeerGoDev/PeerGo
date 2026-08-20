package httpapi

import (
	"bytes"
	"context"
	"errors"
	"mime"
	"net/http"

	"github.com/google/uuid"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/economy/torrentpurchase"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

type TorrentDownloadService interface {
	Download(context.Context, string, torrents.TorrentID) (torrents.TorrentDownloadResult, error)
	MyPurchaseStatus(context.Context, string, torrents.TorrentID) (torrentpurchase.Status, error)
	MyPurchaseHistory(context.Context, string, int, int) (torrentpurchase.HistoryPage, error)
	Purchase(context.Context, string, string, uuid.UUID, torrents.TorrentID) (torrentpurchase.Receipt, error)
	PurchasePolicy(context.Context, authz.StaffActor) (torrentpurchase.PolicySettings, error)
	UpdatePurchasePolicy(context.Context, authz.StaffActor, torrentpurchase.UpdatePolicyCommand) (torrentpurchase.PolicySettings, error)
	UpdateTorrentPrice(context.Context, authz.StaffActor, torrentpurchase.UpdatePriceCommand) (torrentpurchase.PriceChange, error)
	AdminPurchaseHistory(context.Context, authz.StaffActor, torrentpurchase.AdminPurchaseQuery) (torrentpurchase.AdminPurchasePage, error)
	RefundPurchase(context.Context, authz.StaffActor, torrentpurchase.RefundCommand) (torrentpurchase.RefundReceipt, error)
}

// DownloadTorrent returns binary metainfo directly. The browser never receives
// the passkey as JSON or as part of this request URL; it exists only inside the
// server-generated announce field in the attachment body.
func (handler *Handler) DownloadTorrent(ctx context.Context, request generated.DownloadTorrentRequestObject) (generated.DownloadTorrentResponseObject, error) {
	result, err := handler.torrentDownload.Download(ctx, sessionTokenFromContext(ctx), torrents.TorrentID(request.TorrentId))
	if response, handled := torrentDownloadErrorResponse(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	contentDisposition := mime.FormatMediaType("attachment", map[string]string{"filename": result.Filename})
	if contentDisposition == "" {
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "torrent_download_unavailable", "种子暂时无法下载", "请稍后重试，并在反馈时附上 request_id。")
		return generated.DownloadTorrentdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	}
	return generated.DownloadTorrent200ApplicationxBittorrentResponse{
		Body: bytes.NewReader(result.Data),
		Headers: generated.DownloadTorrent200ResponseHeaders{
			CacheControl:       "private, no-store",
			ContentDisposition: contentDisposition,
		},
		ContentLength: int64(len(result.Data)),
	}, nil
}

func torrentDownloadErrorResponse(ctx context.Context, err error) (generated.DownloadTorrentResponseObject, bool) {
	if err == nil {
		return nil, false
	}
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "web_session_required", "需要登录", "请登录后下载种子。")
		return generated.DownloadTorrent401ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, true
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_download_forbidden", "无法下载种子", "当前账户没有下载种子的权限。")
		return generated.DownloadTorrent403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentDownloadEmailUnverified):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "verified_email_required", "需要验证邮箱", "请先完成邮箱验证，再下载种子。")
		return generated.DownloadTorrent403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentDownloadRestricted):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_download_restricted", "当前账户下载受限", "请查看分享率考核与 H&R 待补做记录，或联系管理员核对限制状态。Tracker 做种上传不受影响。")
		return generated.DownloadTorrent403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrents.ErrTorrentDownloadNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不存在", "该种子不存在或当前未发布。")
		return generated.DownloadTorrent404ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrentpurchase.ErrPurchaseRequired):
		problem := newProblemFromContext(ctx, http.StatusPaymentRequired, "torrent_purchase_required", "需要购买种子", "请先使用魔力值购买该种子的永久下载权限。")
		return generated.DownloadTorrent402ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, torrentpurchase.ErrPurchaseDisabled):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_purchase_disabled", "暂时无法购买", "站点已暂停新的种子购买；已有购买权限不受影响。")
		return generated.DownloadTorrent403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, identity.ErrTrackerCredentialUnavailable),
		errors.Is(err, identity.ErrTrackerCredentialStateConflict),
		errors.Is(err, torrents.ErrTorrentDownloadStorageUnavailable),
		errors.Is(err, torrents.ErrTorrentDownloadObjectConflict):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "torrent_download_unavailable", "种子暂时无法下载", "服务端无法安全生成下载副本，请稍后重试。")
		return generated.DownloadTorrentdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, true
	default:
		return nil, false
	}
}
