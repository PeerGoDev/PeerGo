package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strconv"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

// TorrentReadService exposes separate public, self-service and staff
// administration use cases. The administration methods operate on the torrent
// aggregate and emit Tracker control events; they never expose Tracker peers,
// passkeys or storage coordinates through this HTTP boundary.
type TorrentReadService interface {
	Detail(context.Context, torrents.TorrentID) (torrents.PublicDetail, error)
	Cover(context.Context, torrents.TorrentID) (torrents.PublicCover, error)
	Screenshot(context.Context, torrents.TorrentID, int) (torrents.PublicScreenshot, error)
	Content(context.Context, torrents.TorrentID) (torrents.PublicContent, error)
	RelatedVersions(context.Context, torrents.TorrentID) ([]catalog.TorrentSummary, error)
	Files(context.Context, torrents.TorrentID, int, int) (torrents.PublicFilePage, error)
	ActivePeers(context.Context, string, torrents.TorrentID) (torrents.ManagedTorrentPeerList, error)
	MySubmissions(context.Context, string, int) (torrents.MySubmissionPage, error)
	ListManaged(context.Context, authz.StaffActor, torrents.ManagedTorrentQuery) (torrents.ManagedTorrentPage, error)
	ManagedActivePeers(context.Context, authz.StaffActor, torrents.TorrentID) (torrents.ManagedTorrentPeerList, error)
	ChangeAvailability(context.Context, authz.StaffActor, torrents.ChangeTorrentAvailabilityInput) (torrents.TorrentAvailabilityResult, error)
}

func (h *Handler) ListTorrentPeers(ctx context.Context, request generated.ListTorrentPeersRequestObject) (generated.ListTorrentPeersResponseObject, error) {
	page, err := h.torrentRead.ActivePeers(ctx, sessionTokenFromContext(ctx), torrents.TorrentID(request.TorrentId))
	switch {
	case errors.Is(err, torrents.ErrTorrentReadInput), errors.Is(err, torrents.ErrTorrentAdministrationInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_peer_query", "用户列表查询无效", "种子编号无效。")
		return generated.ListTorrentPeers400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请先登录站点账号后查看实时用户。")
		return generated.ListTorrentPeers401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_peers_denied", "无法查看实时用户", "当前账号状态不允许查看实时用户。")
		return generated.ListTorrentPeers403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrents.ErrManagedTorrentNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不可用", "该种子不存在或当前未发布。")
		return generated.ListTorrentPeers404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, torrents.ErrManagedTorrentPeersUnavailable):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "torrent_peers_unavailable", "实时用户暂时不可用", "Tracker 当前无法提供实时用户，请稍后重试。")
		return generated.ListTorrentPeersdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	return generated.ListTorrentPeers200JSONResponse{
		Body: torrentPeerListDTO(page),
		Headers: generated.ListTorrentPeers200ResponseHeaders{
			CacheControl: "private, no-store",
		},
	}, nil
}

func torrentPeerListDTO(page torrents.ManagedTorrentPeerList) generated.TorrentPeerList {
	items := make([]generated.TorrentPeer, 0, len(page.Items))
	for _, peer := range page.Items {
		userNumericID := peer.NumericID
		username := peer.Username
		displayName := peer.DisplayName
		if peer.AnonymousUploader {
			userNumericID = 0
			username = "anonymous"
			displayName = "匿名"
		}
		items = append(items, generated.TorrentPeer{
			UserNumericId: userNumericID, Username: username, DisplayName: displayName,
			Anonymous: peer.AnonymousUploader, ClientFamilies: peer.ClientFamilies,
			AddressFamilies: torrentPeerAddressFamiliesDTO(peer.AddressFamilies), ActiveConnections: peer.ActiveConnections,
			SeedingConnections: peer.SeedingConnections, LeechingConnections: peer.LeechingConnections,
			ProgressBasisPoints: peer.ProgressBasisPoints, Uploaded: strconv.FormatInt(peer.Uploaded, 10),
			Downloaded: strconv.FormatInt(peer.Downloaded, 10), UploadSpeed: strconv.FormatInt(peer.UploadSpeed, 10),
			DownloadSpeed: strconv.FormatInt(peer.DownloadSpeed, 10), LastAnnounce: peer.LastAnnounce,
			Uploader: peer.Uploader, Seedbox: peer.Seedbox,
		})
	}
	return generated.TorrentPeerList{
		TorrentId: int64(page.TorrentID), Items: items, TotalConnections: page.TotalConnections,
		Truncated: page.Truncated, GeneratedAt: page.GeneratedAt,
	}
}

