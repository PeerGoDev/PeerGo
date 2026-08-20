// Package httpserver exposes Audit Sink's write-only, service-authenticated
// ingestion endpoint. No list, update or delete route exists in this process.
package httpserver

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/peergo/peergo/services/audit-sink/internal/journal"
)

type eventJournal interface {
	Append(string, []byte, time.Time) (bool, error)
	Ready() error
}

type envelopeMetadata struct {
	SchemaVersion string    `json:"schema_version"`
	EventType     string    `json:"event_type"`
	EventID       string    `json:"event_id"`
	OccurredAt    time.Time `json:"occurred_at"`
}

func New(store eventJournal, serviceToken string, now func() time.Time, logger *slog.Logger) http.Handler {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if err := store.Ready(); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.Handle("POST /internal/v1/audit/events", requireServiceToken(serviceToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"code": "json_required"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, journal.MaxPayloadBytes)
		payload, err := io.ReadAll(r.Body)
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"code": "payload_too_large"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_request"})
			return
		}

		idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if !contentDigestMatches(payload, r.Header.Get("X-PeerGo-Content-SHA256")) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "content_digest_mismatch"})
			return
		}
		var envelope envelopeMetadata
		if err := json.Unmarshal(payload, &envelope); err != nil ||
			envelope.EventID == "" || envelope.EventID != idempotencyKey ||
			envelope.EventType == "" || envelope.SchemaVersion == "" || envelope.OccurredAt.IsZero() {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_event_envelope"})
			return
		}

		created, err := store.Append(idempotencyKey, payload, now().UTC())
		if errors.Is(err, journal.ErrConflict) {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "event_id_conflict"})
			return
		}
		if errors.Is(err, journal.ErrInvalid) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_event"})
			return
		}
		if err != nil {
			// Payload and service token are deliberately excluded from logs.
			logger.ErrorContext(r.Context(), "append audit event failed", "event_id", idempotencyKey, "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "journal_unavailable"})
			return
		}
		if !created {
			writeJSON(w, http.StatusOK, map[string]string{"status": "already_recorded"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"status": "recorded"})
	})))
	return noStore(mux)
}

func requireServiceToken(expected string, next http.Handler) http.Handler {
	expectedHeader := []byte("Bearer " + expected)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual := []byte(r.Header.Get("Authorization"))
		if len(actual) != len(expectedHeader) || subtle.ConstantTimeCompare(actual, expectedHeader) != 1 {
			writeJSON(w, http.StatusForbidden, map[string]string{"code": "service_auth_failed"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func contentDigestMatches(payload []byte, encoded string) bool {
	expected, err := hex.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(expected) != sha256.Size {
		return false
	}
	actual := sha256.Sum256(payload)
	return subtle.ConstantTimeCompare(actual[:], expected) == 1
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
