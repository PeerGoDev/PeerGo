package social

import (
	"context"
	"crypto/sha256"
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
)

type PostState string

const (
	PostVisible       PostState = "visible"
	PostAuthorDeleted PostState = "author_deleted"
)

type PostSort string

const (
	PostNewest PostSort = "newest"
	PostOldest PostSort = "oldest"
)

type PostAuthor struct {
	ID          uuid.UUID
	Username    string
	DisplayName string
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
}

// PostListQuery is shared by the feed and member-profile projection. An empty
// AuthorUsername selects the complete visible feed; a non-empty value narrows
// both the count and rows inside the repository's repeatable-read snapshot.
type PostListQuery struct {
	Sort           PostSort
	Limit          int
	Offset         int
	AuthorUsername string
}

type CreatePostInput struct {
	RequestID uuid.UUID
	Body      string
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
	if !validPostSort(query.Sort) || query.Limit < 1 || query.Limit > MaxPostLimit || query.Offset < 0 || query.Offset > MaxPostOffset || utf8.RuneCountInString(query.AuthorUsername) > 64 {
		return PostPage{}, ErrPostInput
	}
	if _, err := service.authorizeRead(ctx, cookieToken); err != nil {
		return PostPage{}, err
	}
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
	return PostPage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset, Sort: query.Sort}, nil
}

func (service *PostService) FindVisible(ctx context.Context, cookieToken string, postID uuid.UUID) (Post, error) {
	if postID == uuid.Nil {
		return Post{}, ErrPostInput
	}
	if _, err := service.authorizeRead(ctx, cookieToken); err != nil {
		return Post{}, err
	}
	post, err := service.repository.FindVisible(ctx, postID)
	if err != nil {
		return Post{}, err
	}
	if validatePersistedPost(post) != nil || post.ID != postID || post.State != PostVisible {
		return Post{}, ErrPostInvariant
	}
	return post, nil
}

func (service *PostService) Create(ctx context.Context, cookieToken, csrfToken string, input CreatePostInput) (Post, error) {
	body, err := normalizePostBody(input.Body)
	if err != nil || input.RequestID == uuid.Nil {
		return Post{}, ErrPostInput
	}
	authorID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionSocialPostCreateSelf)
	if err != nil {
		return Post{}, err
	}
	post, err := service.repository.Create(ctx, createPostCommand{
		PublicID: uuid.New(), RequestID: input.RequestID, AuthorID: authorID, Body: body,
		CreateBodySHA256: sha256.Sum256([]byte(body)), CreatedAt: now,
	})
	if err != nil {
		return Post{}, err
	}
	if validatePersistedPost(post) != nil || post.Author.ID != authorID || post.State != PostVisible {
		return Post{}, ErrPostInvariant
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
	return sort == PostNewest || sort == PostOldest
}
