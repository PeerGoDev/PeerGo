package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/review"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

type TorrentReviewService interface {
	ListAssignments(context.Context, string, int) (review.ReviewAssignmentPage, error)
	GetAssignment(context.Context, string, torrents.TorrentID) (review.ReviewDetail, error)
	ListReviewed(context.Context, string, int) (review.ReviewedTorrentPage, error)
	AssignmentFiles(context.Context, string, torrents.TorrentID, int, int) (torrents.PublicFilePage, error)
	AssignmentCover(context.Context, string, torrents.TorrentID) (torrents.PublicCover, error)
	AssignmentScreenshot(context.Context, string, torrents.TorrentID, int) (torrents.PublicScreenshot, error)
	Vote(context.Context, string, string, review.VoteInput) (review.VoteResult, error)
	ListPending(context.Context, authz.StaffActor, int) (review.PendingTorrentPage, error)
	Decide(context.Context, authz.StaffActor, review.DecideInput) (review.DecisionResult, error)
}

func (h *Handler) GetMyTorrentReviewAssignment(ctx context.Context, request generated.GetMyTorrentReviewAssignmentRequestObject) (generated.GetMyTorrentReviewAssignmentResponseObject, error) {
	detail, err := h.torrentReview.GetAssignment(ctx, sessionTokenFromContext(ctx), torrents.TorrentID(request.TorrentId))
	if problem, status, handled := torrentReviewerReadProblem(ctx, err); handled {
		switch status {
		case http.StatusBadRequest:
			return generated.GetMyTorrentReviewAssignment400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
		case http.StatusUnauthorized:
			return generated.GetMyTorrentReviewAssignment401ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusForbidden:
			return generated.GetMyTorrentReviewAssignment403ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.GetMyTorrentReviewAssignment404ApplicationProblemPlusJSONResponse(problem), nil
		}
	}
	if err != nil {
		return nil, err
	}
	return generated.GetMyTorrentReviewAssignment200JSONResponse{
		Body:    torrentReviewDetailDTO(detail),
		Headers: generated.GetMyTorrentReviewAssignment200ResponseHeaders{CacheControl: "private, no-store"},
	}, nil
}

func (h *Handler) ListMyReviewedTorrentReviews(ctx context.Context, request generated.ListMyReviewedTorrentReviewsRequestObject) (generated.ListMyReviewedTorrentReviewsResponseObject, error) {
	limit := 20
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	page, err := h.torrentReview.ListReviewed(ctx, sessionTokenFromContext(ctx), limit)
	if problem, status, handled := torrentReviewerReadProblem(ctx, err); handled {
		switch status {
		case http.StatusBadRequest:
			return generated.ListMyReviewedTorrentReviews400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
		case http.StatusUnauthorized:
			return generated.ListMyReviewedTorrentReviews401ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.ListMyReviewedTorrentReviews403ApplicationProblemPlusJSONResponse(problem), nil
		}
	}
	if err != nil {
		return nil, err
	}
	items := make([]generated.ReviewedTorrentReview, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, reviewedTorrentReviewDTO(item))
	}
	return generated.ListMyReviewedTorrentReviews200JSONResponse{
		Body:    generated.ReviewedTorrentReviewPage{Items: items, Total: page.Total},
		Headers: generated.ListMyReviewedTorrentReviews200ResponseHeaders{CacheControl: "private, no-store"},
	}, nil
}

func (h *Handler) ListMyTorrentReviewFiles(ctx context.Context, request generated.ListMyTorrentReviewFilesRequestObject) (generated.ListMyTorrentReviewFilesResponseObject, error) {
	limit, offset := torrents.DefaultTorrentFileLimit, 0
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.torrentReview.AssignmentFiles(ctx, sessionTokenFromContext(ctx), torrents.TorrentID(request.TorrentId), limit, offset)
	if problem, status, handled := torrentReviewerReadProblem(ctx, err); handled {
		switch status {
		case http.StatusBadRequest:
			return generated.ListMyTorrentReviewFiles400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
		case http.StatusUnauthorized:
			return generated.ListMyTorrentReviewFiles401ApplicationProblemPlusJSONResponse(problem), nil
		case http.StatusForbidden:
			return generated.ListMyTorrentReviewFiles403ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return generated.ListMyTorrentReviewFiles404ApplicationProblemPlusJSONResponse(problem), nil
		}
	}
	if err != nil {
		return nil, err
	}
	return generated.ListMyTorrentReviewFiles200JSONResponse{
		Body:    torrentFilePageDTO(page),
		Headers: generated.ListMyTorrentReviewFiles200ResponseHeaders{CacheControl: "private, no-store"},
	}, nil
}

