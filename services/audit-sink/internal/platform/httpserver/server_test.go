package httpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/peergo/peergo/services/audit-sink/internal/journal"
)

const testServiceToken = "peergo-test-audit-service-token-2026"

type memoryJournal struct {
	created   bool
	appendErr error
	eventID   string
	payload   []byte
}

func (store *memoryJournal) Append(eventID string, payload []byte, _ time.Time) (bool, error) {
	store.eventID = eventID
	store.payload = append([]byte(nil), payload...)
	return store.created, store.appendErr
}

func (*memoryJournal) Ready() error { return nil }

func TestAuditIngestionAuthenticatesValidatesAndAppends(t *testing.T) {
	t.Parallel()

	eventID := "0198f20a-6da8-7e51-9c64-555555555551"
	payload := []byte(`{"schema_version":"1.0.0","event_type":"authz.decision.recorded","event_id":"` + eventID + `","occurred_at":"2026-08-05T12:00:00Z"}`)
	digest := sha256.Sum256(payload)
	now := time.Date(2026, time.August, 5, 12, 1, 0, 0, time.UTC)

	tests := []struct {
		name       string
		store      *memoryJournal
		token      string
		eventID    string
		digest     string
		wantStatus int
	}{
		{name: "created", store: &memoryJournal{created: true}, token: testServiceToken, eventID: eventID, digest: hex.EncodeToString(digest[:]), wantStatus: http.StatusCreated},
		{name: "idempotent replay", store: &memoryJournal{}, token: testServiceToken, eventID: eventID, digest: hex.EncodeToString(digest[:]), wantStatus: http.StatusOK},
		{name: "service auth required", store: &memoryJournal{}, eventID: eventID, digest: hex.EncodeToString(digest[:]), wantStatus: http.StatusForbidden},
		{name: "digest mismatch", store: &memoryJournal{}, token: testServiceToken, eventID: eventID, digest: hex.EncodeToString(make([]byte, sha256.Size)), wantStatus: http.StatusBadRequest},
		{name: "envelope id mismatch", store: &memoryJournal{}, token: testServiceToken, eventID: "0198f20a-6da8-7e51-9c64-555555555552", digest: hex.EncodeToString(digest[:]), wantStatus: http.StatusBadRequest},
		{name: "event id conflict", store: &memoryJournal{appendErr: journal.ErrConflict}, token: testServiceToken, eventID: eventID, digest: hex.EncodeToString(digest[:]), wantStatus: http.StatusConflict},
		{name: "journal unavailable", store: &memoryJournal{appendErr: errors.New("disk unavailable")}, token: testServiceToken, eventID: eventID, digest: hex.EncodeToString(digest[:]), wantStatus: http.StatusServiceUnavailable},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := New(test.store, testServiceToken, func() time.Time { return now }, slog.New(slog.NewTextHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodPost, "/internal/v1/audit/events", bytes.NewReader(payload))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+test.token)
			request.Header.Set("Idempotency-Key", test.eventID)
			request.Header.Set("X-PeerGo-Content-SHA256", test.digest)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", response.Code, response.Body.String(), test.wantStatus)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
			}
			if test.wantStatus == http.StatusCreated && (test.store.eventID != eventID || !bytes.Equal(test.store.payload, payload)) {
				t.Fatalf("append = (%q, %s), want original event", test.store.eventID, test.store.payload)
			}
		})
	}
}
