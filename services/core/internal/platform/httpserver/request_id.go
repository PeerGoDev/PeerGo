package httpserver

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

// opaqueRequestID gives every request a server-generated correlation ID.
//
// Chi's default middleware prefixes IDs with os.Hostname(), which is useful in
// private logs but leaks deployment topology when the same value is returned in
// a public Problem response. It also accepts X-Request-Id verbatim. PeerGo uses
// a random UUID instead so public IDs reveal neither the host nor a predictable
// process counter, and an external caller cannot forge the value used in logs.
func opaqueRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), middleware.RequestIDKey, uuid.NewString())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