func (h *Handler) GetMyTorrentReviewCover(ctx context.Context, request generated.GetMyTorrentReviewCoverRequestObject) (generated.GetMyTorrentReviewCoverResponseObject, error) {
	cover, err := h.torrentReview.AssignmentCover(ctx, sessionTokenFromContext(ctx), torrents.TorrentID(request.TorrentId))
	if response, handled := myTorrentReviewCoverError(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	headers := generated.GetMyTorrentReviewCover200ResponseHeaders{CacheControl: "private, no-store", ETag: cover.ETag}
	reader := bytes.NewReader(cover.Data)
	switch cover.ContentType {
	case "image/gif":
		return generated.GetMyTorrentReviewCover200ImagegifResponse{Body: reader, Headers: headers, ContentLength: int64(len(cover.Data))}, nil
	case "image/jpeg":
		return generated.GetMyTorrentReviewCover200ImagejpegResponse{Body: reader, Headers: headers, ContentLength: int64(len(cover.Data))}, nil
	case "image/png":
		return generated.GetMyTorrentReviewCover200ImagepngResponse{Body: reader, Headers: headers, ContentLength: int64(len(cover.Data))}, nil
	case "image/webp":
		return generated.GetMyTorrentReviewCover200ImagewebpResponse{Body: reader, Headers: headers, ContentLength: int64(len(cover.Data))}, nil
	default:
		return nil, torrents.ErrTorrentCoverConflict
	}
}

func (h *Handler) GetMyTorrentReviewScreenshot(ctx context.Context, request generated.GetMyTorrentReviewScreenshotRequestObject) (generated.GetMyTorrentReviewScreenshotResponseObject, error) {
	screenshot, err := h.torrentReview.AssignmentScreenshot(ctx, sessionTokenFromContext(ctx), torrents.TorrentID(request.TorrentId), request.Position)
	if response, handled := myTorrentReviewScreenshotError(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	headers := generated.GetMyTorrentReviewScreenshot200ResponseHeaders{CacheControl: "private, no-store", ETag: screenshot.ETag}
	reader := bytes.NewReader(screenshot.Data)
	switch screenshot.ContentType {
	case "image/gif":
		return generated.GetMyTorrentReviewScreenshot200ImagegifResponse{Body: reader, Headers: headers, ContentLength: int64(len(screenshot.Data))}, nil
	case "image/jpeg":
		return generated.GetMyTorrentReviewScreenshot200ImagejpegResponse{Body: reader, Headers: headers, ContentLength: int64(len(screenshot.Data))}, nil
	case "image/png":
		return generated.GetMyTorrentReviewScreenshot200ImagepngResponse{Body: reader, Headers: headers, ContentLength: int64(len(screenshot.Data))}, nil
	case "image/webp":
		return generated.GetMyTorrentReviewScreenshot200ImagewebpResponse{Body: reader, Headers: headers, ContentLength: int64(len(screenshot.Data))}, nil
	default:
		return nil, torrents.ErrTorrentScreenshotConflict
	}
}

func (h *Handler) ListMyTorrentReviewAssignments(ctx context.Context, request generated.ListMyTorrentReviewAssignmentsRequestObject) (generated.ListMyTorrentReviewAssignmentsResponseObject, error) {
	limit := 20
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	page, err := h.torrentReview.ListAssignments(ctx, sessionTokenFromContext(ctx), limit)
	switch {
	case errors.Is(err, review.ErrTorrentReviewInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_review_query", "审核队列参数无效", "limit 必须位于 1 到 50 之间。")
		return generated.ListMyTorrentReviewAssignments400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看种审任务。")
		return generated.ListMyTorrentReviewAssignments401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_review_vote_denied", "无法查看种审任务", "当前账号没有种审投票权限。")
		return generated.ListMyTorrentReviewAssignments403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, review.ErrTorrentReviewMembership):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_review_membership_required", "需要有效种审组资格", "请先加入种审组，或联系管理员检查成员资格是否仍在有效期内。")
		return generated.ListMyTorrentReviewAssignments403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	items := make([]generated.MyTorrentReviewAssignment, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, myTorrentReviewAssignmentDTO(item))
	}
	return generated.ListMyTorrentReviewAssignments200JSONResponse{Items: items, Total: page.Total}, nil
}

func (h *Handler) CreateMyTorrentReviewVote(ctx context.Context, request generated.CreateMyTorrentReviewVoteRequestObject) (generated.CreateMyTorrentReviewVoteResponseObject, error) {
	if request.Body == nil {
		return createMyTorrentReviewVoteBadRequest(ctx), nil
	}
	result, err := h.torrentReview.Vote(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), review.VoteInput{
		VoteID: request.Params.IdempotencyKey, TorrentID: torrents.TorrentID(request.TorrentId),
		ExpectedVersion: request.Body.ExpectedVersion, Decision: review.Decision(request.Body.Decision),
		ReasonCode: review.ReasonCode(request.Body.ReasonCode), Reason: request.Body.Reason,
	})
	switch {
	case errors.Is(err, review.ErrTorrentReviewInput):
		return createMyTorrentReviewVoteBadRequest(ctx), nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后提交种审票。")
		return generated.CreateMyTorrentReviewVote401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.CreateMyTorrentReviewVote403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_review_vote_denied", "无法提交种审票", "当前账号没有种审投票权限。")
		return generated.CreateMyTorrentReviewVote403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, review.ErrTorrentReviewMembership):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_review_membership_required", "需要有效种审组资格", "当前种审组成员资格不存在或已经失效。")
		return generated.CreateMyTorrentReviewVote403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, review.ErrTorrentReviewSelf):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_self_review_denied", "不能审核自己的种子", "请由另一名种审组成员处理该提交。")
		return generated.CreateMyTorrentReviewVote403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, review.ErrTorrentReviewNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_review_not_found", "种子不存在", "目标种子不存在或已不在当前数据集中。")
		return generated.CreateMyTorrentReviewVote404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, review.ErrTorrentReviewAlreadyVoted):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_review_already_voted", "本轮已经投票", "每名种审组成员在同一审核轮次只能投一票。")
		return generated.CreateMyTorrentReviewVote409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, review.ErrTorrentReviewRoundEscalated):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_review_round_escalated", "本轮已转管理员处理", "四票形成 2:2，本轮不再接受普通审核票。")
		return generated.CreateMyTorrentReviewVote409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, review.ErrTorrentReviewIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_review_idempotency_conflict", "审核请求内容已经变化", "请勿复用其他种审票的 Idempotency-Key。")
		return generated.CreateMyTorrentReviewVote409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, review.ErrTorrentReviewVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_review_version_conflict", "种子版本已经变化", "请重新载入审核对象并核对最新状态。")
		return generated.CreateMyTorrentReviewVote409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, review.ErrTorrentReviewStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_review_state_conflict", "种子已不再等待审核", "该种子已经被本轮投票或管理员处理。")
		return generated.CreateMyTorrentReviewVote409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, review.ErrTorrentReviewCategoryUnavailable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_review_category_unavailable", "分类当前不可发布", "请先由管理员核对分类状态。")
		return generated.CreateMyTorrentReviewVote409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, review.ErrTorrentReviewObjectUnavailable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_review_object_unavailable", "种子对象当前不可发布", "没有可验证的种子文件存储位置，请先完成存储恢复。")
		return generated.CreateMyTorrentReviewVote409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.CreateMyTorrentReviewVote201JSONResponse(torrentReviewVoteDTO(result)), nil
}