func torrentPeerAddressFamiliesDTO(values []string) []generated.TorrentPeerAddressFamilies {
	result := make([]generated.TorrentPeerAddressFamilies, 0, len(values))
	for _, value := range values {
		result = append(result, generated.TorrentPeerAddressFamilies(value))
	}
	return result
}

func (h *Handler) GetTorrentScreenshot(ctx context.Context, request generated.GetTorrentScreenshotRequestObject) (generated.GetTorrentScreenshotResponseObject, error) {
	screenshot, err := h.torrentRead.Screenshot(ctx, torrents.TorrentID(request.TorrentId), request.Position)
	switch {
	case errors.Is(err, torrents.ErrTorrentScreenshotNotFound),
		errors.Is(err, torrents.ErrTorrentReadNotFound),
		errors.Is(err, torrents.ErrTorrentReadInput):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_screenshot_not_found", "暂无截图", "该位置没有公开截图，或种子已经停止公开访问。")
		return generated.GetTorrentScreenshot404ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, torrents.ErrTorrentScreenshotUnavailable),
		errors.Is(err, torrents.ErrTorrentScreenshotConflict):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "torrent_screenshot_unavailable", "截图暂时不可用", "服务端无法安全读取截图，请稍后重试。")
		return generated.GetTorrentScreenshotdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	headers := generated.GetTorrentScreenshot200ResponseHeaders{
		CacheControl: "public, max-age=300, stale-while-revalidate=86400",
		ETag:         screenshot.ETag,
	}
	reader := bytes.NewReader(screenshot.Data)
	switch screenshot.ContentType {
	case "image/gif":
		return generated.GetTorrentScreenshot200ImagegifResponse{Body: reader, Headers: headers, ContentLength: int64(len(screenshot.Data))}, nil
	case "image/jpeg":
		return generated.GetTorrentScreenshot200ImagejpegResponse{Body: reader, Headers: headers, ContentLength: int64(len(screenshot.Data))}, nil
	case "image/png":
		return generated.GetTorrentScreenshot200ImagepngResponse{Body: reader, Headers: headers, ContentLength: int64(len(screenshot.Data))}, nil
	case "image/webp":
		return generated.GetTorrentScreenshot200ImagewebpResponse{Body: reader, Headers: headers, ContentLength: int64(len(screenshot.Data))}, nil
	default:
		return nil, torrents.ErrTorrentScreenshotConflict
	}
}

