package traffic

import (
	"context"
	"errors"
	"time"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type SessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
	AuthenticateWrite(context.Context, string, string) (identity.WebSession, error)
}

// Service is the authenticated, default-deny read boundary for a user's own
// final traffic projection. It has no port for Tracker Ledger or policy
// evaluation, which prevents request-time history rewrites by construction.
type Service struct {
	authenticator SessionAuthenticator
	authorizer    authz.Authorizer
	repository    Repository
	now           func() time.Time
}

type Repository interface {
	OverviewRepository
	HNRRepository
	HNRAppealRepository
}

func NewService(authenticator SessionAuthenticator, authorizer authz.Authorizer, repository Repository, now func() time.Time) (*Service, error) {
	if authenticator == nil || authorizer == nil || repository == nil {
		return nil, errors.New("traffic overview service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{authenticator: authenticator, authorizer: authorizer, repository: repository, now: now}, nil
}

// MyOverview authenticates the ordinary Web audience before authorizing the
// typed self relationship. The repository receives only the verified user ID;
// callers cannot choose another account through a query or path parameter.
func (service *Service) MyOverview(ctx context.Context, cookieToken string, limit int) (Overview, error) {
	if limit < 1 || limit > MaximumOverviewLimit {
		return Overview{}, ErrInput
	}
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return Overview{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionTrafficReadSelf, service.now().UTC()); err != nil {
		return Overview{}, err
	}
	return service.repository.Overview(ctx, session.User.ID, limit)
}
