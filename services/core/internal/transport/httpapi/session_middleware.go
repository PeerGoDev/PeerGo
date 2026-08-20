package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

type webSessionTokenContextKey struct{}
type staffSessionTokenContextKey struct{}

// CaptureSessionCookies copies each credential audience into a distinct,
// private context key. Admin handlers select only the staff value; they never
// merge authority from the ordinary cookie which browsers also send there.
func CaptureSessionCookies(webCookieName, staffCookieName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(webCookieName)
			if err == nil && cookie.Value != "" {
				r = r.WithContext(context.WithValue(r.Context(), webSessionTokenContextKey{}, cookie.Value))
			}
			staffCookie, err := r.Cookie(staffCookieName)
			if err == nil && staffCookie.Value != "" {
				r = r.WithContext(context.WithValue(r.Context(), staffSessionTokenContextKey{}, staffCookie.Value))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// EnforceSameOrigin rejects browser writes unless Origin, or a Referer fallback,
// exactly matches a configured Web origin. CSRF tokens remain required as a
// second independent check for authenticated writes.
func EnforceSameOrigin(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[origin] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			origin := requestOrigin(r)
			if _, ok := allowed[origin]; !ok {
				WriteProblem(w, r, http.StatusForbidden, "origin_not_allowed", "请求来源无效", "写请求必须来自受信任的同源页面。")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// PrivateResponseHeaders prevents browser/proxy caches from retaining CSRF,
// session profile or authorization capability responses.
func PrivateResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/session" || r.URL.Path == "/api/v1/me/capabilities" ||
			r.URL.Path == "/api/v1/me/avatar" ||
			r.URL.Path == "/api/v1/me/profile" || r.URL.Path == "/api/v1/me/security" || strings.HasPrefix(r.URL.Path, "/api/v1/me/download-restriction") || r.URL.Path == "/api/v1/me/traffic" || r.URL.Path == "/api/v1/me/hit-and-runs" ||
			strings.HasPrefix(r.URL.Path, "/api/v1/me/torrent-submissions") ||
			strings.HasPrefix(r.URL.Path, "/api/v1/me/torrent-bookmark") ||
			strings.HasPrefix(r.URL.Path, "/api/v1/me/rss-subscriptions") ||
			strings.HasPrefix(r.URL.Path, "/api/v1/me/totp") ||
			strings.HasPrefix(r.URL.Path, "/api/v1/staff/elevation") || r.URL.Path == "/api/v1/admin/session" ||
			strings.HasPrefix(r.URL.Path, "/api/v1/admin/") ||
			(r.Method == http.MethodPost && r.URL.Path == "/api/v1/torrents") ||
			(r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/torrents/") && strings.HasSuffix(r.URL.Path, "/comments")) ||
			(!isSafeMethod(r.Method) && strings.HasPrefix(r.URL.Path, "/api/v1/comments/")) ||
			(r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/torrents/") && strings.HasSuffix(r.URL.Path, "/download")) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("X-Content-Type-Options", "nosniff")
		}
		// A private RSS token lives in the URL so error responses need the same
		// cache and referrer protection as successful feed/download responses.
		// Successful generated responses may deliberately narrow Cache-Control
		// to private revalidation, while keeping no-referrer in every case.
		if strings.HasPrefix(r.URL.Path, "/rss/") {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Content-Type-Options", "nosniff")
		}
		next.ServeHTTP(w, r)
	})
}

func sessionTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(webSessionTokenContextKey{}).(string)
	return token
}

func staffSessionTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(staffSessionTokenContextKey{}).(string)
	return token
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func requestOrigin(r *http.Request) string {
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" && origin != "null" {
		parsed, err := url.Parse(origin)
		if err == nil && parsed.Scheme != "" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && (parsed.Path == "" || parsed.Path == "/") {
			return parsed.Scheme + "://" + parsed.Host
		}
		return ""
	}
	referer, err := url.Parse(strings.TrimSpace(r.Referer()))
	if err != nil || referer.Scheme == "" || referer.Host == "" || referer.User != nil {
		return ""
	}
	return referer.Scheme + "://" + referer.Host
}