func (h *Handler) ListPendingTorrentReviews(ctx context.Context, request generated.ListPendingTorrentReviewsRequestObject) (generated.ListPendingTorrentReviewsResponseObject, error) {
	session, authenticationProblem, err := h.authenticateStaffRead(ctx)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.ListPendingTorrentReviews401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.ListPendingTorrentReviews403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	limit := 20
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	page, err := h.torrentReview.ListPending(ctx, staffActor(session), limit)
	if errors.Is(err, review.ErrTorrentReviewInput) {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_review_query", "审核队列参数无效", "limit 必须位于 1 到 50 之间。")
		return generated.ListPendingTorrentReviews400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_review_read_denied", "无法查看种子审核队列", "当前后台身份没有 torrent.review 权限。")
		return generated.ListPendingTorrentReviews403ApplicationProblemPlusJSONResponse(problem), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]generated.PendingTorrentReview, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, pendingTorrentReviewDTO(item))
	}
	return generated.ListPendingTorrentReviews200JSONResponse{Items: items, Total: page.Total}, nil
}

func (h *Handler) DecideTorrentReview(ctx context.Context, request generated.DecideTorrentReviewRequestObject) (generated.DecideTorrentReviewResponseObject, error) {
	if request.Body == nil {
		return decideTorrentReviewBadRequest(ctx), nil
	}
	session, authenticationProblem, err := h.authenticateStaffWrite(ctx, request.Params.XCSRFToken)
	if err != nil {
		return nil, err
	}
	if authenticationProblem != nil {
		if authenticationProblem.Status == http.StatusUnauthorized {
			return generated.DecideTorrentReview401ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
		}
		return generated.DecideTorrentReview403ApplicationProblemPlusJSONResponse(*authenticationProblem), nil
	}
	result, err := h.torrentReview.Decide(ctx, staffActor(session), review.DecideInput{
		DecisionID: request.Params.IdempotencyKey, TorrentID: torrents.TorrentID(request.TorrentId),
		ExpectedVersion: request.Body.ExpectedVersion, Decision: review.Decision(request.Body.Decision),
		ReasonCode: review.ReasonCode(request.Body.ReasonCode), Reason: request.Body.Reason,
	})
	if response, handled := torrentReviewErrorResponse(ctx, err); handled {
		return response, nil
	}
	if err != nil {
		return nil, err
	}
	return generated.DecideTorrentReview201JSONResponse(torrentReviewDecisionDTO(result)), nil
}

