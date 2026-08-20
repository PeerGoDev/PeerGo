package httpserver

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/peergo/peergo/services/email-relay/internal/delivery"
)

const relayToken = "peergo-email-relay-service-token-test"

type senderRecorder struct{ message delivery.Message }

func (sender *senderRecorder) Send(_ context.Context, message delivery.Message) error {
	sender.message = message
	return nil
}

func TestRelayAuthenticatesRendersAndDelivers(t *testing.T) {
	t.Parallel()
	renderer, _ := delivery.NewRenderer("PeerGo")
	sender := &senderRecorder{}
	handler, err := New(renderer, sender, relayToken, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"template":"peergo-delivery-test-v1","recipient":"member@example.test"}`)
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/deliveries/transactional", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+relayToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || sender.message.Recipient != "member@example.test" || sender.message.Subject == "" {
		t.Fatalf("status=%d message=%+v body=%s", response.Code, sender.message, response.Body.String())
	}

	unauthorized := httptest.NewRequest(http.MethodPost, "/internal/v1/deliveries/transactional", bytes.NewReader(body))
	unauthorized.Header.Set("Content-Type", "application/json")
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusForbidden {
		t.Fatalf("unauthorized status = %d", unauthorizedResponse.Code)
	}
}
