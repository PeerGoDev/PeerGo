package traffic

import (
	"context"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

// MyHNR returns only the verified Web-session subject's Core projection. No
// caller-supplied user ID and no Tracker Ledger adapter exists on this path.
func (service *Service) MyHNR(ctx context.Context, cookieToken string, query HNRQuery) (HNRPage, error) {
	if query.Filter == "" {
		query.Filter = HNRFilterOpen
	}
	if query.Limit == 0 {
		query.Limit = DefaultHNRLimit
	}
	if !validHNRFilter(query.Filter) || query.Limit < 1 || query.Limit > MaximumHNRLimit ||
		(query.Cursor != nil && (query.Cursor.CompletedAt.IsZero() || query.Cursor.ObligationID == uuid.Nil)) {
		return HNRPage{}, ErrInput
	}
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return HNRPage{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionHNRReadSelf, service.now().UTC()); err != nil {
		return HNRPage{}, err
	}
	return service.repository.ListHNR(ctx, session.User.ID, query)
}
