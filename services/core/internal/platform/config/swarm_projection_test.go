package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
)

func TestLoadSwarmProjectorBuildsTwoNarrowBindings(t *testing.T) {
	setSwarmProjectorConfigValues(t, "development")
	settings, err := LoadSwarmProjector()
	if err != nil {
		t.Fatal(err)
	}
	if settings.DatabaseURL == "" || len(settings.NATS.URLs) != 1 ||
		settings.Snapshot.Stream != trackerswarmv1.DefaultStream || settings.Snapshot.Subject != trackerswarmv1.DefaultSubject ||
		settings.Snapshot.Durable != "PEERGO_CORE_SWARM_SNAPSHOT_V1" ||
		settings.Completion.Stream != trackerannouncev1.DefaultStream || settings.Completion.Subject != trackerannouncev1.DefaultSubject ||
		settings.Completion.Durable != "PEERGO_CORE_SWARM_COMPLETION_V1" ||
		settings.Snapshot.FetchWait != 2*time.Second || settings.MaxFutureSkew != 2*time.Minute {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestLoadSwarmConsumerProvisionerBuildsIndependentOrderedConsumers(t *testing.T) {
	setSwarmProjectorConfigValues(t, "development")
	settings, err := LoadSwarmConsumerProvisioner()
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.Consumers) != 2 {
		t.Fatalf("consumer count = %d", len(settings.Consumers))
	}
	for _, provision := range settings.Consumers {
		consumer := provision.Consumer
		if consumer.Name != consumer.Durable || consumer.MaxDeliver != -1 || consumer.MaxAckPending != 1 ||
			consumer.MaxRequestBatch != 1 || consumer.Metadata["peergo.owner"] != "core" {
			t.Fatalf("consumer = %+v", consumer)
		}
	}
	if settings.Consumers[0].Consumer.Metadata["peergo.schema"] != trackerswarmv1.SchemaVersion ||
		settings.Consumers[1].Consumer.Metadata["peergo.schema"] != trackerannouncev1.SchemaVersion {
		t.Fatalf("consumers = %+v", settings.Consumers)
	}
}

func TestSwarmProjectorProductionRequiresTLSAndSeparateCredentials(t *testing.T) {
	setSwarmProjectorConfigValues(t, "production")
	t.Setenv("PEERGO_CORE_SWARM_NATS_URLS", "nats://nats.internal:4222")
	if _, err := LoadSwarmProjector(); err == nil || !strings.Contains(err.Error(), "tls://") {
		t.Fatalf("LoadSwarmProjector(insecure NATS) error = %v", err)
	}
	setSwarmProjectorConfigValues(t, "production")
	t.Setenv("PEERGO_CORE_SWARM_NATS_PROVISION_CREDENTIALS_FILE", "")
	if _, err := LoadSwarmConsumerProvisioner(); err == nil || !strings.Contains(err.Error(), "PROVISION_CREDENTIALS_FILE") {
		t.Fatalf("LoadSwarmConsumerProvisioner(missing credentials) error = %v", err)
	}
}

func TestSwarmConsumerProvisionerDoesNotRequireCoreDatabaseCredential(t *testing.T) {
	setSwarmProjectorConfigValues(t, "development")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "")
	if _, err := LoadSwarmConsumerProvisioner(); err != nil {
		t.Fatalf("LoadSwarmConsumerProvisioner() error = %v", err)
	}
}

func setSwarmProjectorConfigValues(t *testing.T, environment string) {
	t.Helper()
	directory := t.TempDir()
	values := map[string]string{
		"PEERGO_ENV":                                        environment,
		"PEERGO_CORE_DATABASE_URL":                          "postgres://peergo_core:secret@db.internal:5432/peergo_core?sslmode=require",
		"PEERGO_CORE_SWARM_NATS_URLS":                       "nats://127.0.0.1:4222",
		"PEERGO_CORE_SWARM_NATS_CREDENTIALS_FILE":           "",
		"PEERGO_CORE_SWARM_NATS_PROVISION_CREDENTIALS_FILE": "",
		"PEERGO_CORE_SWARM_NATS_ROOT_CA_FILE":               "",
		"PEERGO_CORE_SWARM_NATS_CONNECT_TIMEOUT":            "2s",
		"PEERGO_CORE_SWARM_NATS_RECONNECT_WAIT":             "1s",
		"PEERGO_TRACKER_SWARM_SNAPSHOT_STREAM":              trackerswarmv1.DefaultStream,
		"PEERGO_TRACKER_SWARM_SNAPSHOT_SUBJECT":             trackerswarmv1.DefaultSubject,
		"PEERGO_CORE_SWARM_SNAPSHOT_DURABLE":                "PEERGO_CORE_SWARM_SNAPSHOT_V1",
		"PEERGO_TRACKER_ANNOUNCE_STREAM":                    trackerannouncev1.DefaultStream,
		"PEERGO_TRACKER_ANNOUNCE_SUBJECT":                   trackerannouncev1.DefaultSubject,
		"PEERGO_CORE_SWARM_COMPLETION_DURABLE":              "PEERGO_CORE_SWARM_COMPLETION_V1",
		"PEERGO_CORE_SWARM_FETCH_WAIT":                      "2s",
		"PEERGO_CORE_SWARM_PROCESS_TIMEOUT":                 "10s",
		"PEERGO_CORE_SWARM_ACK_TIMEOUT":                     "5s",
		"PEERGO_CORE_SWARM_RETRY_DELAY":                     "1s",
		"PEERGO_CORE_SWARM_STARTUP_TIMEOUT":                 "10s",
		"PEERGO_CORE_SWARM_SHUTDOWN_TIMEOUT":                "10s",
		"PEERGO_CORE_SWARM_MAX_FUTURE_SKEW":                 "2m",
		"PEERGO_CORE_SWARM_CONSUMER_PROVISION_TIMEOUT":      "10s",
		"PEERGO_CORE_SWARM_CONSUMER_ACK_WAIT":               "30s",
		"PEERGO_CORE_SWARM_CONSUMER_MAX_WAITING":            "16",
		"PEERGO_CORE_SWARM_CONSUMER_MAX_REQUEST_EXPIRES":    "5s",
	}
	if environment == "production" {
		values["PEERGO_CORE_SWARM_NATS_URLS"] = "tls://nats.internal:4222"
		values["PEERGO_CORE_SWARM_NATS_CREDENTIALS_FILE"] = filepath.Join(directory, "runtime.creds")
		values["PEERGO_CORE_SWARM_NATS_PROVISION_CREDENTIALS_FILE"] = filepath.Join(directory, "provision.creds")
		values["PEERGO_CORE_SWARM_NATS_ROOT_CA_FILE"] = filepath.Join(directory, "root-ca.pem")
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}
