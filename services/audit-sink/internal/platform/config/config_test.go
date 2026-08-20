package config

import (
	"path/filepath"
	"testing"
)

func TestLoadRequiresPrivateJournalContractInputs(t *testing.T) {
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_AUDIT_JOURNAL_PATH", filepath.Join(t.TempDir(), "events.jsonl"))
	t.Setenv("PEERGO_AUDIT_SERVICE_TOKEN", "peergo-test-audit-service-token-2026")

	settings, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if settings.Address != ":8082" || !filepath.IsAbs(settings.JournalPath) {
		t.Fatalf("settings = %+v", settings)
	}

	t.Setenv("PEERGO_AUDIT_JOURNAL_PATH", "relative/events.jsonl")
	if _, err := Load(); err == nil {
		t.Fatal("Load(relative path) error = nil")
	}
}
