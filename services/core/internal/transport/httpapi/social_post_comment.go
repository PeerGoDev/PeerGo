package httpapi

import (
	"context"
	"errors"
	"net/http"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/social"
)

func (h *Handler) ListSocialPostComments(ctx context.Context, request generated.ListSocialPostCommentsRequestObject) (generated.ListSocialPostCommentsResponseObject, error) {
	limit := social.DefaultCommentLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	offset := 0
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	// CommentService keeps torrent/announcement public reads reusable. Posts
	// are member-only, so re-authorize the target read before touching the
	// shared comment projection instead of widening all comment reads.
	if _, err := h.socialPosts.FindVisible(ctx, sessionTokenFromContext(ctx), request.PostId); err != nil {
		switch {
		case errors.Is(err, identity.ErrSessionNotFound):
			problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请登录后查看动态评论。")
			return generated.ListSocialPostComments401ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, authz.ErrForbidden):
			problem := newProblemFromContext(ctx, http.StatusForbidden, "social_post_read_denied", "无法查看动态评论", "当前账号暂时不能使用动态圈。")
			return generated.ListSocialPostComments403ApplicationProblemPlusJSONResponse(problem), nil
		case errors.Is(err, social.ErrPostNotFound):
			problem := newProblemFromContext(ctx, http.StatusNotFound, "social_post_not_found", "动态不可用", "该动态不存在或已经被删除。")
			return generated.ListSocialPostComments404ApplicationProblemPlusJSONResponse(problem), nil
		default:
			return nil, err
		}
	}
	page, err := h.comments.ListPostComments(ctx, request.PostId, limit, offset)
	switch {
	case errors.Is(err, social.ErrCommentInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment_query", "评论查询无效", "每页数量或偏移量无效。")
		return generated.ListSocialPostComments400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, social.ErrCommentTargetNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "social_post_not_found", "动态不可用", "该动态不存在或已经被删除。")
		return generated.ListSocialPostComments404ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListSocialPostComments200JSONResponse(socialPostCommentPageDTO(page)), nil
}

func (h *Handler) CreateSocialPostComment(ctx context.Context, request generated.CreateSocialPostCommentRequestObject) (generated.CreateSocialPostCommentResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment", "无法发表评论", "请填写评论正文后重试。")
		return generated.CreateSocialPostComment400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	comment, err := h.comments.CreatePostComment(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), social.CreatePostCommentInput{
		RequestID: request.Params.IdempotencyKey, PostPublicID: request.PostId,
		ParentCommentID: request.Body.ParentCommentId, Body: request.Body.Body,
	})
	switch {
	case errors.Is(err, social.ErrCommentInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment", "无法发表评论", "评论正文需为 1 到 2000 个字符，且回复对象必须有效。")
		return generated.CreateSocialPostComment400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后发表评论。")
		return generated.CreateSocialPostComment401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF), errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "social_post_comment_create_denied", "无法发表评论", "请求验证失败或当前账号暂时不能使用评论功能。")
		return generated.CreateSocialPostComment403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentTargetNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "social_post_not_found", "动态不可用", "该动态不存在或已经被删除。")
		return generated.CreateSocialPostComment404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentParentNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "comment_parent_not_found", "回复对象不可用", "该评论不存在、已删除或不能继续回复。")
		return generated.CreateSocialPostComment404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentThreadLocked):
		problem := newProblemFromContext(ctx, http.StatusConflict, "comment_thread_locked", "评论区已关闭", "该动态的评论区目前不接受新评论。")
		return generated.CreateSocialPostComment409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "idempotency_conflict", "请求标识已被使用", "请刷新评论区后重新提交。")
		return generated.CreateSocialPostComment409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.CreateSocialPostComment201JSONResponse(commentDTO(comment)), nil
}

func socialPostCommentPageDTO(page social.CommentPage) generated.SocialPostCommentPage {
	items := make([]generated.Comment, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, commentDTO(item))
	}
	return generated.SocialPostCommentPage{PostId: page.Target.PostPublicID, Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}
