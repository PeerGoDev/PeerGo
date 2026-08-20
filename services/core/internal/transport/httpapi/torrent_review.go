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

type TorrentReviewService interface {
	ListAssignments(context.Context, string, int) (review.ReviewAssignmentPage, error)
	Vote(context.Context, string, string, review.VoteInput) (review.VoteResult, error)
	ListPending(context.Context, authz.StaffActor, int) (review.PendingTorrentPage, error)
	Decide(context.Context, authz.StaffActor, review.DecideInput) (review.DecisionResult, error)
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
