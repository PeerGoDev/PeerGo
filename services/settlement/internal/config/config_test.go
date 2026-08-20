package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadRuntimeBuildsNarrowConsumerConfiguration(t *testing.T) {
	setConfigValues(t, "development")
	settings, err := LoadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if settings.DatabaseURL == "" || len(settings.NATS.URLs) != 1 ||
		settings.Stream != "PEERGO_TRACKER_ANNOUNCE_V1" || settings.Durable != "PEERGO_SETTLEMENT_V1" ||
		settings.FetchWait != 2*time.Second || settings.ProcessTimeout != 10*time.Second ||
		settings.AckTimeout != 5*time.Second || settings.RetryDelay != time.Second {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestLoadConsumerProvisionerBuildsOrderedDurableConsumer(t *testing.T) {
	setConfigValues(t, "development")
	settings, err := LoadConsumerProvisioner()
	if err != nil {
		t.Fatal(err)
	}
	consumer := settings.Consumer
	if consumer.Name != consumer.Durable || consumer.FilterSubject != "peergo.tracker.announce.v1" ||
		consumer.MaxDeliver != -1 || consumer.MaxAckPending != 1 || consumer.MaxRequestBatch != 1 ||
		consumer.Metadata["peergo.schema"] != "tracker.announce.v1" {
		t.Fatalf("consumer = %+v", consumer)
	}
}

func TestProductionRequiresTLSAndSeparateCredentials(t *testing.T) {
	setConfigValues(t, "production")
	t.Setenv("PEERGO_SETTLEMENT_NATS_URLS", "nats://nats.internal:4222")
	if _, err := LoadRuntime(); err == nil || !strings.Contains(err.Error(), "tls://") {
		t.Fatalf("LoadRuntime(insecure NATS) error = %v", err)
	}
	t.Setenv("PEERGO_SETTLEMENT_NATS_URLS", "tls://nats.internal:4222")
	t.Setenv("PEERGO_SETTLEMENT_NATS_CREDENTIALS_FILE", "")
	if _, err := LoadRuntime(); err == nil || !strings.Contains(err.Error(), "required in production") {
		t.Fatalf("LoadRuntime(missing credentials) error = %v", err)
	}
	setConfigValues(t, "production")
	t.Setenv("PEERGO_SETTLEMENT_NATS_PROVISION_CREDENTIALS_FILE", "")
	if _, err := LoadConsumerProvisioner(); err == nil || !strings.Contains(err.Error(), "PROVISION_CREDENTIALS_FILE") {
		t.Fatalf("LoadConsumerProvisioner(missing management credentials) error = %v", err)
	}
}

func TestSingleServerAcceptsOnlyItsPrivateNATSAndDatabaseNames(t *testing.T) {
	setConfigValues(t, "production")
	t.Setenv("PEERGO_DEPLOYMENT_MODE", "single-server")
	t.Setenv("PEERGO_SETTLEMENT_NATS_URLS", "nats://peergo-nats:4222")
	t.Setenv("PEERGO_TRACKER_DATABASE_URL", "postgres://peergo_tracker:secret@postgresql:5432/peergo_tracker?sslmode=disable")
	if _, err := LoadRuntime(); err != nil {
		t.Fatalf("single-server runtime rejected: %v", err)
	}
	t.Setenv("PEERGO_SETTLEMENT_NATS_URLS", "nats://other-nats:4222")
	if _, err := LoadRuntime(); err == nil {
		t.Fatal("single-server accepted another clear-text NATS host")
	}
}

func TestConsumerAckWaitMustContainProcessingAndConfirmedAck(t *testing.T) {
	setConfigValues(t, "development")
	t.Setenv("PEERGO_SETTLEMENT_CONSUMER_ACK_WAIT", "15s")
	if _, err := LoadConsumerProvisioner(); err == nil || !strings.Contains(err.Error(), "must exceed") {
		t.Fatalf("LoadConsumerProvisioner() error = %v", err)
	}
}

func setConfigValues(t *testing.T, environment string) {
	t.Helper()
	directory := t.TempDir()
	values := map[string]string{
		"PEERGO_ENV":                                        environment,
		"PEERGO_TRACKER_DATABASE_URL":                       "postgres://peergo_tracker:secret@db.internal:5432/peergo_tracker?sslmode=require",
		"PEERGO_SETTLEMENT_NATS_URLS":                       "nats://127.0.0.1:4222",
		"PEERGO_SETTLEMENT_NATS_CREDENTIALS_FILE":           "",
		"PEERGO_SETTLEMENT_NATS_PROVISION_CREDENTIALS_FILE": "",
		"PEERGO_SETTLEMENT_NATS_ROOT_CA_FILE":               "",
		"PEERGO_SETTLEMENT_NATS_CONNECT_TIMEOUT":            "2s",
		"PEERGO_SETTLEMENT_NATS_RECONNECT_WAIT":             "1s",
		"PEERGO_TRACKER_ANNOUNCE_STREAM":                    "PEERGO_TRACKER_ANNOUNCE_V1",
		"PEERGO_TRACKER_ANNOUNCE_SUBJECT":                   "peergo.tracker.announce.v1",
		"PEERGO_SETTLEMENT_ANNOUNCE_DURABLE":                "PEERGO_SETTLEMENT_V1",
		"PEERGO_SETTLEMENT_FETCH_WAIT":                      "2s",
		"PEERGO_SETTLEMENT_PROCESS_TIMEOUT":                 "10s",
		"PEERGO_SETTLEMENT_ACK_TIMEOUT":                     "5s",
		"PEERGO_SETTLEMENT_RETRY_DELAY":                     "1s",
		"PEERGO_SETTLEMENT_STARTUP_TIMEOUT":                 "10s",
		"PEERGO_SETTLEMENT_SHUTDOWN_TIMEOUT":                "10s",
		"PEERGO_SETTLEMENT_CONSUMER_PROVISION_TIMEOUT":      "10s",
		"PEERGO_SETTLEMENT_CONSUMER_ACK_WAIT":               "30s",
		"PEERGO_SETTLEMENT_CONSUMER_MAX_WAITING":            "16",
		"PEERGO_SETTLEMENT_CONSUMER_MAX_REQUEST_EXPIRES":    "5s",
	}
	if environment == "production" {
		values["PEERGO_SETTLEMENT_NATS_URLS"] = "tls://nats.internal:4222"
		values["PEERGO_SETTLEMENT_NATS_CREDENTIALS_FILE"] = filepath.Join(directory, "runtime.creds")
		values["PEERGO_SETTLEMENT_NATS_PROVISION_CREDENTIALS_FILE"] = filepath.Join(directory, "provision.creds")
		values["PEERGO_SETTLEMENT_NATS_ROOT_CA_FILE"] = filepath.Join(directory, "root-ca.pem")
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}
