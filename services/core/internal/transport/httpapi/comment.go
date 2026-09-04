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

func (h *Handler) ListTorrentComments(ctx context.Context, request generated.ListTorrentCommentsRequestObject) (generated.ListTorrentCommentsResponseObject, error) {
	limit := social.DefaultCommentLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	offset := 0
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.comments.ListTorrentComments(ctx, request.TorrentId, limit, offset)
	switch {
	case errors.Is(err, social.ErrCommentInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment_query", "评论查询无效", "每页数量必须在 1 到 50 之间，偏移量必须在 0 到 99999 之间。")
		return generated.ListTorrentComments400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, social.ErrCommentTargetNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不可用", "该种子不存在或已经停止公开访问。")
		return generated.ListTorrentComments404ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListTorrentComments200JSONResponse(torrentCommentPageDTO(page)), nil
}

func (h *Handler) CreateTorrentComment(ctx context.Context, request generated.CreateTorrentCommentRequestObject) (generated.CreateTorrentCommentResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment", "无法发表评论", "请填写评论正文后重试。")
		return generated.CreateTorrentComment400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	comment, err := h.comments.CreateTorrentComment(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), social.CreateTorrentCommentInput{
		RequestID:       request.Params.IdempotencyKey,
		TorrentID:       request.TorrentId,
		ParentCommentID: request.Body.ParentCommentId,
		Body:            request.Body.Body,
	})
	switch {
	case errors.Is(err, social.ErrCommentInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment", "无法发表评论", "评论正文需为 1 到 2000 个字符，且回复对象必须有效。")
		return generated.CreateTorrentComment400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后发表评论。")
		return generated.CreateTorrentComment401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.CreateTorrentComment403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "torrent_comment_create_denied", "无法发表评论", "当前账号暂时不能使用评论功能。")
		return generated.CreateTorrentComment403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentTargetNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "torrent_not_found", "种子不可用", "该种子不存在或已经停止公开访问。")
		return generated.CreateTorrentComment404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentParentNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "comment_parent_not_found", "回复对象不可用", "该评论不存在、已删除或不能继续回复。")
		return generated.CreateTorrentComment404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentThreadLocked):
		problem := newProblemFromContext(ctx, http.StatusConflict, "comment_thread_locked", "评论区已关闭", "该种子的评论区目前不接受新评论。")
		return generated.CreateTorrentComment409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "idempotency_conflict", "请求标识已被使用", "请刷新评论区后重新提交。")
		return generated.CreateTorrentComment409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.CreateTorrentComment201JSONResponse(commentDTO(comment)), nil
}

func (h *Handler) ListAnnouncementComments(ctx context.Context, request generated.ListAnnouncementCommentsRequestObject) (generated.ListAnnouncementCommentsResponseObject, error) {
	limit := social.DefaultCommentLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	offset := 0
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	page, err := h.comments.ListAnnouncementComments(ctx, request.AnnouncementId, limit, offset)
	switch {
	case errors.Is(err, social.ErrCommentInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment_query", "评论查询无效", "每页数量必须在 1 到 50 之间，偏移量必须在 0 到 99999 之间。")
		return generated.ListAnnouncementComments400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, social.ErrCommentTargetNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "announcement_not_found", "公告不可用", "该公告不存在或尚未公开。")
		return generated.ListAnnouncementComments404ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListAnnouncementComments200JSONResponse(announcementCommentPageDTO(page)), nil
}

func (h *Handler) CreateAnnouncementComment(ctx context.Context, request generated.CreateAnnouncementCommentRequestObject) (generated.CreateAnnouncementCommentResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment", "无法发表评论", "请填写评论正文后重试。")
		return generated.CreateAnnouncementComment400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	comment, err := h.comments.CreateAnnouncementComment(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), social.CreateAnnouncementCommentInput{
		RequestID: request.Params.IdempotencyKey, AnnouncementID: request.AnnouncementId,
		ParentCommentID: request.Body.ParentCommentId, Body: request.Body.Body,
	})
	switch {
	case errors.Is(err, social.ErrCommentInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment", "无法发表评论", "评论正文需为 1 到 2000 个字符，且回复对象必须有效。")
		return generated.CreateAnnouncementComment400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后发表评论。")
		return generated.CreateAnnouncementComment401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.CreateAnnouncementComment403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "announcement_comment_create_denied", "无法发表评论", "当前账号暂时不能使用公告评论功能。")
		return generated.CreateAnnouncementComment403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentTargetNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "announcement_not_found", "公告不可用", "该公告不存在或尚未公开。")
		return generated.CreateAnnouncementComment404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentParentNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "comment_parent_not_found", "回复对象不可用", "该评论不存在、已删除或不能继续回复。")
		return generated.CreateAnnouncementComment404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentThreadLocked):
		problem := newProblemFromContext(ctx, http.StatusConflict, "comment_thread_locked", "评论区已关闭", "该公告的评论区目前不接受新评论。")
		return generated.CreateAnnouncementComment409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "idempotency_conflict", "请求标识已被使用", "请刷新评论区后重新提交。")
		return generated.CreateAnnouncementComment409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.CreateAnnouncementComment201JSONResponse(commentDTO(comment)), nil
}

