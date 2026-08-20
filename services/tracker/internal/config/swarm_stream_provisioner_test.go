package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
)

func TestLoadSwarmStreamProvisionerBuildsReplaceableSnapshotStream(t *testing.T) {
	values := serverTestValues(t.TempDir())
	for name, value := range values {
		t.Setenv(name, value)
	}
	t.Setenv("PEERGO_TRACKER_NATS_PROVISION_CREDENTIALS_FILE", "")
	t.Setenv("PEERGO_TRACKER_SWARM_STREAM_PROVISION_TIMEOUT", "10s")
	t.Setenv("PEERGO_TRACKER_SWARM_STREAM_MAX_BYTES", "1073741824")
	t.Setenv("PEERGO_TRACKER_SWARM_STREAM_MAX_AGE", "24h")
	t.Setenv("PEERGO_TRACKER_SWARM_STREAM_DUPLICATE_WINDOW", "10m")
	t.Setenv("PEERGO_TRACKER_SWARM_STREAM_REPLICAS", "1")

	settings, err := LoadSwarmStreamProvisioner()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Timeout != 10*time.Second || settings.Stream.Name != trackerswarmv1.DefaultStream ||
		settings.Stream.Discard != jetstream.DiscardOld || settings.Stream.Compression != jetstream.S2Compression ||
		settings.Stream.MaxBytes != 1<<30 || settings.Stream.MaxAge != 24*time.Hour || settings.Stream.Replicas != 1 ||
		!settings.Stream.DenyDelete || !settings.Stream.DenyPurge ||
		settings.Stream.Metadata["peergo.schema"] != trackerswarmv1.SchemaVersion ||
		settings.Stream.Metadata["peergo.snapshot_scope"] != trackerswarmv1.ScopeAll {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestLoadSwarmStreamProvisionerRequiresThreeProductionReplicas(t *testing.T) {
	values := serverTestValues(t.TempDir())
	values["PEERGO_ENV"] = "production"
	values["PEERGO_TRACKER_NATS_URLS"] = "tls://nats.internal:4222"
	for name, value := range values {
		t.Setenv(name, value)
	}
	t.Setenv("PEERGO_TRACKER_NATS_PROVISION_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "operator.creds"))
	t.Setenv("PEERGO_TRACKER_SWARM_STREAM_PROVISION_TIMEOUT", "10s")
	t.Setenv("PEERGO_TRACKER_SWARM_STREAM_MAX_BYTES", "1073741824")
	t.Setenv("PEERGO_TRACKER_SWARM_STREAM_MAX_AGE", "24h")
	t.Setenv("PEERGO_TRACKER_SWARM_STREAM_DUPLICATE_WINDOW", "10m")
	t.Setenv("PEERGO_TRACKER_SWARM_STREAM_REPLICAS", "1")
	if _, err := LoadSwarmStreamProvisioner(); err == nil || !strings.Contains(err.Error(), "at least 3") {
		t.Fatalf("LoadSwarmStreamProvisioner() error = %v", err)
	}
}
