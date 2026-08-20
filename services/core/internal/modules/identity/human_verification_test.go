package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestTurnstileVerifierValidatesSingleUseActionServerSide(t *testing.T) {
	var received url.Values
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatalf("unexpected Siteverify request: %s %q", request.Method, request.Header.Get("Content-Type"))
		}
		if err := request.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		received = request.PostForm
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"success":true,"action":"registration"}`))
	}))
	defer server.Close()

	verifier, err := newTurnstileVerifier("server-secret", server.URL, server.Client())
	if err != nil {
		t.Fatalf("newTurnstileVerifier() error = %v", err)
	}
	if err := verifier.Verify(context.Background(), HumanVerificationFlowRegistration, "browser-token"); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if received.Get("secret") != "server-secret" || received.Get("response") != "browser-token" {
		t.Fatalf("unexpected Siteverify form: %#v", received)
	}
}

func TestTurnstileVerifierRejectsActionMismatchAndProviderFailure(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		expected error
	}{
		{name: "action mismatch", status: http.StatusOK, body: `{"success":true,"action":"login"}`, expected: ErrHumanVerificationFailed},
		{name: "provider rejection", status: http.StatusOK, body: `{"success":false,"error-codes":["timeout-or-duplicate"]}`, expected: ErrHumanVerificationFailed},
		{name: "provider unavailable", status: http.StatusBadGateway, body: `{}`, expected: ErrHumanVerificationUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			verifier, err := newTurnstileVerifier("server-secret", server.URL, server.Client())
			if err != nil {
				t.Fatalf("newTurnstileVerifier() error = %v", err)
			}
			if err := verifier.Verify(context.Background(), HumanVerificationFlowRegistration, "browser-token"); !errors.Is(err, test.expected) {
				t.Fatalf("Verify() error = %v, want %v", err, test.expected)
			}
		})
	}
}

func TestTurnstileVerifierRequiresBoundedToken(t *testing.T) {
	verifier, err := newTurnstileVerifier("server-secret", "https://example.test/siteverify", http.DefaultClient)
	if err != nil {
		t.Fatalf("newTurnstileVerifier() error = %v", err)
	}
	if err := verifier.Verify(context.Background(), HumanVerificationFlowLogin, ""); !errors.Is(err, ErrHumanVerificationRequired) {
		t.Fatalf("empty token error = %v", err)
	}
	if err := verifier.Verify(context.Background(), HumanVerificationFlowLogin, string(make([]byte, 2049))); !errors.Is(err, ErrHumanVerificationFailed) {
		t.Fatalf("oversized token error = %v", err)
	}
}
