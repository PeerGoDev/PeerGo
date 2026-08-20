package config

import "testing"

func TestLoadAuditWorkerValidatesProductionTransport(t *testing.T) {
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo")
	t.Setenv("PEERGO_AUDIT_SINK_URL", "http://127.0.0.1:8082")
	t.Setenv("PEERGO_AUDIT_SERVICE_TOKEN", "peergo-test-audit-service-token-2026")

	settings, err := LoadAuditWorker()
	if err != nil {
		t.Fatalf("LoadAuditWorker() error = %v", err)
	}
	if settings.AuditSinkURL != "http://127.0.0.1:8082" || settings.DatabaseURL == "" {
		t.Fatalf("settings = %+v", settings)
	}

	t.Setenv("PEERGO_ENV", "production")
	if _, err := LoadAuditWorker(); err == nil {
		t.Fatal("LoadAuditWorker(production HTTP) error = nil")
	}
	t.Setenv("PEERGO_AUDIT_SINK_URL", "https://audit.internal.example")
	if _, err := LoadAuditWorker(); err != nil {
		t.Fatalf("LoadAuditWorker(production HTTPS) error = %v", err)
	}
}
