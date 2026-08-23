package social

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	_ "golang.org/x/image/webp"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	MaxPostMedia      = 9
	MaxPostMediaBytes = 5 << 20
)

var (
	ErrSocialMediaInvalid       = errors.New("social media is invalid")
	ErrSocialMediaNotFound      = errors.New("social media was not found")
	ErrSocialMemberNotFound     = errors.New("social member was not found")
	ErrSocialSelfFollow         = errors.New("member cannot follow self")
	ErrSocialPollClosed         = errors.New("social poll is closed")
	ErrSocialRedPacketEmpty     = errors.New("social red packet is empty")
	ErrSocialRedPacketSelfClaim = errors.New("social red packet creator cannot claim")
	ErrSocialInsufficientMagic  = errors.New("social red packet balance is insufficient")
	ErrSocialBoardExists        = errors.New("social board already exists")
	ErrSocialBoardNotFound      = errors.New("social board was not found")
)

var boardIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
var topicPattern = regexp.MustCompile(`#[\p{L}\p{N}_]{1,40}`)

type Board struct {
	ID               string
	Name             string
	Description      string
	Icon             string
	Tone             string
	DisplayOrder     int
	Enabled          bool
	AllowMemberPosts bool
	PostCount        int64
	Version          int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Topic struct {
	Name      string
	PostCount int64
}

type CommunityOverview struct {
	Boards    []Board
	HotTopics []Topic
}

type PostMedia struct {
	ID          uuid.UUID
	ContentType string
	Width       int
	Height      int
	URL         string
}

type MediaObject struct {
	PostMedia
	Payload []byte
	SHA256  [sha256.Size]byte
}

type CreatePollInput struct {
	Question string
	Options  []string
	ClosesAt *time.Time
}

type PollOption struct {
	ID        uuid.UUID
	Label     string
	VoteCount int64
}

type Poll struct {
	Question         string
	Options          []PollOption
	TotalVotes       int64
	SelectedOptionID *uuid.UUID
	ClosesAt         *time.Time
	Closed           bool
}

type CreateRedPacketInput struct {
	TotalAmount int64
	ClaimCount  int
}

type RedPacket struct {
	TotalAmount     int64
	ClaimCount      int
	RemainingAmount int64
	RemainingClaims int
	ClaimedByMe     bool
	MyClaimAmount   *int64
}

type InteractionState struct {
	Active bool
	Count  int64
}

type FollowState struct {
	Username  string
	Following bool
}

type RedPacketClaim struct {
	Amount          int64
	RemainingAmount int64
	RemainingClaims int
	Replayed        bool
}

type CreateBoardInput struct {
	ID, Name, Description, Icon, Tone, Reason string
	DisplayOrder                              int
	Enabled, AllowMemberPosts                 bool
}

type UpdateBoardInput struct {
	ID, Name, Description, Icon, Tone, Reason string
	DisplayOrder                              int
	Enabled, AllowMemberPosts                 bool
	ExpectedVersion                           int64
}

type ModeratePostInput struct {
	PostID          uuid.UUID
	BoardID         string
	Pinned          bool
	Featured        bool
	Hidden          bool
	ExpectedVersion int64
	Reason          string
}

type CommunityRepository interface {
	Overview(context.Context, uuid.UUID) (CommunityOverview, error)
	EnrichPosts(context.Context, uuid.UUID, []Post, time.Time) ([]Post, error)
	UploadMedia(context.Context, uuid.UUID, uuid.UUID, []byte, string, int, int, [sha256.Size]byte, time.Time) (PostMedia, error)
	ReadMedia(context.Context, uuid.UUID) (MediaObject, error)
	SetInteraction(context.Context, uuid.UUID, uuid.UUID, string, bool, time.Time) (InteractionState, error)
	SetFollow(context.Context, uuid.UUID, string, bool, time.Time) (FollowState, error)
	Vote(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) (Poll, error)
	ClaimRedPacket(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) (RedPacketClaim, error)
	ListBoards(context.Context) ([]Board, error)
	CreateBoard(context.Context, authz.StaffActor, authz.Decision, CreateBoardInput, time.Time) (Board, error)
	UpdateBoard(context.Context, authz.StaffActor, authz.Decision, UpdateBoardInput, time.Time) (Board, error)
	ListManagedPosts(context.Context, uuid.UUID, PostListQuery) ([]Post, int64, error)
	ModeratePost(context.Context, authz.StaffActor, authz.Decision, ModeratePostInput, time.Time) (Post, error)
}

func (service *PostService) Overview(ctx context.Context, cookieToken string) (CommunityOverview, error) {
	viewerID, err := service.authorizeRead(ctx, cookieToken)
	if err != nil {
		return CommunityOverview{}, err
	}
	repository, err := service.communityRepository()
	if err != nil {
		return CommunityOverview{}, err
	}
	return repository.Overview(ctx, viewerID)
}

func (service *PostService) UploadMedia(ctx context.Context, cookieToken, csrfToken string, raw []byte) (PostMedia, error) {
	if len(raw) == 0 || len(raw) > MaxPostMediaBytes {
		return PostMedia{}, ErrSocialMediaInvalid
	}
	userID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionSocialMediaCreateSelf)
	if err != nil {
		return PostMedia{}, err
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > 25_000_000 {
		return PostMedia{}, ErrSocialMediaInvalid
	}
	contentType := map[string]string{"jpeg": "image/jpeg", "png": "image/png", "webp": "image/webp"}[format]
	if contentType == "" {
		return PostMedia{}, ErrSocialMediaInvalid
	}
	repository, err := service.communityRepository()
	if err != nil {
		return PostMedia{}, err
	}
	return repository.UploadMedia(ctx, userID, uuid.New(), raw, contentType, config.Width, config.Height, sha256.Sum256(raw), now)
}

func (service *PostService) ReadMedia(ctx context.Context, cookieToken string, mediaID uuid.UUID) (MediaObject, error) {
	if mediaID == uuid.Nil {
		return MediaObject{}, ErrSocialMediaInvalid
	}
	if _, err := service.authorizeRead(ctx, cookieToken); err != nil {
		return MediaObject{}, err
	}
	repository, err := service.communityRepository()
	if err != nil {
		return MediaObject{}, err
	}
	return repository.ReadMedia(ctx, mediaID)
}

func (service *PostService) SetLike(ctx context.Context, cookieToken, csrfToken string, postID uuid.UUID, active bool) (InteractionState, error) {
	return service.setInteraction(ctx, cookieToken, csrfToken, postID, "like", active, authz.ActionSocialPostLikeSelf)
}

func (service *PostService) SetRepost(ctx context.Context, cookieToken, csrfToken string, postID uuid.UUID, active bool) (InteractionState, error) {
	return service.setInteraction(ctx, cookieToken, csrfToken, postID, "repost", active, authz.ActionSocialPostRepostSelf)
}

func (service *PostService) setInteraction(ctx context.Context, cookieToken, csrfToken string, postID uuid.UUID, kind string, active bool, action authz.Action) (InteractionState, error) {
	if postID == uuid.Nil {
		return InteractionState{}, ErrPostInput
	}
	userID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, action)
	if err != nil {
		return InteractionState{}, err
	}
	repository, err := service.communityRepository()
	if err != nil {
		return InteractionState{}, err
	}
	return repository.SetInteraction(ctx, userID, postID, kind, active, now)
}

