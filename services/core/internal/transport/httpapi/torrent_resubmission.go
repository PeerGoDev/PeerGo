package httpapi

import (
	"context"
	"errors"
	"net/http"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

// TorrentResubmissionService is deliberately separate from the initial upload
// surface. This command can edit only catalog metadata on an existing rejected
// aggregate; it cannot replace metainfo, files, info hashes or stored objects.
type TorrentResubmissionService interface {
	Resubmit(context.Context, string, string, review.ResubmitInput) (review.ResubmissionResult, error)
}

// ResubmitMyTorrentSubmission binds the uploader's correction response to the
// immutable review decision it addresses. Authentication, ownership and the
// allowed rejection reason are rechecked inside the use case transaction.
func (h *Handler) ResubmitMyTorrentSubmission(
	ctx context.Context,
	request generated.ResubmitMyTorrentSubmissionRequestObject,
) (generated.ResubmitMyTorrentSubmissionResponseObject, error) {
	if request.Body == nil {
		return torrentResubmissionBadRequest(ctx), nil
	}

	result, err := h.torrentResubmission.Resubmit(
		ctx,
		sessionTokenFromContext(ctx),
		string(request.Params.XCSRFToken),
		review.ResubmitInput{
			ID:              request.Params.IdempotencyKey,
			TorrentID:       torrents.TorrentID(request.TorrentId),
			ExpectedVersion: request.Body.ExpectedVersion,
			CategoryID:      request.Body.CategoryId,
			Title:           request.Body.Title,
			Subtitle:        request.Body.Subtitle,
			CorrectionNote:  request.Body.CorrectionNote,
		},
	)
	if response, handled := torrentResubmissionErrorResponse(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}

	return generated.ResubmitMyTorrentSubmission200JSONResponse{
		Body: generated.TorrentResubmission{
			Id:                result.ID,
			TorrentId:         int64(result.TorrentID),
			State:             generated.TorrentResubmissionStatePendingReview,
			Version:           result.Version,
			CategoryId:        result.Metadata.CategoryID,
			Title:             result.Metadata.Title,
			Subtitle:          result.Metadata.Subtitle,
			ReviewRequestedAt: result.ReviewRequestedAt,
		},
		Headers: generated.ResubmitMyTorrentSubmission200ResponseHeaders{CacheControl: "no-store"},
	}, nil
}

func torrentResubmissionErrorResponse(
	ctx context.Context,
	err error,
) (generated.ResubmitMyTorrentSubmissionResponseObject, bool) {
	switch {
	case err == nil:
		return nil, false
	case errors.Is(err, review.ErrTorrentResubmissionInput):
		return torrentResubmissionBadRequest(ctx), true
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "web_session_required", "需要登录", "请重新登录后再提交整改内容。")
		return generated.ResubmitMyTorrentSubmission401ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重新提交整改内容。")
		return generated.ResubmitMyTorrentSubmission403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentResubmissionEmailUnverified):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "verified_email_required", "需要先验证邮箱", "验证当前账户的邮箱后才能重新送审。")
		return generated.ResubmitMyTorrentSubmission403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_resubmission_denied", "无法重新送审", "当前账户不能整改并重新提交这条发布记录。")
		return generated.ResubmitMyTorrentSubmission403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentResubmissionNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_resubmission_not_found", "提交记录不存在", "该提交记录不存在，或不属于当前账户。")
		return generated.ResubmitMyTorrentSubmission404ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentResubmissionIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_resubmission_idempotency_conflict", "本次整改内容已经变化", "请刷新提交记录后重新开始整改。")
		return generated.ResubmitMyTorrentSubmission409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentResubmissionVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_resubmission_version_conflict", "提交记录已经变化", "请重新载入提交记录，确认最新审核状态后再操作。")
		return generated.ResubmitMyTorrentSubmission409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentResubmissionStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_resubmission_state_conflict", "当前状态不能重新送审", "该提交可能已经重新送审或被其他流程处理，请刷新页面。")
		return generated.ResubmitMyTorrentSubmission409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentResubmissionNotAllowed):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_resubmission_not_allowed", "本次驳回不能直接整改", "该审核结果需要重新发布资源或等待后续申诉功能，不能只修改发布资料。")
		return generated.ResubmitMyTorrentSubmission409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentResubmissionUnchanged):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_resubmission_unchanged", "发布资料没有变化", "请根据审核意见修改分类、标题或副标题后再重新送审。")
		return generated.ResubmitMyTorrentSubmission409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentResubmissionCategoryUnavailable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_resubmission_category_unavailable", "所选分类当前不可用", "请选择一个当前启用的种子分类。")
		return generated.ResubmitMyTorrentSubmission409ApplicationProblemPlusJSONResponse(problem), true
	default:
		return nil, false
	}
}

func torrentResubmissionBadRequest(ctx context.Context) generated.ResubmitMyTorrentSubmission400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_resubmission", "整改内容无效", "请检查分类、标题、副标题、当前版本和至少 10 个字的整改说明。")
	return generated.ResubmitMyTorrentSubmission400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}
