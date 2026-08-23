package httpapi

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/oapi-codegen/runtime"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/social"
)

func (h *Handler) GetSocialCommunityOverview(ctx context.Context, _ generated.GetSocialCommunityOverviewRequestObject) (generated.GetSocialCommunityOverviewResponseObject, error) {
	result, err := h.socialPosts.Overview(ctx, sessionTokenFromContext(ctx))
	if errors.Is(err, identity.ErrSessionNotFound) {
		p := newProblemFromContext(ctx, 401, "session_required", "需要登录", "请登录后查看动态圈。")
		return generated.GetSocialCommunityOverview401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		p := newProblemFromContext(ctx, 403, "social_feed_read_denied", "无法查看动态圈", "当前账号暂时不能使用动态圈。")
		return generated.GetSocialCommunityOverview403ApplicationProblemPlusJSONResponse(p), nil
	}
	if err != nil {
		return nil, err
	}
	boards := make([]generated.SocialBoard, 0, len(result.Boards))
	for _, board := range result.Boards {
		boards = append(boards, socialBoardDTO(board))
	}
	topics := make([]generated.SocialTopic, 0, len(result.HotTopics))
	for _, topic := range result.HotTopics {
		topics = append(topics, generated.SocialTopic{Name: topic.Name, PostCount: topic.PostCount})
	}
	return generated.GetSocialCommunityOverview200JSONResponse{Boards: boards, HotTopics: topics}, nil
}

func (h *Handler) UploadSocialMedia(ctx context.Context, request generated.UploadSocialMediaRequestObject) (generated.UploadSocialMediaResponseObject, error) {
	if request.Body == nil {
		return uploadSocialMediaBad(ctx), nil
	}
	var body generated.SocialMediaUploadRequest
	if err := runtime.BindMultipart(&body, *request.Body); err != nil {
		return uploadSocialMediaBad(ctx), nil
	}
	raw, err := body.Image.Bytes()
	if err != nil {
		return uploadSocialMediaBad(ctx), nil
	}
	result, err := h.socialPosts.UploadMedia(ctx, sessionTokenFromContext(ctx), string(request.Params.XCSRFToken), raw)
	switch {
	case errors.Is(err, social.ErrSocialMediaInvalid):
		return uploadSocialMediaBad(ctx), nil
	case errors.Is(err, identity.ErrSessionNotFound):
		p := newProblemFromContext(ctx, 401, "session_required", "需要登录", "请重新登录后上传图片。")
		return generated.UploadSocialMedia401ApplicationProblemPlusJSONResponse(p), nil
	case errors.Is(err, identity.ErrInvalidCSRF), errors.Is(err, authz.ErrForbidden):
		p := newProblemFromContext(ctx, 403, "social_media_denied", "无法上传图片", "请求验证失败或当前账号没有该权限。")
		return generated.UploadSocialMedia403ApplicationProblemPlusJSONResponse(p), nil
	case err != nil:
		return nil, err
	}
	return generated.UploadSocialMedia201JSONResponse(socialMediaDTOs([]social.PostMedia{result})[0]), nil
}
func uploadSocialMediaBad(ctx context.Context) generated.UploadSocialMedia400ApplicationProblemPlusJSONResponse {
	p := newProblemFromContext(ctx, 400, "invalid_social_media", "图片无效", "请上传不超过 5 MiB 的 JPEG、PNG 或 WebP 图片。")
	return generated.UploadSocialMedia400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}
}