func (service *PostService) SetFollow(ctx context.Context, cookieToken, csrfToken, username string, active bool) (FollowState, error) {
	username = strings.TrimSpace(username)
	if username == "" || utf8.RuneCountInString(username) > 64 {
		return FollowState{}, ErrPostInput
	}
	userID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionSocialFollowWriteSelf)
	if err != nil {
		return FollowState{}, err
	}
	repository, err := service.communityRepository()
	if err != nil {
		return FollowState{}, err
	}
	return repository.SetFollow(ctx, userID, username, active, now)
}

func (service *PostService) Vote(ctx context.Context, cookieToken, csrfToken string, postID, optionID uuid.UUID) (Poll, error) {
	if postID == uuid.Nil || optionID == uuid.Nil {
		return Poll{}, ErrPostInput
	}
	userID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionSocialPollVoteSelf)
	if err != nil {
		return Poll{}, err
	}
	repository, err := service.communityRepository()
	if err != nil {
		return Poll{}, err
	}
	return repository.Vote(ctx, userID, postID, optionID, now)
}

func (service *PostService) ClaimRedPacket(ctx context.Context, cookieToken, csrfToken string, postID, requestID uuid.UUID) (RedPacketClaim, error) {
	if postID == uuid.Nil || requestID == uuid.Nil {
		return RedPacketClaim{}, ErrPostInput
	}
	userID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionSocialRedPacketClaimSelf)
	if err != nil {
		return RedPacketClaim{}, err
	}
	repository, err := service.communityRepository()
	if err != nil {
		return RedPacketClaim{}, err
	}
	return repository.ClaimRedPacket(ctx, userID, postID, requestID, now)
}

func (service *PostService) ListManagedBoards(ctx context.Context, actor authz.StaffActor) ([]Board, error) {
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionSocialBoardManageRead, authz.SiteScope(), service.now().UTC(), "social-board-read"); err != nil {
		return nil, err
	}
	repository, err := service.communityRepository()
	if err != nil {
		return nil, err
	}
	return repository.ListBoards(ctx)
}

func (service *PostService) CreateManagedBoard(ctx context.Context, actor authz.StaffActor, input CreateBoardInput) (Board, error) {
	now := service.now().UTC()
	if validateBoardInput(input.ID, input.Name, input.Description, input.Icon, input.Tone, input.DisplayOrder, input.Reason) != nil {
		return Board{}, ErrPostInput
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionSocialBoardCreate, authz.SiteScope(), now, "social-board-create")
	if err != nil {
		return Board{}, err
	}
	repository, err := service.communityRepository()
	if err != nil {
		return Board{}, err
	}
	return repository.CreateBoard(ctx, actor, decision, input, now)
}

