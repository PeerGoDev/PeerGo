package main

import (
	"path/filepath"
	"testing"
)

func TestLoadSettingsStatusDoesNotRequireSourceDatabase(t *testing.T) {
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo")
	t.Setenv("PEERGO_LEGACY_RUN_ID", "cccccccc-2222-4333-8444-555555555555")
	t.Setenv("PEERGO_LEGACY_SNAPSHOT_SHA256", "70ee54c440fb17a09f14d09639886328375e678e2834937f340cbb0d679f4a9f")
	t.Setenv("PEERGO_LEGACY_SOURCE_DATABASE_URL", "")

	settings, err := loadSettings("status")
	if err != nil {
		t.Fatal(err)
	}
	if settings.SourceDatabaseURL != "" || settings.CoreDatabaseURL == "" {
		t.Fatalf("status settings = %+v", settings)
	}
}

func TestLoadSettingsInventoryStillRequiresDistinctSource(t *testing.T) {
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo")
	t.Setenv("PEERGO_LEGACY_RUN_ID", "cccccccc-2222-4333-8444-555555555555")
	t.Setenv("PEERGO_LEGACY_SNAPSHOT_SHA256", "70ee54c440fb17a09f14d09639886328375e678e2834937f340cbb0d679f4a9f")
	t.Setenv("PEERGO_LEGACY_SOURCE_DATABASE_URL", "")

	if _, err := loadSettings("inventory"); err == nil {
		t.Fatal("inventory settings accepted a missing source database")
	}
}

func TestLoadSettingsPreflightRequiresImmutableInputsAndVault(t *testing.T) {
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo")
	t.Setenv("PEERGO_LEGACY_SOURCE_DATABASE_URL", "postgres://source.example/ptyes")
	t.Setenv("PEERGO_VAULT_DATABASE_URL", "postgres://vault.example/peergo")
	t.Setenv("PEERGO_LEGACY_RUN_ID", "cccccccc-2222-4333-8444-555555555555")
	t.Setenv("PEERGO_LEGACY_SNAPSHOT_SHA256", "70ee54c440fb17a09f14d09639886328375e678e2834937f340cbb0d679f4a9f")
	t.Setenv("PEERGO_LEGACY_OCCURRED_AT", "2026-08-11T00:00:00Z")
	t.Setenv("PEERGO_LEGACY_DUMP_PATH", filepath.Join(t.TempDir(), "rousi.sql.gz"))
	t.Setenv("PEERGO_LEGACY_TORRENT_ROOT", filepath.Join(t.TempDir(), "torrents.zip"))
	t.Setenv("PEERGO_LEGACY_IMAGE_ROOT", filepath.Join(t.TempDir(), "uploads.zip"))
	t.Setenv("PEERGO_LEGACY_PREFLIGHT_OUTPUT", filepath.Join(t.TempDir(), "preflight.json"))

	settings, err := loadSettings("preflight")
	if err != nil {
		t.Fatal(err)
	}
	if settings.VaultDatabaseURL == "" || settings.DatabaseDumpPath == "" ||
		settings.TorrentRoot == "" || settings.ImageRoot == "" ||
		settings.PreflightOutput == "" || settings.OccurredAt.IsZero() {
		t.Fatalf("preflight settings = %+v", settings)
	}
}

func TestLoadSettingsAcceptanceRequiresBoundInputAndNewOutput(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo")
	t.Setenv("PEERGO_LEGACY_SOURCE_DATABASE_URL", "postgres://source.example/ptyes")
	t.Setenv("PEERGO_VAULT_DATABASE_URL", "postgres://vault.example/peergo")
	t.Setenv("PEERGO_LEGACY_RUN_ID", "cccccccc-2222-4333-8444-555555555555")
	t.Setenv("PEERGO_LEGACY_SNAPSHOT_SHA256", "70ee54c440fb17a09f14d09639886328375e678e2834937f340cbb0d679f4a9f")
	t.Setenv("PEERGO_LEGACY_OCCURRED_AT", "2026-08-11T00:00:00Z")
	t.Setenv("PEERGO_LEGACY_DUMP_PATH", filepath.Join(directory, "rousi.sql.gz"))
	t.Setenv("PEERGO_LEGACY_TORRENT_ROOT", filepath.Join(directory, "torrents.zip"))
	t.Setenv("PEERGO_LEGACY_IMAGE_ROOT", filepath.Join(directory, "uploads.zip"))
	t.Setenv("PEERGO_LEGACY_PREFLIGHT_MANIFEST", filepath.Join(directory, "preflight.json"))
	t.Setenv("PEERGO_LEGACY_ACCEPTANCE_OUTPUT", filepath.Join(directory, "acceptance.json"))

	settings, err := loadSettings("acceptance")
	if err != nil {
		t.Fatal(err)
	}
	if settings.PreflightManifest == "" || settings.AcceptanceOutput == "" ||
		settings.VaultDatabaseURL == "" || settings.OccurredAt.IsZero() {
		t.Fatalf("acceptance settings = %+v", settings)
	}

	t.Setenv("PEERGO_LEGACY_ACCEPTANCE_OUTPUT", settings.PreflightManifest)
	if _, err := loadSettings("acceptance"); err == nil {
		t.Fatal("acceptance settings allowed the output to overwrite preflight evidence")
	}
}
