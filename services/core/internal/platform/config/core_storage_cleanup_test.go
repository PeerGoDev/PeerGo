package config

import (
	"testing"
	"time"
)

func TestLoadCoreStorageCleanupUsesTwelveHourBoundary(t *testing.T) {
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo")
	t.Setenv("PEERGO_CORE_STORAGE_CLEANUP_INTERVAL", "15s")
	t.Setenv("PEERGO_CORE_STORAGE_DETAIL_RETENTION", "12h")
	t.Setenv("PEERGO_CORE_STORAGE_HISTORY_RETENTION", "720h")
	t.Setenv("PEERGO_CORE_STORAGE_BATCH_SIZE", "10000")
	t.Setenv("PEERGO_CORE_STORAGE_STARTUP_TIMEOUT", "15s")
	settings, err := LoadCoreStorageCleanup()
	if err != nil {
		t.Fatalf("LoadCoreStorageCleanup() error = %v", err)
	}
	if settings.RunInterval != 15*time.Second || settings.DetailRetention != 12*time.Hour ||
		settings.HistoryRetention != 30*24*time.Hour ||
		settings.BatchSize != 10_000 || settings.StartupTimeout != 15*time.Second {
		t.Fatalf("LoadCoreStorageCleanup() = %+v", settings)
	}
}

func TestLoadCoreStorageCleanupRejectsShortRetention(t *testing.T) {
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo")
	t.Setenv("PEERGO_CORE_STORAGE_CLEANUP_INTERVAL", "15s")
	t.Setenv("PEERGO_CORE_STORAGE_DETAIL_RETENTION", "11h59m")
	t.Setenv("PEERGO_CORE_STORAGE_HISTORY_RETENTION", "720h")
	t.Setenv("PEERGO_CORE_STORAGE_BATCH_SIZE", "10000")
	t.Setenv("PEERGO_CORE_STORAGE_STARTUP_TIMEOUT", "15s")
	if _, err := LoadCoreStorageCleanup(); err == nil {
		t.Fatal("LoadCoreStorageCleanup() error = nil, want unsafe retention rejection")
	}
}