func (h *Handler) GetSocialMedia(ctx context.Context, request generated.GetSocialMediaRequestObject) (generated.GetSocialMediaResponseObject, error) {
	result, err := h.socialPosts.ReadMedia(ctx, sessionTokenFromContext(ctx), request.MediaId)
	if errors.Is(err, identity.ErrSessionNotFound) {
		p := newProblemFromContext(ctx, 401, "session_required", "需要登录", "请登录后查看图片。")
		return generated.GetSocialMedia401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
	}
	if errors.Is(err, authz.ErrForbidden) {
		p := newProblemFromContext(ctx, 403, "social_media_denied", "无法查看图片", "当前账号没有该权限。")
		return generated.GetSocialMedia403ApplicationProblemPlusJSONResponse(p), nil
	}
	if errors.Is(err, social.ErrSocialMediaNotFound) {
		p := newProblemFromContext(ctx, 404, "social_media_not_found", "图片不可用", "图片不存在或所属动态不可见。")
		return generated.GetSocialMedia404ApplicationProblemPlusJSONResponse(p), nil
	}
	if err != nil {
		return nil, err
	}
	cache := "private, max-age=86400, immutable"
	etag := "\"" + hex.EncodeToString(result.SHA256[:]) + "\""
	headers := generated.GetSocialMedia200ResponseHeaders{CacheControl: &cache, ETag: &etag}
	reader := bytes.NewReader(result.Payload)
	switch result.ContentType {
	case "image/jpeg":
		return generated.GetSocialMedia200ImagejpegResponse{Body: reader, Headers: headers, ContentLength: int64(len(result.Payload))}, nil
	case "image/png":
		return generated.GetSocialMedia200ImagepngResponse{Body: reader, Headers: headers, ContentLength: int64(len(result.Payload))}, nil
	default:
		return generated.GetSocialMedia200ImagewebpResponse{Body: reader, Headers: headers, ContentLength: int64(len(result.Payload))}, nil
	}
}

func interactionProblem(ctx context.Context, err error) (generated.Problem, int) {
	switch {
	case errors.Is(err, identity.ErrSessionNotFound):
		return newProblemFromContext(ctx, 401, "session_required", "需要登录", "请重新登录后操作。"), 401
	case errors.Is(err, identity.ErrInvalidCSRF), errors.Is(err, authz.ErrForbidden):
		return newProblemFromContext(ctx, 403, "social_interaction_denied", "无法完成互动", "请求验证失败或当前账号没有该权限。"), 403
	case errors.Is(err, social.ErrPostNotFound), errors.Is(err, social.ErrSocialMemberNotFound):
		return newProblemFromContext(ctx, 404, "social_target_not_found", "目标不可用", "目标不存在或已经不可见。"), 404
	default:
		return generated.Problem{}, 0
	}
}

func (h *Handler) LikeSocialPost(ctx context.Context, r generated.LikeSocialPostRequestObject) (generated.LikeSocialPostResponseObject, error) {
	v, e := h.socialPosts.SetLike(ctx, sessionTokenFromContext(ctx), string(r.Params.XCSRFToken), r.PostId, true)
	if p, s := interactionProblem(ctx, e); s > 0 {
		if s == 401 {
			return generated.LikeSocialPost401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
		}
		if s == 403 {
			return generated.LikeSocialPost403ApplicationProblemPlusJSONResponse(p), nil
		}
		return generated.LikeSocialPost404ApplicationProblemPlusJSONResponse(p), nil
	}
	if e != nil {
		return nil, e
	}
	return generated.LikeSocialPost200JSONResponse{Active: v.Active, Count: v.Count}, nil
}
func (h *Handler) UnlikeSocialPost(ctx context.Context, r generated.UnlikeSocialPostRequestObject) (generated.UnlikeSocialPostResponseObject, error) {
	v, e := h.socialPosts.SetLike(ctx, sessionTokenFromContext(ctx), string(r.Params.XCSRFToken), r.PostId, false)
	if p, s := interactionProblem(ctx, e); s > 0 {
		if s == 401 {
			return generated.UnlikeSocialPost401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
		}
		if s == 403 {
			return generated.UnlikeSocialPost403ApplicationProblemPlusJSONResponse(p), nil
		}
		return generated.UnlikeSocialPost404ApplicationProblemPlusJSONResponse(p), nil
	}
	if e != nil {
		return nil, e
	}
	return generated.UnlikeSocialPost200JSONResponse{Active: v.Active, Count: v.Count}, nil
}
func (h *Handler) RepostSocialPost(ctx context.Context, r generated.RepostSocialPostRequestObject) (generated.RepostSocialPostResponseObject, error) {
	v, e := h.socialPosts.SetRepost(ctx, sessionTokenFromContext(ctx), string(r.Params.XCSRFToken), r.PostId, true)
	if p, s := interactionProblem(ctx, e); s > 0 {
		if s == 401 {
			return generated.RepostSocialPost401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
		}
		if s == 403 {
			return generated.RepostSocialPost403ApplicationProblemPlusJSONResponse(p), nil
		}
		return generated.RepostSocialPost404ApplicationProblemPlusJSONResponse(p), nil
	}
	if e != nil {
		return nil, e
	}
	return generated.RepostSocialPost200JSONResponse{Active: v.Active, Count: v.Count}, nil
}
func (h *Handler) UnrepostSocialPost(ctx context.Context, r generated.UnrepostSocialPostRequestObject) (generated.UnrepostSocialPostResponseObject, error) {
	v, e := h.socialPosts.SetRepost(ctx, sessionTokenFromContext(ctx), string(r.Params.XCSRFToken), r.PostId, false)
	if p, s := interactionProblem(ctx, e); s > 0 {
		if s == 401 {
			return generated.UnrepostSocialPost401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
		}
		if s == 403 {
			return generated.UnrepostSocialPost403ApplicationProblemPlusJSONResponse(p), nil
		}
		return generated.UnrepostSocialPost404ApplicationProblemPlusJSONResponse(p), nil
	}
	if e != nil {
		return nil, e
	}
	return generated.UnrepostSocialPost200JSONResponse{Active: v.Active, Count: v.Count}, nil
}