func torrentReviewErrorResponse(ctx context.Context, err error) (generated.DecideTorrentReviewResponseObject, bool) {
	switch {
	case err == nil:
		return nil, false
	case errors.Is(err, review.ErrTorrentReviewInput):
		return decideTorrentReviewBadRequest(ctx), true
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_review_denied", "无法作出审核决定", "当前后台身份没有 torrent.review 权限。")
		return generated.DecideTorrentReview403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentReviewSelf):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_self_review_denied", "不能审核自己的种子", "请由另一名具备权限的审核员处理该提交。")
		return generated.DecideTorrentReview403ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentReviewNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_review_not_found", "种子不存在", "目标种子不存在或已不在当前数据集中。")
		return generated.DecideTorrentReview404ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentReviewIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_review_idempotency_conflict", "审核请求内容已经变化", "请勿复用其他审核决定的 Idempotency-Key。")
		return generated.DecideTorrentReview409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentReviewVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_review_version_conflict", "种子版本已经变化", "请重新载入审核对象并核对最新状态。")
		return generated.DecideTorrentReview409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentReviewStateConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_review_state_conflict", "种子已不再等待审核", "该种子已经被其他审核决定处理。")
		return generated.DecideTorrentReview409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentReviewCategoryUnavailable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_review_category_unavailable", "分类当前不可发布", "请先核对分类状态，再决定是否要求上传者调整。")
		return generated.DecideTorrentReview409ApplicationProblemPlusJSONResponse(problem), true
	case errors.Is(err, review.ErrTorrentReviewObjectUnavailable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "torrent_review_object_unavailable", "种子对象当前不可发布", "没有可验证的对象存储位置，请先完成存储恢复。")
		return generated.DecideTorrentReview409ApplicationProblemPlusJSONResponse(problem), true
	default:
		return nil, false
	}
}

