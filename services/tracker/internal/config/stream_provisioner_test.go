package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadStreamProvisionerBuildsImmutableLimitsStream(t *testing.T) {
	values := serverTestValues(t.TempDir())
	for name, value := range values {
		t.Setenv(name, value)
	}
	t.Setenv("PEERGO_TRACKER_NATS_PROVISION_CREDENTIALS_FILE", "")
	t.Setenv("PEERGO_TRACKER_ANNOUNCE_STREAM_PROVISION_TIMEOUT", "10s")
	t.Setenv("PEERGO_TRACKER_ANNOUNCE_STREAM_MAX_BYTES", "1073741824")
	t.Setenv("PEERGO_TRACKER_ANNOUNCE_STREAM_MAX_AGE", "168h")
	t.Setenv("PEERGO_TRACKER_ANNOUNCE_STREAM_DUPLICATE_WINDOW", "10m")
	t.Setenv("PEERGO_TRACKER_ANNOUNCE_STREAM_REPLICAS", "1")

	settings, err := LoadStreamProvisioner()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Timeout != 10*time.Second || settings.Stream.MaxBytes != 1<<30 ||
		settings.Stream.MaxAge != 7*24*time.Hour || settings.Stream.Replicas != 1 ||
		!settings.Stream.DenyDelete || !settings.Stream.DenyPurge || settings.Stream.Metadata["peergo.schema"] != "tracker.announce.v1" {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestLoadStreamProvisionerRequiresThreeProductionReplicas(t *testing.T) {
	values := serverTestValues(t.TempDir())
	values["PEERGO_ENV"] = "production"
	values["PEERGO_TRACKER_NATS_URLS"] = "tls://nats.internal:4222"
	for name, value := range values {
		t.Setenv(name, value)
	}
	t.Setenv("PEERGO_TRACKER_NATS_PROVISION_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "operator.creds"))
	t.Setenv("PEERGO_TRACKER_ANNOUNCE_STREAM_PROVISION_TIMEOUT", "10s")
	t.Setenv("PEERGO_TRACKER_ANNOUNCE_STREAM_MAX_BYTES", "1073741824")
	t.Setenv("PEERGO_TRACKER_ANNOUNCE_STREAM_MAX_AGE", "168h")
	t.Setenv("PEERGO_TRACKER_ANNOUNCE_STREAM_DUPLICATE_WINDOW", "10m")
	t.Setenv("PEERGO_TRACKER_ANNOUNCE_STREAM_REPLICAS", "1")
	if _, err := LoadStreamProvisioner(); err == nil || !strings.Contains(err.Error(), "at least 3") {
		t.Fatalf("LoadStreamProvisioner() error = %v", err)
	}
}