func (h *Handler) FollowSocialMember(ctx context.Context, r generated.FollowSocialMemberRequestObject) (generated.FollowSocialMemberResponseObject, error) {
	v, e := h.socialPosts.SetFollow(ctx, sessionTokenFromContext(ctx), string(r.Params.XCSRFToken), r.Username, true)
	if errors.Is(e, social.ErrPostInput) || errors.Is(e, social.ErrSocialSelfFollow) {
		p := newProblemFromContext(ctx, 400, "invalid_social_follow", "无法关注成员", "不能关注自己或成员名无效。")
		return generated.FollowSocialMember400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
	}
	if p, s := interactionProblem(ctx, e); s > 0 {
		if s == 401 {
			return generated.FollowSocialMember401ApplicationProblemPlusJSONResponse(p), nil
		}
		if s == 403 {
			return generated.FollowSocialMember403ApplicationProblemPlusJSONResponse(p), nil
		}
		return generated.FollowSocialMember404ApplicationProblemPlusJSONResponse(p), nil
	}
	if e != nil {
		return nil, e
	}
	return generated.FollowSocialMember200JSONResponse{Username: v.Username, Following: v.Following}, nil
}
func (h *Handler) UnfollowSocialMember(ctx context.Context, r generated.UnfollowSocialMemberRequestObject) (generated.UnfollowSocialMemberResponseObject, error) {
	v, e := h.socialPosts.SetFollow(ctx, sessionTokenFromContext(ctx), string(r.Params.XCSRFToken), r.Username, false)
	if errors.Is(e, social.ErrPostInput) || errors.Is(e, social.ErrSocialSelfFollow) {
		p := newProblemFromContext(ctx, 400, "invalid_social_follow", "无法取消关注", "成员名无效。")
		return generated.UnfollowSocialMember400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
	}
	if p, s := interactionProblem(ctx, e); s > 0 {
		if s == 401 {
			return generated.UnfollowSocialMember401ApplicationProblemPlusJSONResponse(p), nil
		}
		if s == 403 {
			return generated.UnfollowSocialMember403ApplicationProblemPlusJSONResponse(p), nil
		}
		return generated.UnfollowSocialMember404ApplicationProblemPlusJSONResponse(p), nil
	}
	if e != nil {
		return nil, e
	}
	return generated.UnfollowSocialMember200JSONResponse{Username: v.Username, Following: v.Following}, nil
}

func (h *Handler) VoteSocialPoll(ctx context.Context, r generated.VoteSocialPollRequestObject) (generated.VoteSocialPollResponseObject, error) {
	if r.Body == nil {
		p := newProblemFromContext(ctx, 400, "invalid_social_poll_vote", "投票无效", "请选择一个选项。")
		return generated.VoteSocialPoll400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
	}
	v, e := h.socialPosts.Vote(ctx, sessionTokenFromContext(ctx), string(r.Params.XCSRFToken), r.PostId, r.Body.OptionId)
	if errors.Is(e, social.ErrSocialPollClosed) {
		p := newProblemFromContext(ctx, 409, "social_poll_closed", "投票已结束", "该投票已经截止。")
		return generated.VoteSocialPoll409ApplicationProblemPlusJSONResponse(p), nil
	}
	if p, s := interactionProblem(ctx, e); s > 0 {
		if s == 401 {
			return generated.VoteSocialPoll401ApplicationProblemPlusJSONResponse(p), nil
		}
		if s == 403 {
			return generated.VoteSocialPoll403ApplicationProblemPlusJSONResponse(p), nil
		}
		return generated.VoteSocialPoll404ApplicationProblemPlusJSONResponse(p), nil
	}
	if e != nil {
		return nil, e
	}
	return generated.VoteSocialPoll200JSONResponse(*socialPollDTO(&v)), nil
}

