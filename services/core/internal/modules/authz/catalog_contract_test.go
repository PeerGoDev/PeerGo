package authz_test

import (
	"net/http"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	generated "github.com/peergo/peergo/services/core/internal/generated/api"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

func TestEveryOpenAPIPermissionUsesTypedCatalog(t *testing.T) {
	t.Parallel()

	spec, err := generated.GetSwagger()
	if err != nil {
		t.Fatalf("GetSwagger() error = %v", err)
	}
	for path, item := range spec.Paths.Map() {
		operations := map[string]*openapi3.Operation{
			http.MethodGet:    item.Get,
			http.MethodPost:   item.Post,
			http.MethodPut:    item.Put,
			http.MethodPatch:  item.Patch,
			http.MethodDelete: item.Delete,
		}
		for method, operation := range operations {
			if operation == nil {
				continue
			}
			value, exists := operation.Extensions["x-permission"]
			if !exists {
				t.Fatalf("%s %s has no x-permission", method, path)
			}
			permission, ok := value.(string)
			if !ok {
				t.Fatalf("%s %s x-permission has type %T", method, path, value)
			}
			definition, known := authz.Lookup(authz.Action(permission))
			if !known {
				t.Fatalf("%s %s uses untyped permission %q", method, path, permission)
			}
			audienceValue, exists := operation.Extensions["x-credential-audience"]
			if !exists {
				t.Fatalf("%s %s has no x-credential-audience", method, path)
			}
			audience, ok := audienceValue.(string)
			if !ok || authz.CredentialAudience(audience) != definition.CredentialAudience {
				t.Fatalf(
					"%s %s credential audience = %v, want %q from permission %q",
					method,
					path,
					audienceValue,
					definition.CredentialAudience,
					permission,
				)
			}
			conditionalPermissions, exists := operation.Extensions["x-action-permissions"]
			if !exists {
				continue
			}
			permissionMap, ok := conditionalPermissions.(map[string]any)
			if !ok || len(permissionMap) == 0 {
				t.Fatalf("%s %s x-action-permissions has type %T or is empty", method, path, conditionalPermissions)
			}
			for action, rawPermission := range permissionMap {
				conditionalPermission, ok := rawPermission.(string)
				if !ok {
					t.Fatalf("%s %s action %q permission has type %T", method, path, action, rawPermission)
				}
				conditionalDefinition, known := authz.Lookup(authz.Action(conditionalPermission))
				if !known {
					t.Fatalf("%s %s action %q uses untyped permission %q", method, path, action, conditionalPermission)
				}
				if authz.CredentialAudience(audience) != conditionalDefinition.CredentialAudience {
					t.Fatalf(
						"%s %s action %q credential audience = %q, want %q from permission %q",
						method,
						path,
						action,
						audience,
						conditionalDefinition.CredentialAudience,
						conditionalPermission,
					)
				}
			}
		}
	}
}

func TestTypedPermissionCatalogIsStrictlySorted(t *testing.T) {
	t.Parallel()

	catalog := authz.Catalog()
	for index := 1; index < len(catalog); index++ {
		previous := catalog[index-1].Action
		current := catalog[index].Action
		if previous >= current {
			t.Fatalf("permission catalog is not strictly sorted: %q before %q", previous, current)
		}
	}
}
