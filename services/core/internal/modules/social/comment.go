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
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

const (
	DefaultCommentLimit = 20
	MaxCommentLimit     = 50
	MaxCommentOffset    = 99_999
	MaxCommentBodyRunes = 2_000
)

var (
	ErrCommentInput               = errors.New("comment input is invalid")
	ErrCommentTargetNotFound      = errors.New("published comment target was not found")
	ErrCommentParentNotFound      = errors.New("visible comment parent was not found")
	ErrCommentNotFound            = errors.New("owned visible comment was not found")
	ErrCommentThreadLocked        = errors.New("comment thread is locked")
	ErrCommentVersionConflict     = errors.New("comment version conflicts with current state")
	ErrCommentIdempotencyConflict = errors.New("comment idempotency key was reused with different input")
	ErrCommentInvariant           = errors.New("comment projection violates persisted invariants")
)

type CommentState string

const (
	CommentVisible         CommentState = "visible"
	CommentAuthorDeleted   CommentState = "author_deleted"
	CommentModeratorHidden CommentState = "moderator_hidden"
)

type CommentBodyFormat string

const (
	CommentBodyPlainText    CommentBodyFormat = "plain_text"
	CommentBodyLegacyBBCode CommentBodyFormat = "legacy_bbcode"
)

type CommentAuthor struct {
	ID                uuid.UUID
	Username          string
	DisplayName       string
	Online            bool
	VIP               bool
	SiteAdministrator bool
	Medals            []AuthorMedal
}

type CommentTargetKind string

const (
	CommentTargetTorrent      CommentTargetKind = "torrent"
	CommentTargetAnnouncement CommentTargetKind = "announcement"
	CommentTargetPost         CommentTargetKind = "post"
)

// CommentTarget is a closed value object, not a loose target type/key pair.
// Exactly one typed identity is populated and persistence still uses a target-
// specific binding table with a real foreign key.
type CommentTarget struct {
	Kind           CommentTargetKind
	TorrentID      int64
	AnnouncementID string
	PostPublicID   uuid.UUID
}

func TorrentCommentTarget(torrentID int64) CommentTarget {
	return CommentTarget{Kind: CommentTargetTorrent, TorrentID: torrentID}
}

func AnnouncementCommentTarget(announcementID string) CommentTarget {
	return CommentTarget{Kind: CommentTargetAnnouncement, AnnouncementID: announcementID}
}

func PostCommentTarget(postPublicID uuid.UUID) CommentTarget {
	return CommentTarget{Kind: CommentTargetPost, PostPublicID: postPublicID}
}

func (target CommentTarget) Valid() bool {
	return validateCommentTarget(target) == nil
}

// Comment is the public social projection. Internal numeric IDs, create
// request digests, revision history and target binding rows never cross the
// module boundary.
type Comment struct {
	ID              uuid.UUID
	Target          CommentTarget
	ParentCommentID *uuid.UUID
	RootCommentID   *uuid.UUID
	Author          CommentAuthor
	ReplyTo         *CommentAuthor
	Body            string
	BodyFormat      CommentBodyFormat
	State           CommentState
	Version         int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
	EditedAt        *time.Time
}

type CommentPage struct {
	Target CommentTarget
	Items  []Comment
	Total  int64
	Limit  int
	Offset int
}

type CommentThreadSort string

const (
	CommentThreadHot    CommentThreadSort = "hot"
	CommentThreadNewest CommentThreadSort = "newest"
	CommentThreadOldest CommentThreadSort = "oldest"
)

type CommentThreadPage struct {
	Target      CommentTarget
	Items       []Comment
	Total       int64
	ThreadTotal int64
	Limit       int
	Offset      int
}

type CreateTorrentCommentInput struct {
	RequestID       uuid.UUID
	TorrentID       int64
	ParentCommentID *uuid.UUID
	Body            string
}

type CreateAnnouncementCommentInput struct {
	RequestID       uuid.UUID
	AnnouncementID  string
	ParentCommentID *uuid.UUID
	Body            string
}

type CreatePostCommentInput struct {
	RequestID       uuid.UUID
	PostPublicID    uuid.UUID
	ParentCommentID *uuid.UUID
	Body            string
}