func (service *PostService) UpdateManagedBoard(ctx context.Context, actor authz.StaffActor, input UpdateBoardInput) (Board, error) {
	now := service.now().UTC()
	if input.ExpectedVersion < 1 || validateBoardInput(input.ID, input.Name, input.Description, input.Icon, input.Tone, input.DisplayOrder, input.Reason) != nil {
		return Board{}, ErrPostInput
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionSocialBoardUpdate, authz.SiteScope(), now, "social-board-update")
	if err != nil {
		return Board{}, err
	}
	repository, err := service.communityRepository()
	if err != nil {
		return Board{}, err
	}
	return repository.UpdateBoard(ctx, actor, decision, input, now)
}

func (service *PostService) ListManagedPosts(ctx context.Context, actor authz.StaffActor, query PostListQuery) (PostPage, error) {
	if query.Limit < 1 || query.Limit > MaxPostLimit || query.Offset < 0 || query.Offset > MaxPostOffset {
		return PostPage{}, ErrPostInput
	}
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionSocialPostManageRead, authz.SiteScope(), service.now().UTC(), "social-post-manage-read"); err != nil {
		return PostPage{}, err
	}
	repository, err := service.communityRepository()
	if err != nil {
		return PostPage{}, err
	}
	items, total, err := repository.ListManagedPosts(ctx, actor.Subject.ID, query)
	return PostPage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset, Sort: PostNewest, Feed: FeedDiscover}, err
}

func (service *PostService) ModeratePost(ctx context.Context, actor authz.StaffActor, input ModeratePostInput) (Post, error) {
	now := service.now().UTC()
	if input.PostID == uuid.Nil || !validBoardID(input.BoardID) || input.ExpectedVersion < 1 || len(strings.TrimSpace(input.Reason)) < 10 {
		return Post{}, ErrPostInput
	}
	decision, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, authz.ActionSocialPostModerate, authz.SiteScope(), now, "social-post-moderate")
	if err != nil {
		return Post{}, err
	}
	repository, err := service.communityRepository()
	if err != nil {
		return Post{}, err
	}
	return repository.ModeratePost(ctx, actor, decision, input, now)
}

func (service *PostService) communityRepository() (CommunityRepository, error) {
	repository, ok := service.repository.(CommunityRepository)
	if !ok {
		return nil, ErrPostInvariant
	}
	return repository, nil
}

func validBoardID(value string) bool { return boardIDPattern.MatchString(value) }

func validateCreatePoll(input *CreatePollInput, now time.Time) error {
	if input == nil {
		return nil
	}
	if len(strings.TrimSpace(input.Question)) < 1 || utf8.RuneCountInString(strings.TrimSpace(input.Question)) > 120 || len(input.Options) < 2 || len(input.Options) > 6 {
		return ErrPostInput
	}
	seen := map[string]struct{}{}
	for _, option := range input.Options {
		value := strings.TrimSpace(option)
		if value == "" || utf8.RuneCountInString(value) > 80 {
			return ErrPostInput
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			return ErrPostInput
		}
		seen[key] = struct{}{}
	}
	if input.ClosesAt != nil && !input.ClosesAt.After(now) {
		return ErrPostInput
	}
	return nil
}

func validateCreateRedPacket(input *CreateRedPacketInput) error {
	if input == nil {
		return nil
	}
	if input.TotalAmount < 1 || input.TotalAmount > 1_000_000 || input.ClaimCount < 1 || input.ClaimCount > 100 || input.TotalAmount < int64(input.ClaimCount) {
		return ErrPostInput
	}
	return nil
}

func validateBoardInput(id, name, description, icon, tone string, order int, reason string) error {
	validIcons := map[string]bool{"messages-square": true, "coffee": true, "folder-open": true, "clapperboard": true, "megaphone": true, "sparkles": true, "gamepad-2": true, "circle-help": true}
	validTones := map[string]bool{"coral": true, "green": true, "blue": true, "violet": true, "amber": true, "slate": true}
	if !validBoardID(id) || strings.TrimSpace(name) == "" || utf8.RuneCountInString(strings.TrimSpace(name)) > 40 || utf8.RuneCountInString(description) > 120 || !validIcons[icon] || !validTones[tone] || order < 0 || order > 1_000_000 || utf8.RuneCountInString(strings.TrimSpace(reason)) < 10 || utf8.RuneCountInString(reason) > 500 {
		return ErrPostInput
	}
	return nil
}

func extractTopics(body string) []string {
	matches := topicPattern.FindAllString(body, -1)
	result := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, match := range matches {
		display := strings.TrimPrefix(match, "#")
		key := strings.ToLower(display)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, display)
		if len(result) == 10 {
			break
		}
	}
	return result
}
