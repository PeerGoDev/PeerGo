package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadImageDerivativeWorkerUsesActiveStorageAndPrivateTempRoot(t *testing.T) {
	setValidCoreEnvironment(t)
	tempDir := t.TempDir()
	t.Setenv("PEERGO_IMAGE_DERIVATIVE_TEMP_DIR", tempDir)
	t.Setenv("PEERGO_IMAGE_DERIVATIVE_POLL_INTERVAL", "750ms")
	t.Setenv("PEERGO_IMAGE_DERIVATIVE_LEASE_DURATION", "3m")

	settings, err := LoadImageDerivativeWorker()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Store.BackendID != "local-primary" || settings.TempDir != filepath.Clean(tempDir) ||
		settings.PollInterval != 750*time.Millisecond || settings.LeaseDuration != 3*time.Minute {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestLoadImageDerivativeWorkerRejectsRelativeTempRoot(t *testing.T) {
	setValidCoreEnvironment(t)
	t.Setenv("PEERGO_IMAGE_DERIVATIVE_TEMP_DIR", "relative/images")

	_, err := LoadImageDerivativeWorker()
	if err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("error = %v", err)
	}
}
