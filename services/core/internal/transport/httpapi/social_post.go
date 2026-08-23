package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/social"
)

func (h *Handler) ListSocialPosts(ctx context.Context, request generated.ListSocialPostsRequestObject) (generated.ListSocialPostsResponseObject, error) {
	sort := social.PostNewest
	if request.Params.Sort != nil {
		sort = social.PostSort(*request.Params.Sort)
	}
	limit := social.DefaultPostLimit
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	offset := 0
	if request.Params.Offset != nil {
		offset = *request.Params.Offset
	}
	authorUsername := ""
	if request.Params.AuthorUsername != nil {
		authorUsername = *request.Params.AuthorUsername
	}
	page, err := h.socialPosts.List(ctx, sessionTokenFromContext(ctx), social.PostListQuery{
		Sort: sort, Limit: limit, Offset: offset, AuthorUsername: authorUsername,
		Feed:    social.FeedKind(valueOrDefault(request.Params.Feed, generated.Discover)),
		BoardID: optionalString(request.Params.BoardId), FeaturedOnly: request.Params.FeaturedOnly != nil && *request.Params.FeaturedOnly,
		Topic: optionalString(request.Params.Topic),
	})
	switch {
	case errors.Is(err, social.ErrPostInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_social_feed_query", "动态查询无效", "排序、每页数量或偏移量无效。")
		return generated.ListSocialPosts400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请登录后查看动态圈。")
		return generated.ListSocialPosts401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "social_feed_read_denied", "无法查看动态圈", "当前账号暂时不能使用动态圈。")
		return generated.ListSocialPosts403ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.ListSocialPosts200JSONResponse(socialPostPageDTO(page)), nil
}

func (h *Handler) GetSocialPost(ctx context.Context, request generated.GetSocialPostRequestObject) (generated.GetSocialPostResponseObject, error) {
	post, err := h.socialPosts.FindVisible(ctx, sessionTokenFromContext(ctx), request.PostId)
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请登录后查看动态。")
		return generated.GetSocialPost401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "social_post_read_denied", "无法查看动态", "当前账号暂时不能使用动态圈。")
		return generated.GetSocialPost403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "social_post_not_found", "动态不可用", "该动态不存在或已经被删除。")
		return generated.GetSocialPost404ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.GetSocialPost200JSONResponse(socialPostDTO(post)), nil
}

func (h *Handler) CreateSocialPost(ctx context.Context, request generated.CreateSocialPostRequestObject) (generated.CreateSocialPostResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_social_post", "无法发布动态", "请填写动态正文后重试。")
		return generated.CreateSocialPost400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	post, err := h.socialPosts.Create(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), social.CreatePostInput{
		RequestID: request.Params.IdempotencyKey, Body: request.Body.Content, BoardID: request.Body.BoardId,
		MediaIDs: optionalUUIDs(request.Body.MediaIds), Poll: createPollInput(request.Body.Poll), RedPacket: createRedPacketInput(request.Body.RedPacket),
	})
	switch {
	case errors.Is(err, social.ErrPostInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_social_post", "无法发布动态", "动态正文需为 1 到 2000 个字符。")
		return generated.CreateSocialPost400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, social.ErrSocialMediaInvalid), errors.Is(err, social.ErrSocialMediaNotFound):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_social_media", "无法发布动态", "图片不存在、已被使用或不属于当前账号，请重新上传。")
		return generated.CreateSocialPost400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后发布动态。")
		return generated.CreateSocialPost401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "csrf_invalid", "请求验证失败", "请刷新页面后重试。")
		return generated.CreateSocialPost403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "social_post_create_denied", "无法发布动态", "当前账号暂时不能发布动态。")
		return generated.CreateSocialPost403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostIdempotencyConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "idempotency_conflict", "请求标识已被使用", "请保留正文并重新发起发布。")
		return generated.CreateSocialPost409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrSocialBoardUnavailable):
		problem := newProblemFromContext(ctx, http.StatusConflict, "social_board_unavailable", "板块不可发布", "该板块已停用或不接受成员发布，请选择其他板块。")
		return generated.CreateSocialPost409ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrSocialInsufficientMagic):
		problem := newProblemFromContext(ctx, http.StatusConflict, "social_red_packet_balance_insufficient", "魔力值不足", "当前余额不足以发放这个红包，请调整金额后重试。")
		return generated.CreateSocialPost409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.CreateSocialPost201JSONResponse(socialPostDTO(post)), nil
}