func (h *Handler) GetTorrentCover(ctx context.Context, request generated.GetTorrentCoverRequestObject) (generated.GetTorrentCoverResponseObject, error) {
	cover, err := h.torrentRead.Cover(ctx, torrents.TorrentID(request.TorrentId))
	switch {
	case errors.Is(err, torrents.ErrTorrentCoverNotFound),
		errors.Is(err, torrents.ErrTorrentReadNotFound),
		errors.Is(err, torrents.ErrTorrentReadInput):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_cover_not_found", "暂无封面", "该种子尚未提供公开封面，或已经停止公开访问。")
		return generated.GetTorrentCover404ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, torrents.ErrTorrentCoverUnavailable),
		errors.Is(err, torrents.ErrTorrentCoverConflict):
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "torrent_cover_unavailable", "封面暂时不可用", "服务端无法安全读取封面，请稍后重试。")
		return generated.GetTorrentCoverdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, nil
	case err != nil:
		return nil, err
	}
	headers := generated.GetTorrentCover200ResponseHeaders{
		CacheControl: "public, max-age=300, stale-while-revalidate=86400",
		ETag:         cover.ETag,
	}
	reader := bytes.NewReader(cover.Data)
	switch cover.ContentType {
	case "image/gif":
		return generated.GetTorrentCover200ImagegifResponse{Body: reader, Headers: headers, ContentLength: int64(len(cover.Data))}, nil
	case "image/jpeg":
		return generated.GetTorrentCover200ImagejpegResponse{Body: reader, Headers: headers, ContentLength: int64(len(cover.Data))}, nil
	case "image/png":
		return generated.GetTorrentCover200ImagepngResponse{Body: reader, Headers: headers, ContentLength: int64(len(cover.Data))}, nil
	case "image/webp":
		return generated.GetTorrentCover200ImagewebpResponse{Body: reader, Headers: headers, ContentLength: int64(len(cover.Data))}, nil
	default:
		return nil, torrents.ErrTorrentCoverConflict
	}
}

func (h *Handler) GetTorrentRelatedVersions(ctx context.Context, request generated.GetTorrentRelatedVersionsRequestObject) (generated.GetTorrentRelatedVersionsResponseObject, error) {
	items, err := h.torrentRead.RelatedVersions(ctx, torrents.TorrentID(request.TorrentId))
	switch {
	case errors.Is(err, torrents.ErrTorrentReadNotFound), errors.Is(err, torrents.ErrTorrentReadInput):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不可用", "该种子不存在、尚未发布或已经停止公开访问。")
		return generated.GetTorrentRelatedVersions404ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case err != nil:
		return nil, err
	}
	result := make([]generated.TorrentSummary, 0, len(items))
	for _, item := range items {
		result = append(result, torrentSummaryDTO(item))
	}
	return generated.GetTorrentRelatedVersions200JSONResponse{Items: result}, nil
}

func (h *Handler) GetTorrentContent(ctx context.Context, request generated.GetTorrentContentRequestObject) (generated.GetTorrentContentResponseObject, error) {
	content, err := h.torrentRead.Content(ctx, torrents.TorrentID(request.TorrentId))
	switch {
	case errors.Is(err, torrents.ErrTorrentReadNotFound), errors.Is(err, torrents.ErrTorrentReadInput):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不可用", "该种子不存在、尚未发布或已经停止公开访问。")
		return generated.GetTorrentContent404ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case err != nil:
		return nil, err
	}
	return generated.GetTorrentContent200JSONResponse{
		TorrentId:         int64(content.TorrentID),
		Description:       content.Description,
		DescriptionFormat: generated.TorrentPublicContentDescriptionFormat(content.DescriptionFormat),
		MediaInfo:         content.MediaInfo,
	}, nil
}

func (h *Handler) GetTorrentDetail(ctx context.Context, request generated.GetTorrentDetailRequestObject) (generated.GetTorrentDetailResponseObject, error) {
	detail, err := h.torrentRead.Detail(ctx, torrents.TorrentID(request.TorrentId))
	switch {
	case errors.Is(err, torrents.ErrTorrentReadNotFound), errors.Is(err, torrents.ErrTorrentReadInput):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不可用", "该种子不存在、尚未发布或已经停止公开访问。")
		return generated.GetTorrentDetail404ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case err != nil:
		return nil, err
	}
	return generated.GetTorrentDetail200JSONResponse(torrentPublicDetailDTO(detail)), nil
}

