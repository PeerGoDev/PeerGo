package config

import "testing"

func TestLoadRequiresAuthenticatedTLSMailConfiguration(t *testing.T) {
	t.Setenv("PEERGO_ENV", "production")
	t.Setenv("PEERGO_EMAIL_RELAY_SERVICE_TOKEN", "peergo-email-relay-service-token-test")
	t.Setenv("PEERGO_EMAIL_SITE_NAME", "PeerGo")
	t.Setenv("PEERGO_SMTP_HOST", "smtp.example.test")
	t.Setenv("PEERGO_SMTP_PORT", "587")
	t.Setenv("PEERGO_SMTP_USERNAME", "mailer")
	t.Setenv("PEERGO_SMTP_PASSWORD", "secret")
	t.Setenv("PEERGO_SMTP_FROM_ADDRESS", "noreply@example.test")
	t.Setenv("PEERGO_SMTP_TLS_MODE", "starttls")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Address != ":8086" || settings.SMTP.Port != 587 || settings.SMTP.FromName != "PeerGo" || settings.SMTP.Timeout.String() != "10s" {
		t.Fatalf("settings = %+v", settings)
	}

	t.Setenv("PEERGO_SMTP_TLS_MODE", "plaintext")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted plaintext SMTP")
	}
}
