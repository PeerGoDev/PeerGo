package auditsink

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/audit"
)

const testServiceToken = "peergo-test-audit-service-token-2026"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestClientDeliversExactPayloadAndIdempotencyMetadata(t *testing.T) {
	t.Parallel()

	eventID := uuid.MustParse("0198f20a-6da8-7e51-9c64-555555555555")
	payload := []byte(`{"event_id":"0198f20a-6da8-7e51-9c64-555555555555"}`)
	digest := sha256.Sum256(payload)
	client, err := NewClient("https://audit.internal.example", testServiceToken, time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/internal/v1/audit/events" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+testServiceToken || r.Header.Get("Idempotency-Key") != eventID.String() || r.Header.Get("X-PeerGo-Content-SHA256") != hex.EncodeToString(digest[:]) {
			t.Errorf("headers = %v", r.Header)
		}
		var received json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("Decode() error = %v", err)
		}
		if !bytes.Equal(received, payload) {
			t.Errorf("payload = %s, want %s", received, payload)
		}
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(`{"status":"recorded"}`)), Header: make(http.Header)}, nil
	})
	if err := client.Append(context.Background(), audit.Event{ID: eventID, Payload: payload, PayloadSHA256: digest}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
}

func TestClientReturnsBoundedSinkErrorCode(t *testing.T) {
	t.Parallel()

	client, err := NewClient("https://audit.internal.example", testServiceToken, time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusConflict,
			Body:       io.NopCloser(strings.NewReader(`{"code":"event_id_conflict","ignored":"payload is not echoed"}`)),
			Header:     make(http.Header),
		}, nil
	})
	payload := []byte(`{}`)
	digest := sha256.Sum256(payload)
	err = client.Append(context.Background(), audit.Event{ID: uuid.New(), Payload: payload, PayloadSHA256: digest})
	if err == nil || err.Error() != "audit sink rejected event: status=409 code=event_id_conflict" {
		t.Fatalf("Append() error = %v", err)
	}
}