func decideTorrentReviewBadRequest(ctx context.Context) generated.DecideTorrentReview400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_review", "审核决定无效", "请检查版本、决定、原因类别和人工说明。")
	return generated.DecideTorrentReview400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func pendingTorrentReviewDTO(item review.PendingTorrent) generated.PendingTorrentReview {
	return generated.PendingTorrentReview{
		Id: int64(item.ID), UploaderId: item.UploaderID, UploaderDisplayName: item.UploaderDisplayName,
		CategoryId: item.CategoryID, CategoryName: item.CategoryName, Title: item.Title,
		Subtitle: item.Subtitle, ContentName: item.ContentName, InfoHashV1: item.InfoHashV1.Hex(),
		TotalSizeBytes: item.TotalSizeBytes, FileCount: item.FileCount, Version: item.Version,
		SubmittedAt: item.SubmittedAt, ReviewRequestedAt: item.ReviewRequestedAt,
	}
}

func myTorrentReviewAssignmentDTO(item review.ReviewAssignment) generated.MyTorrentReviewAssignment {
	pending := pendingTorrentReviewDTO(item.PendingTorrent)
	return generated.MyTorrentReviewAssignment{
		Id: pending.Id, UploaderId: pending.UploaderId, UploaderDisplayName: pending.UploaderDisplayName,
		CategoryId: pending.CategoryId, CategoryName: pending.CategoryName, Title: pending.Title,
		Subtitle: pending.Subtitle, ContentName: pending.ContentName, InfoHashV1: pending.InfoHashV1,
		TotalSizeBytes: pending.TotalSizeBytes, FileCount: pending.FileCount, Version: pending.Version,
		SubmittedAt: pending.SubmittedAt, ReviewRequestedAt: pending.ReviewRequestedAt,
		VotesCast:     item.VotesCast,
		RequiredVotes: generated.MyTorrentReviewAssignmentRequiredVotes(item.RequiredVotes),
		MaximumVotes:  generated.MyTorrentReviewAssignmentMaximumVotes(item.MaximumVotes),
	}
}

func torrentReviewDetailDTO(detail review.ReviewDetail) generated.MyTorrentReviewDetail {
	assignment := detail.ReviewAssignment
	evidence := detail.Evidence
	facets := make([]generated.TorrentPublicFacet, 0, len(evidence.Facets))
	for _, facet := range evidence.Facets {
		facets = append(facets, generated.TorrentPublicFacet{
			FacetId: facet.FacetID, FacetName: facet.FacetName,
			OptionKey: facet.OptionKey, OptionLabel: facet.OptionLabel,
		})
	}
	identifiers := make([]generated.TorrentExternalIdentifier, 0, len(evidence.ExternalIdentifiers))
	for _, identifier := range evidence.ExternalIdentifiers {
		identifiers = append(identifiers, generated.TorrentExternalIdentifier{
			Provider: generated.TorrentExternalIdentifierProvider(identifier.Provider), ExternalId: identifier.ExternalID,
		})
	}
	return generated.MyTorrentReviewDetail{
		Id: int64(assignment.ID), UploaderId: assignment.UploaderID,
		UploaderDisplayName: assignment.UploaderDisplayName,
		CategoryId:          assignment.CategoryID, CategoryName: assignment.CategoryName,
		Title: assignment.Title, Subtitle: assignment.Subtitle, ContentName: assignment.ContentName,
		InfoHashV1: assignment.InfoHashV1.Hex(), TotalSizeBytes: assignment.TotalSizeBytes,
		FileCount: assignment.FileCount, Version: assignment.Version,
		SubmittedAt: assignment.SubmittedAt, ReviewRequestedAt: assignment.ReviewRequestedAt,
		VotesCast:     assignment.VotesCast,
		RequiredVotes: generated.MyTorrentReviewDetailRequiredVotes(assignment.RequiredVotes),
		MaximumVotes:  generated.MyTorrentReviewDetailMaximumVotes(assignment.MaximumVotes),
		Anonymous:     evidence.Anonymous, Facets: facets, ExternalIdentifiers: identifiers,
		PayloadSizeBytes: evidence.PayloadSizeBytes, PaddingFileCount: evidence.PaddingFileCount,
		ScreenshotCount: evidence.ScreenshotCount, PieceLengthBytes: evidence.PieceLengthBytes,
		PieceCount: evidence.PieceCount, Description: evidence.Description,
		DescriptionFormat: generated.MyTorrentReviewDetailDescriptionFormat(evidence.DescriptionFormat),
		MediaInfo:         evidence.MediaInfo,
	}
}