func (h *Handler) UpdateMyComment(ctx context.Context, request generated.UpdateMyCommentRequestObject) (generated.UpdateMyCommentResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment", "无法编辑评论", "请填写评论正文后重试。")
		return generated.UpdateMyComment400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	}
	comment, err := h.comments.UpdateMyComment(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), social.UpdateCommentInput{
		CommentID:       request.CommentId,
		ExpectedVersion: request.Body.ExpectedVersion,
		Body:            request.Body.Body,
	})
	switch {
	case errors.Is(err, social.ErrCommentInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment", "无法编辑评论", "评论正文需为 1 到 2000 个字符，且版本必须有效。")
		return generated.UpdateMyComment400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后编辑评论。")
		return generated.UpdateMyComment401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.UpdateMyComment403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "comment_update_denied", "无法编辑评论", "当前账号暂时不能编辑评论。")
		return generated.UpdateMyComment403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "comment_not_found", "评论不可用", "该评论不存在、已删除或不属于当前账号。")
		return generated.UpdateMyComment404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentTargetNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "comment_target_not_found", "评论对象不可用", "该评论所属内容已经停止公开，不能再编辑评论。")
		return generated.UpdateMyComment404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "comment_version_conflict", "评论已发生变化", "请刷新评论区后再编辑。")
		return generated.UpdateMyComment409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.UpdateMyComment200JSONResponse(commentDTO(comment)), nil
}

func (h *Handler) DeleteMyComment(ctx context.Context, request generated.DeleteMyCommentRequestObject) (generated.DeleteMyCommentResponseObject, error) {
	err := h.comments.DeleteMyComment(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), social.DeleteCommentInput{
		CommentID:       request.CommentId,
		ExpectedVersion: request.Params.ExpectedVersion,
	})
	switch {
	case errors.Is(err, social.ErrCommentInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_comment_delete", "无法删除评论", "评论标识或版本无效，请刷新页面后重试。")
		return generated.DeleteMyComment400ApplicationProblemPlusJSONResponse{
			ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem),
		}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后删除评论。")
		return generated.DeleteMyComment401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.DeleteMyComment403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "comment_delete_denied", "无法删除评论", "当前账号暂时不能删除评论。")
		return generated.DeleteMyComment403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "comment_not_found", "评论不可用", "该评论不存在或不属于当前账号。")
		return generated.DeleteMyComment404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrCommentVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "comment_version_conflict", "评论已发生变化", "请刷新评论区后再删除。")
		return generated.DeleteMyComment409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.DeleteMyComment204Response{}, nil
}

func torrentCommentPageDTO(page social.CommentPage) generated.TorrentCommentPage {
	items := make([]generated.Comment, 0, len(page.Items))
	for _, comment := range page.Items {
		items = append(items, commentDTO(comment))
	}
	return generated.TorrentCommentPage{
		TorrentId: page.Target.TorrentID,
		Items:     items,
		Total:     page.Total,
		Limit:     page.Limit,
		Offset:    page.Offset,
	}
}

func announcementCommentPageDTO(page social.CommentPage) generated.AnnouncementCommentPage {
	items := make([]generated.Comment, 0, len(page.Items))
	for _, comment := range page.Items {
		items = append(items, commentDTO(comment))
	}
	return generated.AnnouncementCommentPage{
		AnnouncementId: page.Target.AnnouncementID,
		Items:          items, Total: page.Total, Limit: page.Limit, Offset: page.Offset,
	}
}

func commentDTO(comment social.Comment) generated.Comment {
	author := commentAuthorDTO(comment.Author)
	var replyTo *generated.CommentAuthor
	if comment.ReplyTo != nil {
		value := commentAuthorDTO(*comment.ReplyTo)
		replyTo = &value
	}
	return generated.Comment{
		Id:              comment.ID,
		ParentCommentId: comment.ParentCommentID,
		RootCommentId:   comment.RootCommentID,
		ReplyTo:         replyTo,
		Author:          author,
		Body:            comment.Body,
		BodyFormat:      generated.CommentBodyFormat(comment.BodyFormat),
		State:           generated.CommentState(comment.State),
		Version:         comment.Version,
		CreatedAt:       comment.CreatedAt,
		UpdatedAt:       comment.UpdatedAt,
		EditedAt:        comment.EditedAt,
	}
}

func commentAuthorDTO(author social.CommentAuthor) generated.CommentAuthor {
	medals := make([]generated.CommentAuthorMedal, 0, len(author.Medals))
	for _, medal := range author.Medals {
		medals = append(medals, generated.CommentAuthorMedal{
			Id: medal.ID, Name: medal.Name, ImagePath: medal.ImagePath,
		})
	}
	username := author.Username
	if username == "" {
		// Test fixtures and pre-enrichment internal projections may only carry a
		// display name. Production repository reads always provide the canonical
		// username, while this fallback keeps the public contract valid.
		username = author.ID.String()
	}
	return generated.CommentAuthor{
		Id:            author.ID,
		Username:      username,
		DisplayName:   author.DisplayName,
		Online:        author.Online,
		Vip:           author.VIP,
		Administrator: author.SiteAdministrator,
		Medals:        medals,
	}
}
