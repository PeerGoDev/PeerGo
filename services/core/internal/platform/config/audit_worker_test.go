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

func TestLoadAuditWorkerAllowsFixedSingleServerSink(t *testing.T) {
	t.Setenv("PEERGO_ENV", "production")
	t.Setenv("PEERGO_DEPLOYMENT_MODE", "single-server")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://peergo_core:secret@postgresql:5432/peergo_core?sslmode=disable")
	t.Setenv("PEERGO_AUDIT_SINK_URL", "http://audit-sink:8082")
	t.Setenv("PEERGO_AUDIT_SERVICE_TOKEN", "peergo-test-audit-service-token-2026")
	if _, err := LoadAuditWorker(); err != nil {
		t.Fatalf("single-server audit sink rejected: %v", err)
	}
}
