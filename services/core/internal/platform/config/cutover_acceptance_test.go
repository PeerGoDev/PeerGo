package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadCutoverAcceptanceUsesOnlyVerificationMaterial(t *testing.T) {
	publicKey := make([]byte, ed25519.PublicKeySize)
	for index := range publicKey {
		publicKey[index] = 0x44
	}
	directory := t.TempDir()
	t.Setenv("PEERGO_ENV", "production")
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_PATH", filepath.Join(directory, "control.snapshot"))
	t.Setenv("PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH", filepath.Join(directory, "subjects.snapshot"))
	t.Setenv("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH", filepath.Join(directory, "runtime.snapshot"))
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_TRUSTED_KEYS", "active="+base64.StdEncoding.EncodeToString(publicKey))
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_MAX_AGE", "15m")
	t.Setenv("PEERGO_TRACKER_SUBJECT_SNAPSHOT_MAX_AGE", "2m")
	t.Setenv("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_MAX_AGE", "15m")
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_MAX_FUTURE_SKEW", "30s")

	settings, err := LoadCutoverAcceptance()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Environment != "production" || len(settings.TrustedKeys) != 1 ||
		settings.SnapshotMaxAge != 15*time.Minute || settings.SubjectMaxAge != 2*time.Minute ||
		settings.RuntimePolicyMaxAge != 15*time.Minute ||
		settings.MaxFutureSkew != 30*time.Second {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestLoadCutoverAcceptanceRejectsSharedPathsAndUnsafeDurations(t *testing.T) {
	publicKey := base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))
	path := filepath.Join(t.TempDir(), "snapshot")
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_PATH", path)
	t.Setenv("PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH", path)
	t.Setenv("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH", filepath.Join(t.TempDir(), "runtime.snapshot"))
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_TRUSTED_KEYS", "active="+publicKey)
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_MAX_AGE", "15m")
	t.Setenv("PEERGO_TRACKER_SUBJECT_SNAPSHOT_MAX_AGE", "2m")
	t.Setenv("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_MAX_AGE", "2m")
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_MAX_FUTURE_SKEW", "30s")
	if _, err := LoadCutoverAcceptance(); err == nil {
		t.Fatal("acceptance configuration allowed one path for two snapshot schemas")
	}

	t.Setenv("PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH", filepath.Join(t.TempDir(), "subjects.snapshot"))
	t.Setenv("PEERGO_TRACKER_SNAPSHOT_MAX_AGE", "2h")
	if _, err := LoadCutoverAcceptance(); err == nil {
		t.Fatal("acceptance configuration allowed an excessive snapshot age")
	}

	t.Setenv("PEERGO_TRACKER_SNAPSHOT_MAX_AGE", "15m")
	t.Setenv("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_MAX_AGE", "2h")
	if _, err := LoadCutoverAcceptance(); err == nil {
		t.Fatal("acceptance configuration allowed an excessive runtime policy snapshot age")
	}
}
