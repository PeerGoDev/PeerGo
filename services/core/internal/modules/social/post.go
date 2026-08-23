package social

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

const (
	DefaultPostLimit = 20
	MaxPostLimit     = 50
	MaxPostOffset    = 99_999
	MaxPostBodyRunes = 2_000
)

var (
	ErrPostInput               = errors.New("social post input is invalid")
	ErrPostNotFound            = errors.New("visible owned social post was not found")
	ErrPostVersionConflict     = errors.New("social post version conflicts with current state")
	ErrPostIdempotencyConflict = errors.New("social post idempotency key was reused with different input")
	ErrPostInvariant           = errors.New("social post projection violates persisted invariants")
	ErrSocialBoardUnavailable  = errors.New("social board is unavailable")
	ErrSocialCommunityConflict = errors.New("social community state conflicts with current state")
)

type PostState string

const (
	PostVisible         PostState = "visible"
	PostAuthorDeleted   PostState = "author_deleted"
	PostModeratorHidden PostState = "moderator_hidden"
)

type PostSort string

const (
	PostNewest PostSort = "newest"
	PostOldest PostSort = "oldest"
	PostHot    PostSort = "hot"
)

type FeedKind string

const (
	FeedDiscover  FeedKind = "discover"
	FeedFollowing FeedKind = "following"
)

type PostAuthor struct {
	ID                uuid.UUID
	Username          string
	DisplayName       string
	FollowedByMe      bool
	Online            bool
	VIP               bool
	SiteAdministrator bool
	Medals            []AuthorMedal
}

type AuthorMedal struct {
	ID        int64
	Name      string
	ImagePath *string
}

// Post is the public dynamic-feed projection. Internal IDs, request digests,
// revision evidence and comment-thread bindings never leave the module.
type Post struct {
	ID           uuid.UUID
	Author       PostAuthor
	Body         string
	State        PostState
	Version      int64
	CommentCount int64
	LikeCount    int64
	RepostCount  int64
	LikedByMe    bool
	RepostedByMe bool
	Board        Board
	Pinned       bool
	Featured     bool
	Topics       []string
	Media        []PostMedia
	Poll         *Poll
	RedPacket    *RedPacket
	CreatedAt    time.Time
	UpdatedAt    time.Time
	EditedAt     *time.Time
}

type PostPage struct {
	Items  []Post
	Total  int64
	Limit  int
	Offset int
	Sort   PostSort
	Feed   FeedKind
}

// PostListQuery is shared by the feed and member-profile projection. An empty
// AuthorUsername selects the complete visible feed; a non-empty value narrows
// both the count and rows inside the repository's repeatable-read snapshot.
type PostListQuery struct {
	Sort           PostSort
	Limit          int
	Offset         int
	AuthorUsername string
	ViewerID       uuid.UUID
	Feed           FeedKind
	BoardID        string
	FeaturedOnly   bool
	Topic          string
}

type CreatePostInput struct {
	RequestID uuid.UUID
	Body      string
	BoardID   string
	MediaIDs  []uuid.UUID
	Poll      *CreatePollInput
	RedPacket *CreateRedPacketInput
}

type UpdatePostInput struct {
	PostID          uuid.UUID
	ExpectedVersion int64
	Body            string
}

type DeletePostInput struct {
	PostID          uuid.UUID
	ExpectedVersion int64
}

type createPostCommand struct {
	PublicID         uuid.UUID
	RequestID        uuid.UUID
	AuthorID         uuid.UUID
	Body             string
	BoardID          string
	MediaIDs         []uuid.UUID
	Poll             *CreatePollInput
	RedPacket        *CreateRedPacketInput
	Topics           []string
	CreateBodySHA256 [sha256.Size]byte
	CreatedAt        time.Time
}

type updatePostCommand struct {
	PostID          uuid.UUID
	AuthorID        uuid.UUID
	ExpectedVersion int64
	Body            string
	UpdatedAt       time.Time
}

type deletePostCommand struct {
	PostID          uuid.UUID
	AuthorID        uuid.UUID
	ExpectedVersion int64
	DeletedAt       time.Time
}

type PostRepository interface {
	List(context.Context, PostListQuery) ([]Post, int64, error)
	FindVisible(context.Context, uuid.UUID) (Post, error)
	Create(context.Context, createPostCommand) (Post, error)
	Update(context.Context, updatePostCommand) (Post, error)
	Delete(context.Context, deletePostCommand) error
}

// PostSessionAuthenticator keeps both read and CSRF-bound write credentials
// explicit. Unlike catalog content, the community feed is member-only.
type PostSessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type PostService struct {
	authenticator PostSessionAuthenticator
	authorizer    authz.Authorizer
	repository    PostRepository
	now           func() time.Time
}

