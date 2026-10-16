package config

import (
	"testing"
	"time"
)

func TestLoadStorageCleanupUsesBoundedRetention(t *testing.T) {
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_CATCHUP_BUDGET", "10s")
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_TRACKER_DATABASE_URL", "postgres://peergo:secret@127.0.0.1:5434/peergo_tracker?sslmode=disable")
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_CLEANUP_INTERVAL", "1m")
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_TERMINAL_RETENTION", "3h")
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_SESSION_RETENTION", "12h")
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_DETAIL_RETENTION", "12h")
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_ANOMALY_RETENTION", "720h")
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_STARTUP_TIMEOUT", "10s")
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_BATCH_SIZE", "1000")

	settings, err := LoadStorageCleanup()
	if err != nil {
		t.Fatalf("LoadStorageCleanup() error = %v", err)
	}
	if settings.RunInterval != time.Minute || settings.TerminalRetention != 3*time.Hour ||
		settings.SessionRetention != 12*time.Hour || settings.DetailRetention != 12*time.Hour ||
		settings.AnomalyRetention != 30*24*time.Hour || settings.BatchSize != 1000 || settings.CatchupBudget != 10*time.Second {
		t.Fatalf("LoadStorageCleanup() = %+v", settings)
	}
}

func TestLoadStorageCleanupRejectsUnsafeDetailRetention(t *testing.T) {
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_TRACKER_DATABASE_URL", "postgres://peergo:secret@127.0.0.1:5434/peergo_tracker?sslmode=disable")
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_CLEANUP_INTERVAL", "1m")
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_TERMINAL_RETENTION", "3h")
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_SESSION_RETENTION", "12h")
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_DETAIL_RETENTION", "11h59m")
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_ANOMALY_RETENTION", "720h")
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_STARTUP_TIMEOUT", "10s")
	t.Setenv("PEERGO_SETTLEMENT_STORAGE_BATCH_SIZE", "1000")

	if _, err := LoadStorageCleanup(); err == nil {
		t.Fatal("LoadStorageCleanup() error = nil, want unsafe retention rejection")
	}
}

func TestStorageCatchupBudgetOptionalAndBounded(t *testing.T) {
	for _, value := range []string{"", "0s", "30s", "31s", "-1s", "invalid"} {
		t.Run(value, func(t *testing.T) {
			for k, v := range map[string]string{
				"PEERGO_ENV": "development", "PEERGO_TRACKER_DATABASE_URL": "postgres://peergo:secret@127.0.0.1:5434/peergo_tracker?sslmode=disable",
				"PEERGO_SETTLEMENT_STORAGE_CLEANUP_INTERVAL": "30s", "PEERGO_SETTLEMENT_STORAGE_TERMINAL_RETENTION": "3h",
				"PEERGO_SETTLEMENT_STORAGE_SESSION_RETENTION": "12h", "PEERGO_SETTLEMENT_STORAGE_DETAIL_RETENTION": "12h",
				"PEERGO_SETTLEMENT_STORAGE_ANOMALY_RETENTION": "720h", "PEERGO_SETTLEMENT_STORAGE_STARTUP_TIMEOUT": "10s",
				"PEERGO_SETTLEMENT_STORAGE_BATCH_SIZE": "5000", "PEERGO_SETTLEMENT_STORAGE_CATCHUP_BUDGET": value,
			} {
				t.Setenv(k, v)
			}
			settings, err := LoadStorageCleanup()
			valid := value == "" || value == "0s" || value == "30s"
			if (err == nil) != valid {
				t.Fatalf("budget=%q err=%v", value, err)
			}
			if err == nil && value != "30s" && settings.CatchupBudget != 0 {
				t.Fatalf("default=%v", settings.CatchupBudget)
			}
		})
	}
}
