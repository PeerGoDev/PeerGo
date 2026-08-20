package catalog

import (
	"context"
	"time"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

// catalogStaffAuthorizer is shared by the catalog-owned administration
// sections. It avoids each settings or business-object service inventing a
// subtly different interpretation of a verified staff actor.
type catalogStaffAuthorizer = authz.Authorizer

func authorizeCatalogStaff(
	ctx context.Context,
	authorizer catalogStaffAuthorizer,
	actor authz.StaffActor,
	action authz.Action,
	now time.Time,
	purpose string,
) (authz.Decision, error) {
	return authz.AuthorizeStaffAction(ctx, authorizer, actor, action, authz.SiteScope(), now, purpose)
}