func NewPostService(authenticator PostSessionAuthenticator, authorizer authz.Authorizer, repository PostRepository, now func() time.Time) (*PostService, error) {
	if authenticator == nil || authorizer == nil || repository == nil {
		return nil, errors.New("social post service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &PostService{authenticator: authenticator, authorizer: authorizer, repository: repository, now: now}, nil
}

func (service *PostService) List(ctx context.Context, cookieToken string, query PostListQuery) (PostPage, error) {
	query.AuthorUsername = strings.TrimSpace(query.AuthorUsername)
	query.BoardID = strings.TrimSpace(query.BoardID)
	query.Topic = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(query.Topic, "#")))
	if query.Feed == "" {
		query.Feed = FeedDiscover
	}
	if !validPostSort(query.Sort) || !validFeedKind(query.Feed) || query.Limit < 1 || query.Limit > MaxPostLimit || query.Offset < 0 || query.Offset > MaxPostOffset || utf8.RuneCountInString(query.AuthorUsername) > 64 || utf8.RuneCountInString(query.BoardID) > 64 || utf8.RuneCountInString(query.Topic) > 40 {
		return PostPage{}, ErrPostInput
	}
	viewerID, err := service.authorizeRead(ctx, cookieToken)
	if err != nil {
		return PostPage{}, err
	}
	query.ViewerID = viewerID
	items, total, err := service.repository.List(ctx, query)
	if err != nil {
		return PostPage{}, err
	}
	if total < 0 || len(items) > query.Limit || (len(items) > 0 && int64(query.Offset+len(items)) > total) {
		return PostPage{}, ErrPostInvariant
	}
	for _, item := range items {
		if validatePersistedPost(item) != nil || item.State != PostVisible {
			return PostPage{}, ErrPostInvariant
		}
	}
	return PostPage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset, Sort: query.Sort, Feed: query.Feed}, nil
}

func (service *PostService) FindVisible(ctx context.Context, cookieToken string, postID uuid.UUID) (Post, error) {
	if postID == uuid.Nil {
		return Post{}, ErrPostInput
	}
	viewerID, err := service.authorizeRead(ctx, cookieToken)
	if err != nil {
		return Post{}, err
	}
	post, err := service.repository.FindVisible(ctx, postID)
	if err != nil {
		return Post{}, err
	}
	if validatePersistedPost(post) != nil || post.ID != postID || post.State != PostVisible {
		return Post{}, ErrPostInvariant
	}
	if community, ok := service.repository.(CommunityRepository); ok {
		posts, enrichErr := community.EnrichPosts(ctx, viewerID, []Post{post}, service.now().UTC())
		if enrichErr != nil {
			return Post{}, enrichErr
		}
		post = posts[0]
	}
	return post, nil
}

func (service *PostService) Create(ctx context.Context, cookieToken, csrfToken string, input CreatePostInput) (Post, error) {
	body, err := normalizePostBody(input.Body)
	boardID := strings.TrimSpace(input.BoardID)
	if boardID == "" {
		boardID = "general"
	}
	validationNow := service.now().UTC()
	if err != nil || input.RequestID == uuid.Nil || !validBoardID(boardID) || len(input.MediaIDs) > MaxPostMedia || validateCreatePoll(input.Poll, validationNow) != nil || validateCreateRedPacket(input.RedPacket) != nil {
		return Post{}, ErrPostInput
	}
	authorID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionSocialPostCreateSelf)
	if err != nil {
		return Post{}, err
	}
	post, err := service.repository.Create(ctx, createPostCommand{
		PublicID: uuid.New(), RequestID: input.RequestID, AuthorID: authorID, Body: body,
		BoardID: boardID, MediaIDs: append([]uuid.UUID(nil), input.MediaIDs...), Poll: input.Poll,
		RedPacket: input.RedPacket, Topics: extractTopics(body),
		CreateBodySHA256: createPostInputSHA256(body, boardID, input.MediaIDs, input.Poll, input.RedPacket), CreatedAt: now,
	})
	if err != nil {
		return Post{}, err
	}
	if validatePersistedPost(post) != nil || post.Author.ID != authorID || post.State != PostVisible {
		return Post{}, ErrPostInvariant
	}
	if community, ok := service.repository.(CommunityRepository); ok {
		posts, enrichErr := community.EnrichPosts(ctx, authorID, []Post{post}, now)
		if enrichErr != nil {
			return Post{}, enrichErr
		}
		post = posts[0]
	}
	return post, nil
}

func (service *PostService) UpdateMyPost(ctx context.Context, cookieToken, csrfToken string, input UpdatePostInput) (Post, error) {
	body, err := normalizePostBody(input.Body)
	if err != nil || input.PostID == uuid.Nil || input.ExpectedVersion < 1 {
		return Post{}, ErrPostInput
	}
	authorID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionSocialPostUpdateSelf)
	if err != nil {
		return Post{}, err
	}
	post, err := service.repository.Update(ctx, updatePostCommand{
		PostID: input.PostID, AuthorID: authorID, ExpectedVersion: input.ExpectedVersion, Body: body, UpdatedAt: now,
	})
	if err != nil {
		return Post{}, err
	}
	if validatePersistedPost(post) != nil || post.ID != input.PostID || post.Author.ID != authorID || post.State != PostVisible {
		return Post{}, ErrPostInvariant
	}
	if community, ok := service.repository.(CommunityRepository); ok {
		posts, enrichErr := community.EnrichPosts(ctx, authorID, []Post{post}, now)
		if enrichErr != nil {
			return Post{}, enrichErr
		}
		post = posts[0]
	}
	return post, nil
}

