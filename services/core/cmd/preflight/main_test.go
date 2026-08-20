package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestLoadSettingsRequiresExplicitPolicyChoicesInStrictMode(t *testing.T) {
	setDatabaseURLs(t)
	t.Setenv("PEERGO_PREFLIGHT_STRICT_POLICIES", "true")
	if _, err := loadSettings(); err == nil || !strings.Contains(err.Error(), "REGISTRATION_MODE") {
		t.Fatalf("loadSettings() error = %v", err)
	}

	t.Setenv("PEERGO_PREFLIGHT_REGISTRATION_MODE", "closed")
	if _, err := loadSettings(); err == nil || !strings.Contains(err.Error(), "NEWCOMER_STATE") {
		t.Fatalf("loadSettings() error = %v", err)
	}

	t.Setenv("PEERGO_PREFLIGHT_NEWCOMER_STATE", "disabled")
	settings, err := loadSettings()
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if !settings.StrictPolicies || !settings.RequireSettlementPolicy || !settings.RequireHNRPolicy || !settings.RequireEmailDelivery {
		t.Fatalf("strict settings = %+v", settings)
	}
}

func TestLoadSettingsAcceptsNonStrictInspection(t *testing.T) {
	setDatabaseURLs(t)
	settings, err := loadSettings()
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if settings.StrictPolicies || settings.ExpectedRegistrationMode != "" || settings.ExpectedNewcomerState != "any" || settings.RequireSettlementPolicy || settings.RequireHNRPolicy || settings.RequireEmailDelivery {
		t.Fatalf("inspection settings = %+v", settings)
	}
}

func TestEmailDeliveryCheckRequiresProductionRelayAndSecurePublicOrigins(t *testing.T) {
	responseBody := `{
		"generated_at":"2026-08-19T02:00:00Z",
		"delivery_mode":"https_relay",
		"verification_public_origin":"https://peergo.example",
		"password_recovery_public_origin":"https://peergo.example",
		"verification_ttl_seconds":1800,
		"password_recovery_ttl_seconds":1800,
		"cooldown_seconds":120,
		"templates":["peergo-email-verification-v1","peergo-password-recovery-v1"]
		,"stats":{}
	}`
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer "+strings.Repeat("v", 32) {
			t.Fatalf("authorization header = %q", request.Header.Get("Authorization"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(responseBody)),
			Request:    request,
		}, nil
	})}
	result := emailDeliveryCheck(context.Background(), client, "http://vault.internal:8081", strings.Repeat("v", 32))
	if result.Status != checkPass {
		t.Fatalf("emailDeliveryCheck() = %+v", result)
	}

	responseBody = strings.Replace(responseBody, "https_relay", "development_outbox", 1)
	result = emailDeliveryCheck(context.Background(), client, "http://vault.internal:8081", strings.Repeat("v", 32))
	if result.Status != checkFail || !strings.Contains(result.Detail, "HTTPS Relay") {
		t.Fatalf("emailDeliveryCheck() = %+v, want relay failure", result)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestRequiredDatabaseURLRejectsMissingDatabaseName(t *testing.T) {
	t.Setenv("TEST_DATABASE_URL", "postgres://database.example/")
	if _, err := requiredDatabaseURL("TEST_DATABASE_URL"); err == nil {
		t.Fatal("requiredDatabaseURL() error = nil")
	}
}

func setDatabaseURLs(t *testing.T) {
	t.Helper()
	for name, value := range map[string]string{
		"PEERGO_CORE_DATABASE_URL":    "postgres://core.example/peergo_core?sslmode=verify-full",
		"PEERGO_VAULT_DATABASE_URL":   "postgres://vault.example/peergo_vault?sslmode=verify-full",
		"PEERGO_TRACKER_DATABASE_URL": "postgres://tracker.example/peergo_tracker?sslmode=verify-full",
	} {
		t.Setenv(name, value)
	}
	t.Setenv("PEERGO_PREFLIGHT_STRICT_POLICIES", "")
	t.Setenv("PEERGO_PREFLIGHT_REGISTRATION_MODE", "")
	t.Setenv("PEERGO_PREFLIGHT_NEWCOMER_STATE", "")
	t.Setenv("PEERGO_PREFLIGHT_REQUIRE_SETTLEMENT_POLICY", "")
	t.Setenv("PEERGO_PREFLIGHT_REQUIRE_HNR_POLICY", "")
	t.Setenv("PEERGO_PREFLIGHT_REQUIRE_EMAIL_DELIVERY", "")
	t.Setenv("PEERGO_VAULT_URL", "http://vault.internal:8081")
	t.Setenv("PEERGO_VAULT_SERVICE_TOKEN", strings.Repeat("v", 32))
}