func (h *Handler) UpdateMySocialPost(ctx context.Context, request generated.UpdateMySocialPostRequestObject) (generated.UpdateMySocialPostResponseObject, error) {
	if request.Body == nil {
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_social_post", "无法编辑动态", "请填写动态正文后重试。")
		return generated.UpdateMySocialPost400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	}
	post, err := h.socialPosts.UpdateMyPost(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), social.UpdatePostInput{
		PostID: request.PostId, ExpectedVersion: request.Body.ExpectedVersion, Body: request.Body.Content,
	})
	switch {
	case errors.Is(err, social.ErrPostInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_social_post", "无法编辑动态", "正文或版本无效。")
		return generated.UpdateMySocialPost400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后编辑动态。")
		return generated.UpdateMySocialPost401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF), errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "social_post_update_denied", "无法编辑动态", "请求验证失败或当前账号没有该权限。")
		return generated.UpdateMySocialPost403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "social_post_not_found", "动态不可用", "该动态不存在、已删除或不属于当前账号。")
		return generated.UpdateMySocialPost404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "social_post_version_conflict", "动态已发生变化", "请刷新后再编辑。")
		return generated.UpdateMySocialPost409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.UpdateMySocialPost200JSONResponse(socialPostDTO(post)), nil
}

func (h *Handler) DeleteMySocialPost(ctx context.Context, request generated.DeleteMySocialPostRequestObject) (generated.DeleteMySocialPostResponseObject, error) {
	err := h.socialPosts.DeleteMyPost(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), social.DeletePostInput{
		PostID: request.PostId, ExpectedVersion: request.Params.ExpectedVersion,
	})
	switch {
	case errors.Is(err, social.ErrPostInput):
		problem := newProblemFromContext(ctx, http.StatusBadRequest, "invalid_social_post_delete", "无法删除动态", "动态标识或版本无效。")
		return generated.DeleteMySocialPost400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(problem)}, nil
	case errors.Is(err, identity.ErrSessionNotFound):
		problem := newProblemFromContext(ctx, http.StatusUnauthorized, "session_required", "需要登录", "请重新登录后删除动态。")
		return generated.DeleteMySocialPost401ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, identity.ErrInvalidCSRF), errors.Is(err, authz.ErrForbidden):
		problem := newProblemFromContext(ctx, http.StatusForbidden, "social_post_delete_denied", "无法删除动态", "请求验证失败或当前账号没有该权限。")
		return generated.DeleteMySocialPost403ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostNotFound):
		problem := newProblemFromContext(ctx, http.StatusNotFound, "social_post_not_found", "动态不可用", "该动态不存在或不属于当前账号。")
		return generated.DeleteMySocialPost404ApplicationProblemPlusJSONResponse(problem), nil
	case errors.Is(err, social.ErrPostVersionConflict):
		problem := newProblemFromContext(ctx, http.StatusConflict, "social_post_version_conflict", "动态已发生变化", "请刷新后再删除。")
		return generated.DeleteMySocialPost409ApplicationProblemPlusJSONResponse(problem), nil
	case err != nil:
		return nil, err
	}
	return generated.DeleteMySocialPost204Response{}, nil
}

func socialPostPageDTO(page social.PostPage) generated.SocialPostPage {
	items := make([]generated.SocialPost, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, socialPostDTO(item))
	}
	return generated.SocialPostPage{Items: items, Total: page.Total, Limit: page.Limit, Offset: page.Offset, Sort: generated.SocialPostSort(page.Sort), Feed: generated.SocialFeedKind(page.Feed)}
}