func (h *Handler) ListTorrentFiles(ctx context.Context, request generated.ListTorrentFilesRequestObject) (generated.ListTorrentFilesResponseObject, error) {
	limit := torrents.DefaultTorrentFileLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	offset := 0
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.torrentRead.Files(ctx, torrents.TorrentID(request.TorrentId), limit, offset)
	switch {
	case errors.Is(err, torrents.ErrTorrentReadInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_file_query", "文件查询无效", "文件数量必须在 1 到 100 之间，偏移量必须在 0 到 99999 之间。")
		return generated.ListTorrentFiles400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, torrents.ErrTorrentReadNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不可用", "该种子不存在、尚未发布或已经停止公开访问。")
		return generated.ListTorrentFiles404ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListTorrentFiles200JSONResponse(torrentFilePageDTO(page)), nil
}

func (h *Handler) ListMyTorrentSubmissions(ctx context.Context, request generated.ListMyTorrentSubmissionsRequestObject) (generated.ListMyTorrentSubmissionsResponseObject, error) {
	limit := torrents.DefaultMyTorrentSubmissionLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	page, err := h.torrentRead.MySubmissions(ctx, sessionTokenFromContext(ctx), limit)
	switch {
	case errors.Is(err, torrents.ErrTorrentReadInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_submission_query", "提交记录查询无效", "最近提交记录的返回数量必须在 1 到 50 之间。")
		return generated.ListMyTorrentSubmissions400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看自己的种子提交记录。")
		return generated.ListMyTorrentSubmissions401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_submission_read_denied", "无法查看提交记录", "当前账户没有 torrent.submission.read.self 能力。")
		return generated.ListMyTorrentSubmissions403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListMyTorrentSubmissions200JSONResponse{
		Body:    myTorrentSubmissionPageDTO(page),
		Headers: generated.ListMyTorrentSubmissions200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func torrentPublicDetailDTO(detail torrents.PublicDetail) generated.TorrentPublicDetail {
	facets := make([]generated.TorrentPublicFacet, 0, len(detail.Facets))
	for _, facet := range detail.Facets {
		facets = append(facets, generated.TorrentPublicFacet{
			FacetId: facet.FacetID, FacetName: facet.FacetName,
			OptionKey: facet.OptionKey, OptionLabel: facet.OptionLabel,
		})
	}
	externalIdentifiers := make([]generated.TorrentExternalIdentifier, 0, len(detail.ExternalIdentifiers))
	for _, identifier := range detail.ExternalIdentifiers {
		externalIdentifiers = append(externalIdentifiers, generated.TorrentExternalIdentifier{
			Provider:   generated.TorrentExternalIdentifierProvider(identifier.Provider),
			ExternalId: identifier.ExternalID,
		})
	}
	return generated.TorrentPublicDetail{
		Id:       int64(detail.ID),
		Category: generated.Category{Id: detail.Category.ID, Name: detail.Category.Name},
		Title:    detail.Title, Subtitle: detail.Subtitle, ContentName: detail.ContentName,
		UploaderDisplayName: detail.UploaderDisplayName, Anonymous: detail.Anonymous,
		Promotion: generated.TorrentPublicDetailPromotion(detail.Promotion), PromotionEndsAt: detail.PromotionEndsAt,
		StickyUntil: detail.StickyUntil,
		Facets:      facets, ExternalIdentifiers: externalIdentifiers, InfoHashV1: detail.InfoHashV1.Hex(),
		TotalSizeBytes: detail.TotalSizeBytes, PayloadSizeBytes: detail.PayloadSizeBytes,
		FileCount: detail.FileCount, PaddingFileCount: detail.PaddingFileCount,
		ScreenshotCount:  detail.ScreenshotCount,
		PieceLengthBytes: detail.PieceLengthBytes, PieceCount: detail.PieceCount,
		State:       generated.TorrentPublicDetailStatePublished,
		SubmittedAt: detail.SubmittedAt, PublishedAt: detail.PublishedAt,
	}
}

func torrentFilePageDTO(page torrents.PublicFilePage) generated.TorrentFilePage {
	items := make([]generated.TorrentFile, 0, len(page.Items))
	for _, file := range page.Items {
		items = append(items, generated.TorrentFile{
			FileIndex: file.Index, DisplayPath: file.DisplayPath,
			SizeBytes: file.SizeBytes, IsPadding: file.IsPadding,
		})
	}
	return generated.TorrentFilePage{
		TorrentId: int64(page.TorrentID), Items: items,
		Total: page.Total, Limit: page.Limit, Offset: page.Offset,
	}
}

func myTorrentSubmissionPageDTO(page torrents.MySubmissionPage) generated.MyTorrentSubmissionPage {
	items := make([]generated.MyTorrentSubmission, 0, len(page.Items))
	for _, submission := range page.Items {
		var latestReview *generated.TorrentReviewFeedback
		var latestContentChange *generated.MyPublishedTorrentContentChangeStatus
		var latestScreenshotChange *generated.MyPublishedTorrentScreenshotChangeStatus
		var latestWithdrawal *generated.MyTorrentWithdrawalStatus
		resubmissionAllowed := false
		if submission.LatestReview != nil {
			latestReview = &generated.TorrentReviewFeedback{
				Outcome:    generated.TorrentReviewFeedbackOutcome(submission.LatestReview.Outcome),
				ReasonCode: generated.TorrentReviewReasonCode(submission.LatestReview.ReasonCode),
				Reason:     submission.LatestReview.Reason,
				DecidedAt:  submission.LatestReview.DecidedAt,
			}
			// This is a safe discoverability projection only. The write use case
			// independently rechecks ownership, capability and the latest decision.
			resubmissionAllowed = review.MetadataResubmissionAllowed(
				submission.State,
				review.ReasonCode(submission.LatestReview.ReasonCode),
			)
		}
		if submission.ContentChange != nil {
			latestContentChange = &generated.MyPublishedTorrentContentChangeStatus{
				Status:      generated.PublishedTorrentContentChangeStatus(submission.ContentChange.Status),
				SubmittedAt: submission.ContentChange.SubmittedAt, DecidedAt: submission.ContentChange.DecidedAt,
				DecisionReason: submission.ContentChange.DecisionReason,
			}
		}
		if submission.ScreenshotChange != nil {
			latestScreenshotChange = &generated.MyPublishedTorrentScreenshotChangeStatus{
				Status:      generated.PublishedTorrentScreenshotChangeStatus(submission.ScreenshotChange.Status),
				SubmittedAt: submission.ScreenshotChange.SubmittedAt, DecidedAt: submission.ScreenshotChange.DecidedAt,
				DecisionReason: submission.ScreenshotChange.DecisionReason,
			}
		}
		if submission.Withdrawal != nil {
			latestWithdrawal = &generated.MyTorrentWithdrawalStatus{
				Status:      generated.TorrentWithdrawalStatus(submission.Withdrawal.Status),
				SubmittedAt: submission.Withdrawal.SubmittedAt, DecidedAt: submission.Withdrawal.DecidedAt,
				DecisionReason: submission.Withdrawal.DecisionReason,
			}
		}
		items = append(items, generated.MyTorrentSubmission{
			Id:       int64(submission.ID),
			Category: generated.Category{Id: submission.Category.ID, Name: submission.Category.Name},
			Title:    submission.Title, Subtitle: submission.Subtitle, ContentName: submission.ContentName,
			InfoHashV1: submission.InfoHashV1.Hex(), TotalSizeBytes: submission.TotalSizeBytes,
			FileCount: submission.FileCount, State: generated.TorrentLifecycleState(submission.State),
			Version: submission.Version, SubmittedAt: submission.SubmittedAt,
			PublishedAt: submission.PublishedAt, StateChangedAt: submission.StateChangedAt,
			LatestReview: latestReview, LatestContentChange: latestContentChange, LatestScreenshotChange: latestScreenshotChange, LatestWithdrawal: latestWithdrawal,
			ResubmissionAllowed: resubmissionAllowed,
		})
	}
	return generated.MyTorrentSubmissionPage{Items: items, Total: page.Total, Limit: page.Limit}
}
