package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadTrackerSnapshotBuilderAcceptsSeed(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = 0x71
	}
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo")
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_KEY_ID", "control-2026-08")
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_SIGNING_KEY_BASE64", base64.StdEncoding.EncodeToString(seed))
	directory := t.TempDir()
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_PATH", filepath.Join(directory, "control.snapshot"))
	t.Setenv("PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH", filepath.Join(directory, "subjects.snapshot"))
	t.Setenv("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH", filepath.Join(directory, "runtime-policy.snapshot"))
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_PUBLISH_INTERVAL", "30s")

	settings, err := LoadTrackerSnapshotBuilder()
	if err != nil {
		t.Fatal(err)
	}
	if settings.KeyID != "control-2026-08" || len(settings.SigningKey) != ed25519.PrivateKeySize ||
		!filepath.IsAbs(settings.SnapshotPath) || !filepath.IsAbs(settings.SubjectSnapshotPath) || !filepath.IsAbs(settings.RuntimePolicyPath) ||
		settings.PublishInterval != 30*time.Second {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestLoadTrackerSnapshotBuilderDefaultsToOneShotAndRejectsUnsafeInterval(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo")
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_KEY_ID", "active")
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_SIGNING_KEY_BASE64", base64.StdEncoding.EncodeToString(seed))
	directory := t.TempDir()
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_PATH", filepath.Join(directory, "control.snapshot"))
	t.Setenv("PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH", filepath.Join(directory, "subjects.snapshot"))
	t.Setenv("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH", filepath.Join(directory, "runtime-policy.snapshot"))

	settings, err := LoadTrackerSnapshotBuilder()
	if err != nil || settings.PublishInterval != 0 {
		t.Fatalf("one-shot settings = %+v, error = %v", settings, err)
	}
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_PUBLISH_INTERVAL", "5s")
	if _, err := LoadTrackerSnapshotBuilder(); err == nil || !strings.Contains(err.Error(), "between 10s and 1h") {
		t.Fatalf("unsafe interval error = %v", err)
	}
}

func TestLoadTrackerSnapshotBuilderRejectsMalformedKeyAndRelativePath(t *testing.T) {
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo")
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_KEY_ID", "active")
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_SIGNING_KEY_BASE64", "not-base64")
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_PATH", "control.snapshot")
	t.Setenv("PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH", "subjects.snapshot")
	t.Setenv("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH", "runtime-policy.snapshot")
	if _, err := LoadTrackerSnapshotBuilder(); err == nil || !strings.Contains(err.Error(), "standard padded Base64") {
		t.Fatalf("malformed key error = %v", err)
	}

	seed := make([]byte, ed25519.SeedSize)
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_SIGNING_KEY_BASE64", base64.StdEncoding.EncodeToString(seed))
	if _, err := LoadTrackerSnapshotBuilder(); err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("relative path error = %v", err)
	}
}