func reviewedTorrentReviewDTO(item review.ReviewedTorrent) generated.ReviewedTorrentReview {
	pending := pendingTorrentReviewDTO(item.PendingTorrent)
	return generated.ReviewedTorrentReview{
		Id: pending.Id, UploaderId: pending.UploaderId, UploaderDisplayName: pending.UploaderDisplayName,
		CategoryId: pending.CategoryId, CategoryName: pending.CategoryName,
		Title: pending.Title, Subtitle: pending.Subtitle, ContentName: pending.ContentName,
		InfoHashV1: pending.InfoHashV1, TotalSizeBytes: pending.TotalSizeBytes,
		FileCount: pending.FileCount, Version: pending.Version,
		SubmittedAt: pending.SubmittedAt, ReviewRequestedAt: pending.ReviewRequestedAt,
		VoteId: item.VoteID, RoundId: item.RoundID,
		Decision:   generated.ReviewedTorrentReviewDecision(item.Decision),
		ReasonCode: generated.TorrentReviewReasonCode(item.ReasonCode), Reason: item.Reason,
		VotedAt: item.VotedAt, ApproveCount: item.ApproveCount, RejectCount: item.RejectCount,
		Outcome: generated.TorrentReviewRoundOutcome(item.Outcome),
	}
}

func torrentReviewerReadProblem(ctx context.Context, err error) (generated.Problem, int, bool) {
	switch {
	case err == nil:
		return generated.Problem{}, 0, false
	case errors.Is(err, review.ErrTorrentReviewInput), errors.Is(err, torrents.ErrTorrentReadInput):
		return newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_review_query", "审核请求无效", "请检查种子编号或分页参数。"), http.StatusBadRequest, true
	case errors.Is(err, identity.ErrSessionNotFound):
		return newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后查看种审任务。"), http.StatusUnauthorized, true
	case errors.Is(err, authz.ErrForbidden):
		return newProblemFromContext(ctx, http.StatusForbidden, "torrent_review_vote_denied", "无法查看种审任务", "当前账号没有种审投票权限。"), http.StatusForbidden, true
	case errors.Is(err, review.ErrTorrentReviewMembership):
		return newProblemFromContext(ctx, http.StatusForbidden, "torrent_review_membership_required", "需要有效种审组资格", "当前种审组成员资格不存在或已经失效。"), http.StatusForbidden, true
	case errors.Is(err, review.ErrTorrentReviewNotFound), errors.Is(err, torrents.ErrTorrentReadNotFound),
		errors.Is(err, torrents.ErrTorrentCoverNotFound), errors.Is(err, torrents.ErrTorrentScreenshotNotFound):
		return newProblemFromContext(ctx, http.StatusNotFound, "torrent_review_not_found", "审核任务不存在", "该种子已不再等待您审核，或本轮已经投过票。"), http.StatusNotFound, true
	default:
		return generated.Problem{}, 0, false
	}
}

