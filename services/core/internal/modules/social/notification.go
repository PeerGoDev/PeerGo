package social

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const (
	DefaultSocialNotificationLimit = 20
	MaxSocialNotificationLimit     = 50
	MaxSocialNotificationOffset    = 99_999
)

var (
	ErrSocialNotificationInput     = errors.New("social notification input is invalid")
	ErrSocialNotificationNotFound  = errors.New("social notification was not found")
	ErrSocialNotificationInvariant = errors.New("social notification projection violates persisted invariants")
)

type SocialNotificationKind string

const (
	SocialNotificationFollow       SocialNotificationKind = "follow"
	SocialNotificationPostLike     SocialNotificationKind = "post_like"
	SocialNotificationPostRepost   SocialNotificationKind = "post_repost"
	SocialNotificationPostComment  SocialNotificationKind = "post_comment"
	SocialNotificationCommentReply SocialNotificationKind = "comment_reply"
)

type SocialNotificationCategory string

const (
	SocialNotificationAll     SocialNotificationCategory = "all"
	SocialNotificationReplies SocialNotificationCategory = "replies"
	SocialNotificationLikes   SocialNotificationCategory = "likes"
	SocialNotificationFollows SocialNotificationCategory = "follows"
)

type SocialNotification struct {
	ID             uuid.UUID
	Kind           SocialNotificationKind
	Actor          PostAuthor
	PostID         *uuid.UUID
	CommentID      *uuid.UUID
	PostPreview    string
	CommentPreview string
	CreatedAt      time.Time
	ReadAt         *time.Time
}

type SocialNotificationQuery struct {
	Category SocialNotificationCategory
	Limit    int
	Offset   int
}

type SocialNotificationPage struct {
	Items       []SocialNotification
	Total       int
	UnreadCount int
	Limit       int
	Offset      int
}

type SocialNotificationSummary struct {
	UnreadCount int
}

type SocialNotificationReadReceipt struct {
	NotificationID uuid.UUID
	ReadAt         time.Time
	AlreadyRead    bool
}

type SocialNotificationReadAllReceipt struct {
	UpdatedCount int
	ReadAt       time.Time
}

type SocialNotificationRepository interface {
	ListNotifications(context.Context, uuid.UUID, SocialNotificationQuery, time.Time) (SocialNotificationPage, error)
	NotificationSummary(context.Context, uuid.UUID) (SocialNotificationSummary, error)
	MarkNotificationRead(context.Context, uuid.UUID, uuid.UUID, time.Time) (SocialNotificationReadReceipt, error)
	MarkAllNotificationsRead(context.Context, uuid.UUID, time.Time) (SocialNotificationReadAllReceipt, error)
}

func (service *PostService) ListNotifications(ctx context.Context, cookieToken string, query SocialNotificationQuery) (SocialNotificationPage, error) {
	if query.Category == "" {
		query.Category = SocialNotificationAll
	}
	if !validSocialNotificationCategory(query.Category) || query.Limit < 1 || query.Limit > MaxSocialNotificationLimit || query.Offset < 0 || query.Offset > MaxSocialNotificationOffset {
		return SocialNotificationPage{}, ErrSocialNotificationInput
	}
	userID, now, err := service.authorizeNotificationRead(ctx, cookieToken)
	if err != nil {
		return SocialNotificationPage{}, err
	}
	repository, err := service.socialNotificationRepository()
	if err != nil {
		return SocialNotificationPage{}, err
	}
	page, err := repository.ListNotifications(ctx, userID, query, now)
	if err != nil {
		return SocialNotificationPage{}, err
	}
	if page.Total < 0 || page.UnreadCount < 0 || len(page.Items) > query.Limit || (len(page.Items) > 0 && query.Offset+len(page.Items) > page.Total) {
		return SocialNotificationPage{}, ErrSocialNotificationInvariant
	}
	page.Limit = query.Limit
	page.Offset = query.Offset
	return page, nil
}

func (service *PostService) NotificationSummary(ctx context.Context, cookieToken string) (SocialNotificationSummary, error) {
	userID, _, err := service.authorizeNotificationRead(ctx, cookieToken)
	if err != nil {
		return SocialNotificationSummary{}, err
	}
	repository, err := service.socialNotificationRepository()
	if err != nil {
		return SocialNotificationSummary{}, err
	}
	return repository.NotificationSummary(ctx, userID)
}

func (service *PostService) MarkNotificationRead(ctx context.Context, cookieToken, csrfToken string, notificationID uuid.UUID) (SocialNotificationReadReceipt, error) {
	if notificationID == uuid.Nil {
		return SocialNotificationReadReceipt{}, ErrSocialNotificationInput
	}
	userID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionNotificationReadStateWriteSelf)
	if err != nil {
		return SocialNotificationReadReceipt{}, err
	}
	repository, err := service.socialNotificationRepository()
	if err != nil {
		return SocialNotificationReadReceipt{}, err
	}
	return repository.MarkNotificationRead(ctx, userID, notificationID, now)
}

func (service *PostService) MarkAllNotificationsRead(ctx context.Context, cookieToken, csrfToken string) (SocialNotificationReadAllReceipt, error) {
	userID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken, authz.ActionNotificationReadStateWriteSelf)
	if err != nil {
		return SocialNotificationReadAllReceipt{}, err
	}
	repository, err := service.socialNotificationRepository()
	if err != nil {
		return SocialNotificationReadAllReceipt{}, err
	}
	return repository.MarkAllNotificationsRead(ctx, userID, now)
}

func (service *PostService) authorizeNotificationRead(ctx context.Context, cookieToken string) (uuid.UUID, time.Time, error) {
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionNotificationReadSelf, now); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	return session.User.ID, now, nil
}

func (service *PostService) socialNotificationRepository() (SocialNotificationRepository, error) {
	repository, ok := service.repository.(SocialNotificationRepository)
	if !ok {
		return nil, errors.New("social notification repository is unavailable")
	}
	return repository, nil
}

func validSocialNotificationCategory(category SocialNotificationCategory) bool {
	switch category {
	case SocialNotificationAll, SocialNotificationReplies, SocialNotificationLikes, SocialNotificationFollows:
		return true
	default:
		return false
	}
}