func (service *PostService) DeleteMyPost(ctx context.Context, cookieToken, csrfToken string, input DeletePostInput) error {
	if input.PostID == uuid.Nil || input.ExpectedVersion < 1 {
		return ErrPostInput
	}
	authorID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionSocialPostDeleteSelf)
	if err != nil {
		return err
	}
	return service.repository.Delete(ctx, deletePostCommand{
		PostID: input.PostID, AuthorID: authorID, ExpectedVersion: input.ExpectedVersion, DeletedAt: now,
	})
}

func (service *PostService) authorizeRead(ctx context.Context, cookieToken string) (uuid.UUID, error) {
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return uuid.Nil, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebMemberAction(ctx, service.authorizer, session.User.ID, authz.ActionSocialPostRead, now); err != nil {
		return uuid.Nil, err
	}
	return session.User.ID, nil
}

func createPostInputSHA256(body, boardID string, mediaIDs []uuid.UUID, poll *CreatePollInput, redPacket *CreateRedPacketInput) [sha256.Size]byte {
	type canonicalPoll struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
		ClosesAt string   `json:"closes_at,omitempty"`
	}
	type canonicalRedPacket struct {
		TotalAmount int64 `json:"total_amount"`
		ClaimCount  int   `json:"claim_count"`
	}
	type canonicalInput struct {
		Body      string              `json:"body"`
		BoardID   string              `json:"board_id"`
		MediaIDs  []string            `json:"media_ids"`
		Poll      *canonicalPoll      `json:"poll,omitempty"`
		RedPacket *canonicalRedPacket `json:"red_packet,omitempty"`
	}
	canonical := canonicalInput{Body: body, BoardID: boardID, MediaIDs: make([]string, 0, len(mediaIDs))}
	for _, mediaID := range mediaIDs {
		canonical.MediaIDs = append(canonical.MediaIDs, mediaID.String())
	}
	if poll != nil {
		canonical.Poll = &canonicalPoll{Question: strings.TrimSpace(poll.Question), Options: make([]string, 0, len(poll.Options))}
		for _, option := range poll.Options {
			canonical.Poll.Options = append(canonical.Poll.Options, strings.TrimSpace(option))
		}
		if poll.ClosesAt != nil {
			canonical.Poll.ClosesAt = poll.ClosesAt.UTC().Format(time.RFC3339Nano)
		}
	}
	if redPacket != nil {
		canonical.RedPacket = &canonicalRedPacket{TotalAmount: redPacket.TotalAmount, ClaimCount: redPacket.ClaimCount}
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		panic("canonical social post input cannot fail to encode: " + err.Error())
	}
	return sha256.Sum256(payload)
}

func createPostDigestMatches(stored []byte, command createPostCommand) bool {
	if bytes.Equal(stored, command.CreateBodySHA256[:]) {
		return true
	}
	// Posts created before boards and attachments existed stored a digest of the
	// body only. Preserve retry compatibility for that exact legacy shape while
	// still rejecting a reused request ID that adds any new feature.
	legacyShape := command.BoardID == "general" && len(command.MediaIDs) == 0 && command.Poll == nil && command.RedPacket == nil
	legacyDigest := sha256.Sum256([]byte(command.Body))
	return legacyShape && bytes.Equal(stored, legacyDigest[:])
}

func (service *PostService) authorizeWrite(ctx context.Context, cookieToken, csrfToken string, action authz.Action) (uuid.UUID, time.Time, error) {
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, action, now); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	return session.User.ID, now, nil
}

func normalizePostBody(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrPostInput
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.TrimSpace(value)
	count := utf8.RuneCountInString(value)
	if count < 1 || count > MaxPostBodyRunes {
		return "", ErrPostInput
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return "", ErrPostInput
		}
	}
	return value, nil
}

func validatePersistedPost(post Post) error {
	if post.ID == uuid.Nil || post.Author.ID == uuid.Nil || strings.TrimSpace(post.Author.Username) == "" ||
		strings.TrimSpace(post.Author.DisplayName) == "" || utf8.RuneCountInString(post.Author.Username) > 64 ||
		utf8.RuneCountInString(post.Author.DisplayName) > 80 || post.Version < 1 || post.CommentCount < 0 ||
		post.CreatedAt.IsZero() || post.UpdatedAt.Before(post.CreatedAt) {
		return ErrPostInvariant
	}
	switch post.State {
	case PostVisible:
		if _, err := normalizePostBody(post.Body); err != nil {
			return ErrPostInvariant
		}
	case PostAuthorDeleted:
		if post.Body != "" {
			return ErrPostInvariant
		}
	default:
		return ErrPostInvariant
	}
	if post.EditedAt != nil && (post.EditedAt.Before(post.CreatedAt) || post.EditedAt.After(post.UpdatedAt)) {
		return ErrPostInvariant
	}
	return nil
}

func validPostSort(sort PostSort) bool {
	return sort == PostNewest || sort == PostOldest || sort == PostHot
}

func validFeedKind(feed FeedKind) bool {
	return feed == FeedDiscover || feed == FeedFollowing
}
