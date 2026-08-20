package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadParsesRotatableTrustedKeys(t *testing.T) {
	first := make([]byte, ed25519.PublicKeySize)
	second := make([]byte, ed25519.PublicKeySize)
	second[0] = 1
	t.Setenv("PEERGO_ENV", "development")
	directory := t.TempDir()
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_PATH", filepath.Join(directory, "control.snapshot"))
	t.Setenv("PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH", filepath.Join(directory, "subjects.snapshot"))
	t.Setenv("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH", filepath.Join(directory, "runtime-policy.snapshot"))
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_TRUSTED_KEYS", "old="+base64.StdEncoding.EncodeToString(first)+",active="+base64.StdEncoding.EncodeToString(second))
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_MAX_AGE", "5m")
	t.Setenv("PEERGO_TRACKER_SUBJECT_SNAPSHOT_MAX_AGE", "30s")
	t.Setenv("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_MAX_AGE", "5m")
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_MAX_FUTURE_SKEW", "30s")
	t.Setenv("PEERGO_TRACKER_PASSKEY_LOOKUP_KEY", "tracker-passkey-lookup-key-test-2026")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.TrustedKeys) != 2 || settings.MaxAge != 5*time.Minute || settings.SubjectMaxAge != 30*time.Second ||
		settings.MaxFutureSkew != 30*time.Second || len(settings.PasskeyLookupKey) < 32 {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestLoadRejectsMalformedTrustedKey(t *testing.T) {
	t.Setenv("PEERGO_ENV", "development")
	directory := t.TempDir()
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_PATH", filepath.Join(directory, "control.snapshot"))
	t.Setenv("PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH", filepath.Join(directory, "subjects.snapshot"))
	t.Setenv("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH", filepath.Join(directory, "runtime-policy.snapshot"))
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_TRUSTED_KEYS", "active=short")
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_MAX_AGE", "5m")
	t.Setenv("PEERGO_TRACKER_SUBJECT_SNAPSHOT_MAX_AGE", "30s")
	t.Setenv("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_MAX_AGE", "5m")
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_MAX_FUTURE_SKEW", "30s")
	t.Setenv("PEERGO_TRACKER_PASSKEY_LOOKUP_KEY", "tracker-passkey-lookup-key-test-2026")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "32-byte Ed25519 public keys") {
		t.Fatalf("Load() error = %v", err)
	}
}