func (h *Handler) ClaimSocialRedPacket(ctx context.Context, r generated.ClaimSocialRedPacketRequestObject) (generated.ClaimSocialRedPacketResponseObject, error) {
	v, e := h.socialPosts.ClaimRedPacket(ctx, sessionTokenFromContext(ctx), string(r.Params.XCSRFToken), r.PostId, r.Params.IdempotencyKey)
	if errors.Is(e, social.ErrSocialRedPacketEmpty) || errors.Is(e, social.ErrSocialRedPacketSelfClaim) {
		p := newProblemFromContext(ctx, 409, "social_red_packet_unavailable", "红包不可领取", "红包已领完或不能领取自己发布的红包。")
		return generated.ClaimSocialRedPacket409ApplicationProblemPlusJSONResponse(p), nil
	}
	if p, s := interactionProblem(ctx, e); s > 0 {
		if s == 401 {
			return generated.ClaimSocialRedPacket401ApplicationProblemPlusJSONResponse(p), nil
		}
		if s == 403 {
			return generated.ClaimSocialRedPacket403ApplicationProblemPlusJSONResponse(p), nil
		}
		return generated.ClaimSocialRedPacket404ApplicationProblemPlusJSONResponse(p), nil
	}
	if e != nil {
		return nil, e
	}
	return generated.ClaimSocialRedPacket200JSONResponse{Amount: v.Amount, RemainingAmount: v.RemainingAmount, RemainingClaims: v.RemainingClaims, Replayed: v.Replayed}, nil
}