type UpdateCommentInput struct {
	CommentID       uuid.UUID
	ExpectedVersion int64
	Body            string
}

type DeleteCommentInput struct {
	CommentID       uuid.UUID
	ExpectedVersion int64
}

type createCommentCommand struct {
	PublicID         uuid.UUID
	RequestID        uuid.UUID
	Target           CommentTarget
	ParentCommentID  *uuid.UUID
	AuthorID         uuid.UUID
	Body             string
	CreateBodySHA256 [sha256.Size]byte
	CreatedAt        time.Time
}

type updateCommentCommand struct {
	CommentID       uuid.UUID
	AuthorID        uuid.UUID
	ExpectedVersion int64
	Body            string
	UpdatedAt       time.Time
}

type deleteCommentCommand struct {
	CommentID       uuid.UUID
	AuthorID        uuid.UUID
	ExpectedVersion int64
	DeletedAt       time.Time
}

type CommentRepository interface {
	List(context.Context, CommentTarget, int, int) ([]Comment, int64, error)
	ListThreads(context.Context, CommentTarget, CommentThreadSort, int, int) ([]Comment, int64, int64, error)
	Create(context.Context, createCommentCommand) (Comment, error)
	Update(context.Context, updateCommentCommand) (Comment, error)
	Delete(context.Context, deleteCommentCommand) error
}