func socialPostDTO(post social.Post) generated.SocialPost {
	return generated.SocialPost{
		Id: post.ID, Author: generated.SocialPostAuthor{
			Id: post.Author.ID, Username: post.Author.Username, DisplayName: post.Author.DisplayName,
			FollowedByMe: post.Author.FollowedByMe, Online: post.Author.Online, Vip: post.Author.VIP,
			Administrator: post.Author.SiteAdministrator, Medals: socialAuthorMedalDTOs(post.Author.Medals),
		},
		Board: socialBoardDTO(post.Board), Content: post.Body, Version: post.Version, CommentCount: post.CommentCount,
		LikeCount: post.LikeCount, RepostCount: post.RepostCount, LikedByMe: post.LikedByMe, RepostedByMe: post.RepostedByMe,
		Pinned: post.Pinned, Featured: post.Featured, Hidden: boolPointer(post.State == social.PostModeratorHidden), Topics: post.Topics, Media: socialMediaDTOs(post.Media), Poll: socialPollDTO(post.Poll), RedPacket: socialRedPacketDTO(post.RedPacket),
		CreatedAt: post.CreatedAt, UpdatedAt: post.UpdatedAt, EditedAt: post.EditedAt,
	}
}

func socialAuthorMedalDTOs(medals []social.AuthorMedal) []generated.SocialAuthorMedal {
	items := make([]generated.SocialAuthorMedal, 0, len(medals))
	for _, medal := range medals {
		items = append(items, generated.SocialAuthorMedal{Id: medal.ID, Name: medal.Name, ImagePath: medal.ImagePath})
	}
	return items
}

func valueOrDefault[T comparable](value *T, fallback T) T {
	if value == nil {
		return fallback
	}
	return *value
}
func boolPointer(value bool) *bool { return &value }
func optionalUUIDs(value *[]uuid.UUID) []uuid.UUID {
	if value == nil {
		return nil
	}
	return append([]uuid.UUID(nil), (*value)...)
}
func createPollInput(value *generated.CreateSocialPollRequest) *social.CreatePollInput {
	if value == nil {
		return nil
	}
	return &social.CreatePollInput{Question: value.Question, Options: append([]string(nil), value.Options...), ClosesAt: value.ClosesAt}
}
func createRedPacketInput(value *generated.CreateSocialRedPacketRequest) *social.CreateRedPacketInput {
	if value == nil {
		return nil
	}
	return &social.CreateRedPacketInput{TotalAmount: value.TotalAmount, ClaimCount: value.ClaimCount}
}
func socialBoardDTO(board social.Board) generated.SocialBoard {
	return generated.SocialBoard{Id: board.ID, Name: board.Name, Description: board.Description, Icon: generated.SocialBoardIcon(board.Icon), Tone: generated.SocialBoardTone(board.Tone), DisplayOrder: board.DisplayOrder, Enabled: board.Enabled, AllowMemberPosts: board.AllowMemberPosts, PostCount: board.PostCount, Version: board.Version}
}
func socialMediaDTOs(media []social.PostMedia) []generated.SocialPostMedia {
	result := make([]generated.SocialPostMedia, 0, len(media))
	for _, item := range media {
		result = append(result, generated.SocialPostMedia{Id: item.ID, ContentType: generated.SocialPostMediaContentType(item.ContentType), Width: item.Width, Height: item.Height, Url: item.URL})
	}
	return result
}
func socialPollDTO(poll *social.Poll) *generated.SocialPoll {
	if poll == nil {
		return nil
	}
	options := make([]generated.SocialPollOption, 0, len(poll.Options))
	for _, option := range poll.Options {
		options = append(options, generated.SocialPollOption{Id: option.ID, Label: option.Label, VoteCount: option.VoteCount})
	}
	return &generated.SocialPoll{Question: poll.Question, Options: options, TotalVotes: poll.TotalVotes, SelectedOptionId: poll.SelectedOptionID, ClosesAt: poll.ClosesAt, Closed: poll.Closed}
}
func socialRedPacketDTO(packet *social.RedPacket) *generated.SocialRedPacket {
	if packet == nil {
		return nil
	}
	return &generated.SocialRedPacket{TotalAmount: packet.TotalAmount, ClaimCount: packet.ClaimCount, RemainingAmount: packet.RemainingAmount, RemainingClaims: packet.RemainingClaims, ClaimedByMe: packet.ClaimedByMe, MyClaimAmount: packet.MyClaimAmount}
}