func managedBoardDTO(board social.Board) generated.ManagedSocialBoard {
	return generated.ManagedSocialBoard{Id: board.ID, Name: board.Name, Description: board.Description, Icon: generated.SocialBoardIcon(board.Icon), Tone: generated.SocialBoardTone(board.Tone), DisplayOrder: board.DisplayOrder, Enabled: board.Enabled, AllowMemberPosts: board.AllowMemberPosts, PostCount: board.PostCount, Version: board.Version, CreatedAt: board.CreatedAt, UpdatedAt: board.UpdatedAt}
}
func (h *Handler) ListManagedSocialBoards(ctx context.Context, _ generated.ListManagedSocialBoardsRequestObject) (generated.ListManagedSocialBoardsResponseObject, error) {
	session, p, e := h.authenticateStaffRead(ctx)
	if e != nil {
		return nil, e
	}
	if p != nil {
		if p.Status == 401 {
			return generated.ListManagedSocialBoards401ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(*p)}, nil
		}
		return generated.ListManagedSocialBoards403ApplicationProblemPlusJSONResponse(*p), nil
	}
	items, e := h.socialPosts.ListManagedBoards(ctx, staffActor(session))
	if errors.Is(e, authz.ErrForbidden) {
		p := newProblemFromContext(ctx, 403, "social_board_read_denied", "无法查看板块管理", "当前后台身份没有对应权限。")
		return generated.ListManagedSocialBoards403ApplicationProblemPlusJSONResponse(p), nil
	}
	if e != nil {
		return nil, e
	}
	result := make([]generated.ManagedSocialBoard, 0, len(items))
	for _, item := range items {
		result = append(result, managedBoardDTO(item))
	}
	return generated.ListManagedSocialBoards200JSONResponse(result), nil
}
func (h *Handler) CreateManagedSocialBoard(ctx context.Context, r generated.CreateManagedSocialBoardRequestObject) (generated.CreateManagedSocialBoardResponseObject, error) {
	if r.Body == nil {
		p := newProblemFromContext(ctx, 400, "invalid_social_board", "板块无效", "请检查板块资料和变更理由。")
		return generated.CreateManagedSocialBoard400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
	}
	session, p, e := h.authenticateStaffWrite(ctx, r.Params.XCSRFToken)
	if e != nil {
		return nil, e
	}
	if p != nil {
		if p.Status == 401 {
			return generated.CreateManagedSocialBoard401ApplicationProblemPlusJSONResponse(*p), nil
		}
		return generated.CreateManagedSocialBoard403ApplicationProblemPlusJSONResponse(*p), nil
	}
	v, e := h.socialPosts.CreateManagedBoard(ctx, staffActor(session), social.CreateBoardInput{ID: r.Body.Id, Name: r.Body.Name, Description: r.Body.Description, Icon: string(r.Body.Icon), Tone: string(r.Body.Tone), DisplayOrder: r.Body.DisplayOrder, Enabled: r.Body.Enabled, AllowMemberPosts: r.Body.AllowMemberPosts, Reason: r.Body.Reason})
	if errors.Is(e, social.ErrPostInput) {
		p := newProblemFromContext(ctx, 400, "invalid_social_board", "板块无效", "请检查板块资料和变更理由。")
		return generated.CreateManagedSocialBoard400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
	}
	if errors.Is(e, authz.ErrForbidden) {
		p := newProblemFromContext(ctx, 403, "social_board_create_denied", "无法创建板块", "当前后台身份没有对应权限。")
		return generated.CreateManagedSocialBoard403ApplicationProblemPlusJSONResponse(p), nil
	}
	if errors.Is(e, social.ErrSocialBoardExists) {
		p := newProblemFromContext(ctx, 409, "social_board_exists", "板块已存在", "稳定标识已经被使用。")
		return generated.CreateManagedSocialBoard409ApplicationProblemPlusJSONResponse(p), nil
	}
	if e != nil {
		return nil, e
	}
	return generated.CreateManagedSocialBoard201JSONResponse(managedBoardDTO(v)), nil
}
func (h *Handler) UpdateManagedSocialBoard(ctx context.Context, r generated.UpdateManagedSocialBoardRequestObject) (generated.UpdateManagedSocialBoardResponseObject, error) {
	if r.Body == nil {
		p := newProblemFromContext(ctx, 400, "invalid_social_board", "板块无效", "请检查板块资料和变更理由。")
		return generated.UpdateManagedSocialBoard400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
	}
	session, p, e := h.authenticateStaffWrite(ctx, r.Params.XCSRFToken)
	if e != nil {
		return nil, e
	}
	if p != nil {
		if p.Status == 401 {
			return generated.UpdateManagedSocialBoard401ApplicationProblemPlusJSONResponse(*p), nil
		}
		return generated.UpdateManagedSocialBoard403ApplicationProblemPlusJSONResponse(*p), nil
	}
	v, e := h.socialPosts.UpdateManagedBoard(ctx, staffActor(session), social.UpdateBoardInput{ID: r.BoardId, Name: r.Body.Name, Description: r.Body.Description, Icon: string(r.Body.Icon), Tone: string(r.Body.Tone), DisplayOrder: r.Body.DisplayOrder, Enabled: r.Body.Enabled, AllowMemberPosts: r.Body.AllowMemberPosts, ExpectedVersion: r.Body.ExpectedVersion, Reason: r.Body.Reason})
	if errors.Is(e, social.ErrPostInput) {
		p := newProblemFromContext(ctx, 400, "invalid_social_board", "板块无效", "请检查板块资料和变更理由。")
		return generated.UpdateManagedSocialBoard400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
	}
	if errors.Is(e, authz.ErrForbidden) {
		p := newProblemFromContext(ctx, 403, "social_board_update_denied", "无法更新板块", "当前后台身份没有对应权限。")
		return generated.UpdateManagedSocialBoard403ApplicationProblemPlusJSONResponse(p), nil
	}
	if errors.Is(e, social.ErrSocialBoardNotFound) {
		p := newProblemFromContext(ctx, 404, "social_board_not_found", "板块不存在", "请刷新列表后重试。")
		return generated.UpdateManagedSocialBoard404ApplicationProblemPlusJSONResponse(p), nil
	}
	if errors.Is(e, social.ErrSocialCommunityConflict) {
		p := newProblemFromContext(ctx, 409, "social_board_conflict", "板块已变化", "请刷新列表后重试。")
		return generated.UpdateManagedSocialBoard409ApplicationProblemPlusJSONResponse(p), nil
	}
	if e != nil {
		return nil, e
	}
	return generated.UpdateManagedSocialBoard200JSONResponse(managedBoardDTO(v)), nil
}

