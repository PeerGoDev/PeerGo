package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

// RequireMemberBrowsing protects the catalog and published content handlers.
// Other private operations retain their existing audience and permission checks.
func RequireMemberBrowsing(sessions IdentityService, spec *openapi3.T) generated.StrictMiddlewareFunc {
	operations := make(map[string]struct{})
	for _, path := range spec.Paths.Map() {
		for _, operation := range path.Operations() {
			permission, _ := operation.Extensions["x-permission"].(string)
			switch authz.Action(permission) {
			case authz.ActionTorrentRead, authz.ActionCategoryRead, authz.ActionAnnouncementRead, authz.ActionWikiPageRead:
				operations[strings.ToLower(operation.OperationID)] = struct{}{}
			}
		}
	}
	return func(next generated.StrictHandlerFunc, operationID string) generated.StrictHandlerFunc {
		if _, protected := operations[strings.ToLower(operationID)]; !protected {
			return next
		}
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			w.Header().Set("Cache-Control", "private, no-store")
			token := sessionTokenFromContext(ctx)
			if token != "" {
				_, err := sessions.CurrentSession(ctx, token)
				if err == nil {
					return next(ctx, w, r, request)
				}
				if !errors.Is(err, identity.ErrSessionNotFound) {
					return nil, err
				}
			}
			WriteProblem(w, r, http.StatusUnauthorized, "web_session_required", "请先登录", "登录后才能浏览站内内容。")
			return nil, nil
		}
	}
}
