package catalog

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

const (
	DefaultTorrentBookmarkLimit       = 20
	MaxTorrentBookmarkLimit           = 50
	MaxTorrentBookmarkOffset          = 99_999
	MaxTorrentBookmarkStatusBatchSize = 100
)

var (
	ErrTorrentBookmarkInput     = errors.New("torrent bookmark input is invalid")
	ErrTorrentBookmarkNotFound  = errors.New("published catalog torrent was not found")
	ErrTorrentBookmarkInvariant = errors.New("torrent bookmark projection violates persisted invariants")
)

// TorrentBookmark reuses the canonical public summary and adds only the one
// private fact this use case owns: when the current user saved it.
type TorrentBookmark struct {
	Torrent      TorrentSummary
	BookmarkedAt time.Time
}

type TorrentBookmarkPage struct {
	Items  []TorrentBookmark
	Total  int
	Limit  int
	Offset int
}

type TorrentBookmarkState struct {
	TorrentID    int64
	BookmarkedAt time.Time
}

// torrentBookmarkRecord is the persistence-facing form before the shared
// catalog freshness policy derives TorrentSummary.SwarmStale.
type torrentBookmarkRecord struct {
	Torrent      Torrent
	BookmarkedAt time.Time
}

type TorrentBookmarkRepository interface {
	List(context.Context, uuid.UUID, int, int) ([]torrentBookmarkRecord, int, error)
	Statuses(context.Context, uuid.UUID, []int64) ([]int64, error)
	Put(context.Context, uuid.UUID, int64, time.Time) (time.Time, error)
	Delete(context.Context, uuid.UUID, int64) error
}

// TorrentBookmarkSessionAuthenticator keeps ordinary read authentication and
// CSRF-bound write authentication explicit instead of letting the repository
// infer identity from transport state.
type TorrentBookmarkSessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

type TorrentBookmarkService struct {
	authenticator TorrentBookmarkSessionAuthenticator
	authorizer    authz.Authorizer
	repository    TorrentBookmarkRepository
	now           func() time.Time
}

func NewTorrentBookmarkService(
	authenticator TorrentBookmarkSessionAuthenticator,
	authorizer authz.Authorizer,
	repository TorrentBookmarkRepository,
	now func() time.Time,
) (*TorrentBookmarkService, error) {
	if authenticator == nil || authorizer == nil || repository == nil {
		return nil, errors.New("torrent bookmark service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &TorrentBookmarkService{
		authenticator: authenticator,
		authorizer:    authorizer,
		repository:    repository,
		now:           now,
	}, nil
}

func (service *TorrentBookmarkService) List(ctx context.Context, cookieToken string, limit, offset int) (TorrentBookmarkPage, error) {
	if limit < 1 || limit > MaxTorrentBookmarkLimit || offset < 0 || offset > MaxTorrentBookmarkOffset {
		return TorrentBookmarkPage{}, ErrTorrentBookmarkInput
	}
	userID, err := service.authorizeRead(ctx, cookieToken)
	if err != nil {
		return TorrentBookmarkPage{}, err
	}
	records, total, err := service.repository.List(ctx, userID, limit, offset)
	if err != nil {
		return TorrentBookmarkPage{}, err
	}
	if total < 0 || len(records) > limit || (len(records) > 0 && offset+len(records) > total) {
		return TorrentBookmarkPage{}, ErrTorrentBookmarkInvariant
	}
	now := service.now().UTC()
	items := make([]TorrentBookmark, 0, len(records))
	for _, record := range records {
		if record.BookmarkedAt.IsZero() {
			return TorrentBookmarkPage{}, ErrTorrentBookmarkInvariant
		}
		items = append(items, TorrentBookmark{
			Torrent:      SummarizeTorrent(record.Torrent, now),
			BookmarkedAt: record.BookmarkedAt.UTC(),
		})
	}
	return TorrentBookmarkPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (service *TorrentBookmarkService) Statuses(ctx context.Context, cookieToken string, torrentIDs []int64) ([]int64, error) {
	normalized, err := normalizeTorrentBookmarkIDs(torrentIDs)
	if err != nil {
		return nil, err
	}
	userID, err := service.authorizeRead(ctx, cookieToken)
	if err != nil {
		return nil, err
	}
	return service.repository.Statuses(ctx, userID, normalized)
}

func (service *TorrentBookmarkService) Put(ctx context.Context, cookieToken, csrfToken string, torrentID int64) (TorrentBookmarkState, error) {
	if torrentID < 1 {
		return TorrentBookmarkState{}, ErrTorrentBookmarkInput
	}
	userID, now, err := service.authorizeWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return TorrentBookmarkState{}, err
	}
	createdAt, err := service.repository.Put(ctx, userID, torrentID, now)
	if err != nil {
		return TorrentBookmarkState{}, err
	}
	if createdAt.IsZero() || createdAt.After(now) {
		return TorrentBookmarkState{}, ErrTorrentBookmarkInvariant
	}
	return TorrentBookmarkState{TorrentID: torrentID, BookmarkedAt: createdAt.UTC()}, nil
}

func (service *TorrentBookmarkService) Delete(ctx context.Context, cookieToken, csrfToken string, torrentID int64) error {
	if torrentID < 1 {
		return ErrTorrentBookmarkInput
	}
	userID, _, err := service.authorizeWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return err
	}
	return service.repository.Delete(ctx, userID, torrentID)
}

func (service *TorrentBookmarkService) authorizeRead(ctx context.Context, cookieToken string) (uuid.UUID, error) {
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentBookmarkReadSelf, service.now().UTC()); err != nil {
		return uuid.Nil, err
	}
	return session.User.ID, nil
}

func (service *TorrentBookmarkService) authorizeWrite(ctx context.Context, cookieToken, csrfToken string) (uuid.UUID, time.Time, error) {
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return uuid.Nil, time.Time{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentBookmarkWriteSelf, now); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	return session.User.ID, now, nil
}

func normalizeTorrentBookmarkIDs(values []int64) ([]int64, error) {
	if len(values) < 1 || len(values) > MaxTorrentBookmarkStatusBatchSize {
		return nil, ErrTorrentBookmarkInput
	}
	seen := make(map[int64]struct{}, len(values))
	normalized := make([]int64, 0, len(values))
	for _, value := range values {
		if value < 1 {
			return nil, ErrTorrentBookmarkInput
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, ErrTorrentBookmarkInput
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized, nil
}