func (h *Handler) ListManagedSocialPosts(ctx context.Context, r generated.ListManagedSocialPostsRequestObject) (generated.ListManagedSocialPostsResponseObject, error) {
	session, p, e := h.authenticateStaffRead(ctx)
	if e != nil {
		return nil, e
	}
	if p != nil {
		if p.Status == 401 {
			return generated.ListManagedSocialPosts401ApplicationProblemPlusJSONResponse(*p), nil
		}
		return generated.ListManagedSocialPosts403ApplicationProblemPlusJSONResponse(*p), nil
	}
	limit := social.DefaultPostLimit
	if r.Params.Limit != nil {
		limit = *r.Params.Limit
	}
	offset := 0
	if r.Params.Offset != nil {
		offset = *r.Params.Offset
	}
	v, e := h.socialPosts.ListManagedPosts(ctx, staffActor(session), social.PostListQuery{Sort: social.PostNewest, Feed: social.FeedDiscover, Limit: limit, Offset: offset, BoardID: optionalString(r.Params.BoardId)})
	if errors.Is(e, social.ErrPostInput) {
		p := newProblemFromContext(ctx, 400, "invalid_social_feed_query", "动态查询无效", "分页参数无效。")
		return generated.ListManagedSocialPosts400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
	}
	if errors.Is(e, authz.ErrForbidden) {
		p := newProblemFromContext(ctx, 403, "social_post_manage_read_denied", "无法查看动态管理", "当前后台身份没有对应权限。")
		return generated.ListManagedSocialPosts403ApplicationProblemPlusJSONResponse(p), nil
	}
	if e != nil {
		return nil, e
	}
	return generated.ListManagedSocialPosts200JSONResponse(socialPostPageDTO(v)), nil
}
func (h *Handler) ModerateSocialPost(ctx context.Context, r generated.ModerateSocialPostRequestObject) (generated.ModerateSocialPostResponseObject, error) {
	if r.Body == nil {
		p := newProblemFromContext(ctx, 400, "invalid_social_moderation", "管理操作无效", "请检查状态、版本和理由。")
		return generated.ModerateSocialPost400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
	}
	session, p, e := h.authenticateStaffWrite(ctx, r.Params.XCSRFToken)
	if e != nil {
		return nil, e
	}
	if p != nil {
		if p.Status == 401 {
			return generated.ModerateSocialPost401ApplicationProblemPlusJSONResponse(*p), nil
		}
		return generated.ModerateSocialPost403ApplicationProblemPlusJSONResponse(*p), nil
	}
	v, e := h.socialPosts.ModeratePost(ctx, staffActor(session), social.ModeratePostInput{PostID: r.PostId, BoardID: r.Body.BoardId, Pinned: r.Body.Pinned, Featured: r.Body.Featured, Hidden: r.Body.Hidden, ExpectedVersion: r.Body.ExpectedVersion, Reason: r.Body.Reason})
	if errors.Is(e, social.ErrPostInput) {
		p := newProblemFromContext(ctx, 400, "invalid_social_moderation", "管理操作无效", "请检查状态、版本和理由。")
		return generated.ModerateSocialPost400ApplicationProblemPlusJSONResponse{ProblemResponseApplicationProblemPlusJSONResponse: generated.ProblemResponseApplicationProblemPlusJSONResponse(p)}, nil
	}
	if errors.Is(e, authz.ErrForbidden) {
		p := newProblemFromContext(ctx, 403, "social_post_moderate_denied", "无法管理动态", "当前后台身份没有对应权限。")
		return generated.ModerateSocialPost403ApplicationProblemPlusJSONResponse(p), nil
	}
	if errors.Is(e, social.ErrPostNotFound) || errors.Is(e, social.ErrSocialBoardNotFound) {
		p := newProblemFromContext(ctx, 404, "social_target_not_found", "目标不存在", "动态或板块已经不存在。")
		return generated.ModerateSocialPost404ApplicationProblemPlusJSONResponse(p), nil
	}
	if errors.Is(e, social.ErrPostVersionConflict) {
		p := newProblemFromContext(ctx, 409, "social_post_version_conflict", "动态已变化", "请刷新后重试。")
		return generated.ModerateSocialPost409ApplicationProblemPlusJSONResponse(p), nil
	}
	if e != nil {
		return nil, e
	}
	return generated.ModerateSocialPost200JSONResponse(socialPostDTO(v)), nil
}

var _ = http.StatusOK
