package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/peergo/peergo/services/core/internal/platform/audittext"
	"github.com/peergo/peergo/services/core/internal/transport/httpapi"
)

const (
	maximumAutomaticCommandTextBodyBytes = 32 << 20
)

// fillAutomaticCommandText runs before OpenAPI validation so blank or very
// short audit fields become stable, human-readable evidence instead of forcing
// every small settings change to invent filler text. User text is preserved;
// only surrounding whitespace is normalized and short text receives context.
func fillAutomaticCommandText(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || !commandTextMethod(r.Method) || !isJSONMediaType(r.Header.Get("Content-Type")) {
			next.ServeHTTP(w, r)
			return
		}

		originalLength := r.ContentLength
		raw, err := io.ReadAll(io.LimitReader(r.Body, maximumAutomaticCommandTextBodyBytes+1))
		if err != nil {
			restoreUnreadBody(r, raw, originalLength)
			next.ServeHTTP(w, r)
			return
		}
		if len(raw) > maximumAutomaticCommandTextBodyBytes {
			httpapi.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "request_body_too_large", "请求内容过大", "请求正文超过当前接口允许的大小。")
			return
		}

		var payload any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			restoreJSONBody(r, raw)
			next.ServeHTTP(w, r)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			restoreJSONBody(r, raw)
			next.ServeHTTP(w, r)
			return
		}
		if !normalizeAutomaticCommandText(payload) {
			restoreJSONBody(r, raw)
			next.ServeHTTP(w, r)
			return
		}
		normalized, err := json.Marshal(payload)
		if err != nil {
			restoreJSONBody(r, raw)
			next.ServeHTTP(w, r)
			return
		}
		restoreJSONBody(r, normalized)
		next.ServeHTTP(w, r)
	})
}

func normalizeAutomaticCommandText(value any) bool {
	changed := false
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if text, ok := item.(string); ok {
				if normalized, known := audittext.NormalizeField(key, text); known {
					if normalized != text {
						typed[key] = normalized
						changed = true
					}
					continue
				}
			}
			changed = normalizeAutomaticCommandText(item) || changed
		}
	case []any:
		for _, item := range typed {
			changed = normalizeAutomaticCommandText(item) || changed
		}
	}
	return changed
}

func commandTextMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch
}

func isJSONMediaType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(raw)
	return err == nil && (mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"))
}

func restoreJSONBody(r *http.Request, raw []byte) {
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	r.Header.Set("Content-Length", strconv.Itoa(len(raw)))
}

func restoreUnreadBody(r *http.Request, prefix []byte, originalLength int64) {
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(prefix), r.Body))
	r.ContentLength = originalLength
	if originalLength >= 0 {
		r.Header.Set("Content-Length", strconv.FormatInt(originalLength, 10))
	}
}