func myTorrentReviewCoverError(ctx context.Context, err error) (generated.GetMyTorrentReviewCoverResponseObject, bool) {
	if problem, status, handled := torrentReviewerReadProblem(ctx, err); handled {
		switch status {
		case http.StatusBadRequest:
			return generated.GetMyTorrentReviewCover400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, true
		case http.StatusUnauthorized:
			return generated.GetMyTorrentReviewCover401ApplicationProblemPlusJSONResponse(problem), true
		case http.StatusForbidden:
			return generated.GetMyTorrentReviewCover403ApplicationProblemPlusJSONResponse(problem), true
		default:
			return generated.GetMyTorrentReviewCover404ApplicationProblemPlusJSONResponse(problem), true
		}
	}
	if errors.Is(err, torrents.ErrTorrentCoverUnavailable) || errors.Is(err, torrents.ErrTorrentCoverConflict) {
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "torrent_review_cover_unavailable", "审核封面暂时不可用", "对象存储读取或完整性校验暂时失败。")
		return generated.GetMyTorrentReviewCoverdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, true
	}
	return nil, false
}

func myTorrentReviewScreenshotError(ctx context.Context, err error) (generated.GetMyTorrentReviewScreenshotResponseObject, bool) {
	if problem, status, handled := torrentReviewerReadProblem(ctx, err); handled {
		switch status {
		case http.StatusBadRequest:
			return generated.GetMyTorrentReviewScreenshot400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, true
		case http.StatusUnauthorized:
			return generated.GetMyTorrentReviewScreenshot401ApplicationProblemPlusJSONResponse(problem), true
		case http.StatusForbidden:
			return generated.GetMyTorrentReviewScreenshot403ApplicationProblemPlusJSONResponse(problem), true
		default:
			return generated.GetMyTorrentReviewScreenshot404ApplicationProblemPlusJSONResponse(problem), true
		}
	}
	if errors.Is(err, torrents.ErrTorrentScreenshotUnavailable) || errors.Is(err, torrents.ErrTorrentScreenshotConflict) {
		problem := newProblemFromContext(ctx, http.StatusServiceUnavailable, "torrent_review_screenshot_unavailable", "审核截图暂时不可用", "对象存储读取或完整性校验暂时失败。")
		return generated.GetMyTorrentReviewScreenshotdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: http.StatusServiceUnavailable}, true
	}
	return nil, false
}

func torrentReviewVoteDTO(result review.VoteResult) generated.TorrentReviewVoteResult {
	dto := generated.TorrentReviewVoteResult{
		VoteId: result.VoteID, RoundId: result.RoundID, TorrentId: int64(result.TorrentID),
		Decision:      generated.TorrentReviewVoteResultDecision(result.Decision),
		VotesCast:     result.VotesCast,
		RequiredVotes: generated.TorrentReviewVoteResultRequiredVotes(result.RequiredVotes),
		MaximumVotes:  generated.TorrentReviewVoteResultMaximumVotes(result.MaximumVotes),
		Outcome:       generated.TorrentReviewRoundOutcome(result.Outcome), VotedAt: result.VotedAt,
	}
	if result.FinalDecision != nil {
		finalDecision := torrentReviewDecisionDTO(*result.FinalDecision)
		dto.FinalDecision = &finalDecision
	}
	return dto
}

func createMyTorrentReviewVoteBadRequest(ctx context.Context) generated.CreateMyTorrentReviewVote400ApplicationProblemPlusJSONResponse {
	problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_torrent_review_vote", "种审票无效", "请检查种子版本、决定、原因类别和不少于 10 个字的审核说明。")
	return generated.CreateMyTorrentReviewVote400ApplicationProblemPlusJSONResponse{
		ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
	}
}

func torrentReviewDecisionDTO(result review.DecisionResult) generated.TorrentReviewDecisionResult {
	return generated.TorrentReviewDecisionResult{
		DecisionId: result.DecisionID, TorrentId: int64(result.TorrentID),
		Decision:   generated.TorrentReviewDecisionResultDecision(result.Decision),
		ReasonCode: generated.TorrentReviewReasonCode(result.ReasonCode),
		State:      generated.TorrentReviewDecisionResultState(result.State), Version: result.Version,
		DecidedAt: result.OccurredAt,
	}
}