// CommentSessionAuthenticator makes the CSRF-bound Web credential explicit at
// the social boundary. Public reads do not consult session state.
type CommentSessionAuthenticator interface {
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type CommentService struct {
	authenticator CommentSessionAuthenticator
	authorizer    authz.Authorizer
	repository    CommentRepository
	now           func() time.Time
}

func NewCommentService(
	authenticator CommentSessionAuthenticator,
	authorizer authz.Authorizer,
	repository CommentRepository,
	now func() time.Time,
) (*CommentService, error) {
	if authenticator == nil || authorizer == nil || repository == nil {
		return nil, errors.New("comment service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &CommentService{
		authenticator: authenticator,
		authorizer:    authorizer,
		repository:    repository,
		now:           now,
	}, nil
}

func (service *CommentService) ListTorrentComments(ctx context.Context, torrentID int64, limit, offset int) (CommentPage, error) {
	return service.listComments(ctx, TorrentCommentTarget(torrentID), limit, offset)
}

func (service *CommentService) ListAnnouncementComments(ctx context.Context, announcementID string, limit, offset int) (CommentPage, error) {
	return service.listComments(ctx, AnnouncementCommentTarget(announcementID), limit, offset)
}

func (service *CommentService) ListPostComments(ctx context.Context, postPublicID uuid.UUID, sort CommentThreadSort, limit, offset int) (CommentThreadPage, error) {
	target := PostCommentTarget(postPublicID)
	if validateCommentTarget(target) != nil || !validCommentThreadSort(sort) || limit < 1 || limit > MaxCommentLimit || offset < 0 || offset > MaxCommentOffset {
		return CommentThreadPage{}, ErrCommentInput
	}
	items, total, threadTotal, err := service.repository.ListThreads(ctx, target, sort, limit, offset)
	if err != nil {
		return CommentThreadPage{}, err
	}
	rootIDs := make(map[uuid.UUID]struct{}, limit)
	for _, item := range items {
		if item.Target != target || validatePersistedComment(item) != nil {
			return CommentThreadPage{}, ErrCommentInvariant
		}
		if item.ParentCommentID == nil {
			if item.RootCommentID != nil || item.ReplyTo != nil {
				return CommentThreadPage{}, ErrCommentInvariant
			}
			rootIDs[item.ID] = struct{}{}
			continue
		}
		if item.RootCommentID == nil || item.ReplyTo == nil {
			return CommentThreadPage{}, ErrCommentInvariant
		}
	}
	for _, item := range items {
		if item.RootCommentID != nil {
			if _, ok := rootIDs[*item.RootCommentID]; !ok {
				return CommentThreadPage{}, ErrCommentInvariant
			}
		}
	}
	if total < 0 || threadTotal < 0 || total < threadTotal || len(rootIDs) > limit ||
		(len(rootIDs) > 0 && int64(offset+len(rootIDs)) > threadTotal) {
		return CommentThreadPage{}, ErrCommentInvariant
	}
	return CommentThreadPage{
		Target: target, Items: items, Total: total, ThreadTotal: threadTotal,
		Limit: limit, Offset: offset,
	}, nil
}

func (service *CommentService) listComments(ctx context.Context, target CommentTarget, limit, offset int) (CommentPage, error) {
	if validateCommentTarget(target) != nil || limit < 1 || limit > MaxCommentLimit || offset < 0 || offset > MaxCommentOffset {
		return CommentPage{}, ErrCommentInput
	}
	items, total, err := service.repository.List(ctx, target, limit, offset)
	if err != nil {
		return CommentPage{}, err
	}
	if total < 0 || len(items) > limit || (len(items) > 0 && int64(offset+len(items)) > total) {
		return CommentPage{}, ErrCommentInvariant
	}
	for _, item := range items {
		if item.Target != target || validatePersistedComment(item) != nil {
			return CommentPage{}, ErrCommentInvariant
		}
	}
	return CommentPage{
		Target: target,
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func (service *CommentService) CreateTorrentComment(ctx context.Context, cookieToken, csrfToken string, input CreateTorrentCommentInput) (Comment, error) {
	return service.createComment(ctx, cookieToken, csrfToken, createCommentInput{
		RequestID: input.RequestID, Target: TorrentCommentTarget(input.TorrentID),
		ParentCommentID: input.ParentCommentID, Body: input.Body,
	}, authz.ActionTorrentCommentCreateSelf)
}

func (service *CommentService) CreateAnnouncementComment(ctx context.Context, cookieToken, csrfToken string, input CreateAnnouncementCommentInput) (Comment, error) {
	return service.createComment(ctx, cookieToken, csrfToken, createCommentInput{
		RequestID: input.RequestID, Target: AnnouncementCommentTarget(input.AnnouncementID),
		ParentCommentID: input.ParentCommentID, Body: input.Body,
	}, authz.ActionAnnouncementCommentCreateSelf)
}

func (service *CommentService) CreatePostComment(ctx context.Context, cookieToken, csrfToken string, input CreatePostCommentInput) (Comment, error) {
	return service.createComment(ctx, cookieToken, csrfToken, createCommentInput{
		RequestID: input.RequestID, Target: PostCommentTarget(input.PostPublicID),
		ParentCommentID: input.ParentCommentID, Body: input.Body,
	}, authz.ActionSocialPostCommentCreateSelf)
}

type createCommentInput struct {
	RequestID       uuid.UUID
	Target          CommentTarget
	ParentCommentID *uuid.UUID
	Body            string
}

func (service *CommentService) createComment(ctx context.Context, cookieToken, csrfToken string, input createCommentInput, action authz.Action) (Comment, error) {
	body, err := normalizeCommentBody(input.Body)
	if err != nil || input.RequestID == uuid.Nil || validateCommentTarget(input.Target) != nil || invalidOptionalUUID(input.ParentCommentID) {
		return Comment{}, ErrCommentInput
	}
	authorID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, action)
	if err != nil {
		return Comment{}, err
	}
	comment, err := service.repository.Create(ctx, createCommentCommand{
		PublicID:         uuid.New(),
		RequestID:        input.RequestID,
		Target:           input.Target,
		ParentCommentID:  copyUUIDPointer(input.ParentCommentID),
		AuthorID:         authorID,
		Body:             body,
		CreateBodySHA256: sha256.Sum256([]byte(body)),
		CreatedAt:        now,
	})
	if err != nil {
		return Comment{}, err
	}
	if validatePersistedComment(comment) != nil || comment.Target != input.Target || comment.Author.ID != authorID {
		return Comment{}, ErrCommentInvariant
	}
	return comment, nil
}

func (service *CommentService) UpdateMyComment(ctx context.Context, cookieToken, csrfToken string, input UpdateCommentInput) (Comment, error) {
	body, err := normalizeCommentBody(input.Body)
	if err != nil || input.CommentID == uuid.Nil || input.ExpectedVersion < 1 {
		return Comment{}, ErrCommentInput
	}
	authorID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionCommentUpdateSelf)
	if err != nil {
		return Comment{}, err
	}
	comment, err := service.repository.Update(ctx, updateCommentCommand{
		CommentID:       input.CommentID,
		AuthorID:        authorID,
		ExpectedVersion: input.ExpectedVersion,
		Body:            body,
		UpdatedAt:       now,
	})
	if err != nil {
		return Comment{}, err
	}
	if validatePersistedComment(comment) != nil || comment.ID != input.CommentID || comment.Author.ID != authorID {
		return Comment{}, ErrCommentInvariant
	}
	return comment, nil
}

func (service *CommentService) DeleteMyComment(ctx context.Context, cookieToken, csrfToken string, input DeleteCommentInput) error {
	if input.CommentID == uuid.Nil || input.ExpectedVersion < 1 {
		return ErrCommentInput
	}
	authorID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionCommentDeleteSelf)
	if err != nil {
		return err
	}
	return service.repository.Delete(ctx, deleteCommentCommand{
		CommentID:       input.CommentID,
		AuthorID:        authorID,
		ExpectedVersion: input.ExpectedVersion,
		DeletedAt:       now,
	})
}

func (service *CommentService) authorizeWrite(ctx context.Context, cookieToken, csrfToken string, action authz.Action) (uuid.UUID, time.Time, error) {
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

func normalizeCommentBody(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrCommentInput
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.TrimSpace(value)
	count := utf8.RuneCountInString(value)
	if count < 1 || count > MaxCommentBodyRunes {
		return "", ErrCommentInput
	}
	for _, character := range value {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return "", ErrCommentInput
		}
	}
	return value, nil
}

func validatePersistedComment(comment Comment) error {
	if comment.ID == uuid.Nil || validateCommentTarget(comment.Target) != nil || comment.Author.ID == uuid.Nil ||
		invalidOptionalUUID(comment.ParentCommentID) || invalidOptionalUUID(comment.RootCommentID) ||
		(comment.ParentCommentID == nil && (comment.RootCommentID != nil || comment.ReplyTo != nil)) ||
		strings.TrimSpace(comment.Author.DisplayName) == "" ||
		utf8.RuneCountInString(comment.Author.DisplayName) > 80 || comment.Version < 1 ||
		comment.CreatedAt.IsZero() || comment.UpdatedAt.Before(comment.CreatedAt) {
		return ErrCommentInvariant
	}
	if comment.BodyFormat != CommentBodyPlainText && comment.BodyFormat != CommentBodyLegacyBBCode {
		return ErrCommentInvariant
	}
	switch comment.State {
	case CommentVisible:
		if _, err := normalizeCommentBody(comment.Body); err != nil {
			return ErrCommentInvariant
		}
	case CommentAuthorDeleted, CommentModeratorHidden:
		if comment.Body != "" {
			return ErrCommentInvariant
		}
	default:
		return ErrCommentInvariant
	}
	if comment.EditedAt != nil && (comment.EditedAt.Before(comment.CreatedAt) || comment.EditedAt.After(comment.UpdatedAt)) {
		return ErrCommentInvariant
	}
	return nil
}

func validCommentThreadSort(sort CommentThreadSort) bool {
	return sort == CommentThreadHot || sort == CommentThreadNewest || sort == CommentThreadOldest
}

func validateCommentTarget(target CommentTarget) error {
	switch target.Kind {
	case CommentTargetTorrent:
		if target.TorrentID < 1 || target.AnnouncementID != "" || target.PostPublicID != uuid.Nil {
			return ErrCommentInvariant
		}
	case CommentTargetAnnouncement:
		if target.TorrentID != 0 || !catalog.ValidAnnouncementID(target.AnnouncementID) || target.PostPublicID != uuid.Nil {
			return ErrCommentInvariant
		}
	case CommentTargetPost:
		if target.TorrentID != 0 || target.AnnouncementID != "" || target.PostPublicID == uuid.Nil {
			return ErrCommentInvariant
		}
	default:
		return ErrCommentInvariant
	}
	return nil
}

func invalidOptionalUUID(value *uuid.UUID) bool {
	return value != nil && *value == uuid.Nil
}

func copyUUIDPointer(value *uuid.UUID) *uuid.UUID {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
