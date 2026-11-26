package torrents

import (
	"context"
	"errors"
	"time"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

var (
	ErrTorrentReseedNotFound        = errors.New("published torrent was not found")
	ErrTorrentReseedUnavailable     = errors.New("torrent reseed requires a fresh zero-seeder snapshot")
	ErrTorrentReseedEmailUnverified = errors.New("torrent reseed requires a verified email")
)

func (service *TorrentMaintenanceService) RequestTorrentReseed(ctx context.Context, cookieToken, csrfToken string, torrentID TorrentID) error {
	if torrentID < 1 {
		return ErrTorrentReseedNotFound
	}
	session, err := service.authenticator.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return err
	}
	if session.User.EmailVerifiedAt == nil {
		return ErrTorrentReseedEmailUnverified
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTorrentReseedRequestSelf, now); err != nil {
		return err
	}
	return service.repository.RequestTorrentReseed(ctx, torrentID, session.User.ID, now.Truncate(time.Microsecond))
}
