package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadTrafficProjectorBuildsNarrowRuntimeConfiguration(t *testing.T) {
	setTrafficProjectorConfigValues(t, "development")
	settings, err := LoadTrafficProjector()
	if err != nil {
		t.Fatal(err)
	}
	if settings.DatabaseURL == "" || len(settings.NATS.URLs) != 1 ||
		settings.Stream != "PEERGO_SETTLEMENT_TRAFFIC_V1" || settings.Subject != "peergo.settlement.traffic.v1" ||
		settings.Durable != "PEERGO_CORE_TRAFFIC_V1" || settings.FetchWait != 2*time.Second ||
		settings.ProcessTimeout != 10*time.Second || settings.AckTimeout != 5*time.Second || settings.RetryDelay != time.Second ||
		settings.Concurrency != 4 {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestLoadTrafficConsumerProvisionerBuildsOrderedDurableConsumer(t *testing.T) {
	setTrafficProjectorConfigValues(t, "development")
	settings, err := LoadTrafficConsumerProvisioner()
	if err != nil {
		t.Fatal(err)
	}
	consumer := settings.Consumer
	if consumer.Name != consumer.Durable || consumer.FilterSubject != "peergo.settlement.traffic.v1" ||
		consumer.MaxDeliver != -1 || consumer.MaxAckPending != 4 || consumer.MaxRequestBatch != 1 ||
		consumer.Metadata["peergo.schema"] != "settlement.traffic.v1" {
		t.Fatalf("consumer = %+v", consumer)
	}
}

func TestTrafficProjectionRejectsConcurrencyOutsideBound(t *testing.T) {
	setTrafficProjectorConfigValues(t, "development")
	t.Setenv("PEERGO_CORE_TRAFFIC_CONCURRENCY", "33")
	if _, err := LoadTrafficProjector(); err == nil || !strings.Contains(err.Error(), "between 1 and 32") {
		t.Fatalf("LoadTrafficProjector() error = %v", err)
	}
	if _, err := LoadTrafficConsumerProvisioner(); err == nil || !strings.Contains(err.Error(), "between 1 and 32") {
		t.Fatalf("LoadTrafficConsumerProvisioner() error = %v", err)
	}
}

func TestLoadHNRProjectionUsesIndependentIdentityAndCredentials(t *testing.T) {
	setHNRProjectorConfigValues(t, "development")
	projector, err := LoadHNRProjector()
	if err != nil {
		t.Fatal(err)
	}
	provisioner, err := LoadHNRConsumerProvisioner()
	if err != nil {
		t.Fatal(err)
	}
	if projector.Stream != "PEERGO_SETTLEMENT_HNR_V1" || projector.Subject != "peergo.settlement.hnr.v1" ||
		projector.Durable != "PEERGO_CORE_HNR_V1" || provisioner.Consumer.Metadata["peergo.schema"] != "settlement.hnr.v1" ||
		provisioner.Consumer.FilterSubject != projector.Subject {
		t.Fatalf("projector=%+v consumer=%+v", projector, provisioner.Consumer)
	}
}

func TestLoadSeedingEvidenceProjectionUsesIndependentIdentity(t *testing.T) {
	setSeedingEvidenceProjectorConfigValues(t, "development")
	projector, err := LoadSeedingEvidenceProjector()
	if err != nil {
		t.Fatal(err)
	}
	provisioner, err := LoadSeedingEvidenceConsumerProvisioner()
	if err != nil {
		t.Fatal(err)
	}
	if projector.Stream != "PEERGO_SETTLEMENT_SEEDING_EVIDENCE_V1" ||
		projector.Subject != "peergo.settlement.seeding.evidence.v1" ||
		projector.Durable != "PEERGO_CORE_SEEDING_EVIDENCE_V1" || projector.MaxFutureSkew != 2*time.Minute ||
		provisioner.Consumer.Metadata["peergo.schema"] != "settlement.seeding.evidence.v1" ||
		provisioner.Consumer.FilterSubject != projector.Subject {
		t.Fatalf("projector=%+v consumer=%+v", projector, provisioner.Consumer)
	}
}

func TestTrafficProjectorProductionRequiresTLSAndSeparateCredentials(t *testing.T) {
	setTrafficProjectorConfigValues(t, "production")
	t.Setenv("PEERGO_CORE_TRAFFIC_NATS_URLS", "nats://nats.internal:4222")
	if _, err := LoadTrafficProjector(); err == nil || !strings.Contains(err.Error(), "tls://") {
		t.Fatalf("LoadTrafficProjector(insecure NATS) error = %v", err)
	}
	t.Setenv("PEERGO_CORE_TRAFFIC_NATS_URLS", "tls://nats.internal:4222")
	t.Setenv("PEERGO_CORE_TRAFFIC_NATS_CREDENTIALS_FILE", "")
	if _, err := LoadTrafficProjector(); err == nil || !strings.Contains(err.Error(), "required in production") {
		t.Fatalf("LoadTrafficProjector(missing credentials) error = %v", err)
	}
	setTrafficProjectorConfigValues(t, "production")
	t.Setenv("PEERGO_CORE_TRAFFIC_NATS_PROVISION_CREDENTIALS_FILE", "")
	if _, err := LoadTrafficConsumerProvisioner(); err == nil || !strings.Contains(err.Error(), "PROVISION_CREDENTIALS_FILE") {
		t.Fatalf("LoadTrafficConsumerProvisioner(missing management credentials) error = %v", err)
	}
}

func TestTrafficConsumerAckWaitMustContainProcessingAndConfirmedAck(t *testing.T) {
	setTrafficProjectorConfigValues(t, "development")
	t.Setenv("PEERGO_CORE_TRAFFIC_CONSUMER_ACK_WAIT", "15s")
	if _, err := LoadTrafficConsumerProvisioner(); err == nil || !strings.Contains(err.Error(), "must exceed") {
		t.Fatalf("LoadTrafficConsumerProvisioner() error = %v", err)
	}
}

func TestTrafficConsumerProvisionerDoesNotRequireCoreDatabaseCredential(t *testing.T) {
	setTrafficProjectorConfigValues(t, "development")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "")
	if _, err := LoadTrafficConsumerProvisioner(); err != nil {
		t.Fatalf("LoadTrafficConsumerProvisioner() error = %v", err)
	}
}

func setTrafficProjectorConfigValues(t *testing.T, environment string) {
	t.Helper()
	directory := t.TempDir()
	values := map[string]string{
		"PEERGO_ENV":                                          environment,
		"PEERGO_CORE_DATABASE_URL":                            "postgres://peergo_core:secret@db.internal:5432/peergo_core?sslmode=require",
		"PEERGO_CORE_TRAFFIC_NATS_URLS":                       "nats://127.0.0.1:4222",
		"PEERGO_CORE_TRAFFIC_NATS_CREDENTIALS_FILE":           "",
		"PEERGO_CORE_TRAFFIC_NATS_PROVISION_CREDENTIALS_FILE": "",
		"PEERGO_CORE_TRAFFIC_NATS_ROOT_CA_FILE":               "",
		"PEERGO_CORE_TRAFFIC_NATS_CONNECT_TIMEOUT":            "2s",
		"PEERGO_CORE_TRAFFIC_NATS_RECONNECT_WAIT":             "1s",
		"PEERGO_SETTLEMENT_TRAFFIC_STREAM":                    "PEERGO_SETTLEMENT_TRAFFIC_V1",
		"PEERGO_SETTLEMENT_TRAFFIC_SUBJECT":                   "peergo.settlement.traffic.v1",
		"PEERGO_CORE_TRAFFIC_DURABLE":                         "PEERGO_CORE_TRAFFIC_V1",
		"PEERGO_CORE_TRAFFIC_CONCURRENCY":                     "4",
		"PEERGO_CORE_TRAFFIC_FETCH_WAIT":                      "2s",
		"PEERGO_CORE_TRAFFIC_PROCESS_TIMEOUT":                 "10s",
		"PEERGO_CORE_TRAFFIC_ACK_TIMEOUT":                     "5s",
		"PEERGO_CORE_TRAFFIC_RETRY_DELAY":                     "1s",
		"PEERGO_CORE_TRAFFIC_STARTUP_TIMEOUT":                 "10s",
		"PEERGO_CORE_TRAFFIC_SHUTDOWN_TIMEOUT":                "10s",
		"PEERGO_CORE_TRAFFIC_CONSUMER_PROVISION_TIMEOUT":      "10s",
		"PEERGO_CORE_TRAFFIC_CONSUMER_ACK_WAIT":               "30s",
		"PEERGO_CORE_TRAFFIC_CONSUMER_MAX_WAITING":            "16",
		"PEERGO_CORE_TRAFFIC_CONSUMER_MAX_REQUEST_EXPIRES":    "5s",
	}
	if environment == "production" {
		values["PEERGO_CORE_TRAFFIC_NATS_URLS"] = "tls://nats.internal:4222"
		values["PEERGO_CORE_TRAFFIC_NATS_CREDENTIALS_FILE"] = filepath.Join(directory, "runtime.creds")
		values["PEERGO_CORE_TRAFFIC_NATS_PROVISION_CREDENTIALS_FILE"] = filepath.Join(directory, "provision.creds")
		values["PEERGO_CORE_TRAFFIC_NATS_ROOT_CA_FILE"] = filepath.Join(directory, "root-ca.pem")
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}

func setHNRProjectorConfigValues(t *testing.T, environment string) {
	t.Helper()
	directory := t.TempDir()
	values := map[string]string{
		"PEERGO_ENV":                                      environment,
		"PEERGO_CORE_DATABASE_URL":                        "postgres://peergo_core:secret@db.internal:5432/peergo_core?sslmode=require",
		"PEERGO_CORE_HNR_NATS_URLS":                       "nats://127.0.0.1:4222",
		"PEERGO_CORE_HNR_NATS_CREDENTIALS_FILE":           "",
		"PEERGO_CORE_HNR_NATS_PROVISION_CREDENTIALS_FILE": "",
		"PEERGO_CORE_HNR_NATS_ROOT_CA_FILE":               "",
		"PEERGO_CORE_HNR_NATS_CONNECT_TIMEOUT":            "2s",
		"PEERGO_CORE_HNR_NATS_RECONNECT_WAIT":             "1s",
		"PEERGO_SETTLEMENT_HNR_STREAM":                    "PEERGO_SETTLEMENT_HNR_V1",
		"PEERGO_SETTLEMENT_HNR_SUBJECT":                   "peergo.settlement.hnr.v1",
		"PEERGO_CORE_HNR_DURABLE":                         "PEERGO_CORE_HNR_V1",
		"PEERGO_CORE_HNR_FETCH_WAIT":                      "2s",
		"PEERGO_CORE_HNR_PROCESS_TIMEOUT":                 "10s",
		"PEERGO_CORE_HNR_ACK_TIMEOUT":                     "5s",
		"PEERGO_CORE_HNR_RETRY_DELAY":                     "1s",
		"PEERGO_CORE_HNR_STARTUP_TIMEOUT":                 "10s",
		"PEERGO_CORE_HNR_SHUTDOWN_TIMEOUT":                "10s",
		"PEERGO_CORE_HNR_CONSUMER_PROVISION_TIMEOUT":      "10s",
		"PEERGO_CORE_HNR_CONSUMER_ACK_WAIT":               "30s",
		"PEERGO_CORE_HNR_CONSUMER_MAX_WAITING":            "16",
		"PEERGO_CORE_HNR_CONSUMER_MAX_REQUEST_EXPIRES":    "5s",
	}
	if environment == "production" {
		values["PEERGO_CORE_HNR_NATS_URLS"] = "tls://nats.internal:4222"
		values["PEERGO_CORE_HNR_NATS_CREDENTIALS_FILE"] = filepath.Join(directory, "hnr-runtime.creds")
		values["PEERGO_CORE_HNR_NATS_PROVISION_CREDENTIALS_FILE"] = filepath.Join(directory, "hnr-provision.creds")
		values["PEERGO_CORE_HNR_NATS_ROOT_CA_FILE"] = filepath.Join(directory, "hnr-root-ca.pem")
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}

func setSeedingEvidenceProjectorConfigValues(t *testing.T, environment string) {
	t.Helper()
	directory := t.TempDir()
	values := map[string]string{
		"PEERGO_ENV":                                                   environment,
		"PEERGO_CORE_DATABASE_URL":                                     "postgres://peergo_core:secret@db.internal:5432/peergo_core?sslmode=require",
		"PEERGO_CORE_SEEDING_EVIDENCE_NATS_URLS":                       "nats://127.0.0.1:4222",
		"PEERGO_CORE_SEEDING_EVIDENCE_NATS_CREDENTIALS_FILE":           "",
		"PEERGO_CORE_SEEDING_EVIDENCE_NATS_PROVISION_CREDENTIALS_FILE": "",
		"PEERGO_CORE_SEEDING_EVIDENCE_NATS_ROOT_CA_FILE":               "",
		"PEERGO_CORE_SEEDING_EVIDENCE_NATS_CONNECT_TIMEOUT":            "2s",
		"PEERGO_CORE_SEEDING_EVIDENCE_NATS_RECONNECT_WAIT":             "1s",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STREAM":                    "PEERGO_SETTLEMENT_SEEDING_EVIDENCE_V1",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_SUBJECT":                   "peergo.settlement.seeding.evidence.v1",
		"PEERGO_CORE_SEEDING_EVIDENCE_DURABLE":                         "PEERGO_CORE_SEEDING_EVIDENCE_V1",
		"PEERGO_CORE_SEEDING_EVIDENCE_FETCH_WAIT":                      "2s",
		"PEERGO_CORE_SEEDING_EVIDENCE_PROCESS_TIMEOUT":                 "10s",
		"PEERGO_CORE_SEEDING_EVIDENCE_ACK_TIMEOUT":                     "5s",
		"PEERGO_CORE_SEEDING_EVIDENCE_RETRY_DELAY":                     "1s",
		"PEERGO_CORE_SEEDING_EVIDENCE_STARTUP_TIMEOUT":                 "10s",
		"PEERGO_CORE_SEEDING_EVIDENCE_SHUTDOWN_TIMEOUT":                "10s",
		"PEERGO_CORE_SEEDING_EVIDENCE_MAX_FUTURE_SKEW":                 "2m",
		"PEERGO_CORE_SEEDING_EVIDENCE_CONSUMER_PROVISION_TIMEOUT":      "10s",
		"PEERGO_CORE_SEEDING_EVIDENCE_CONSUMER_ACK_WAIT":               "30s",
		"PEERGO_CORE_SEEDING_EVIDENCE_CONSUMER_MAX_WAITING":            "16",
		"PEERGO_CORE_SEEDING_EVIDENCE_CONSUMER_MAX_REQUEST_EXPIRES":    "5s",
	}
	if environment == "production" {
		values["PEERGO_CORE_SEEDING_EVIDENCE_NATS_URLS"] = "tls://nats.internal:4222"
		values["PEERGO_CORE_SEEDING_EVIDENCE_NATS_CREDENTIALS_FILE"] = filepath.Join(directory, "seeding-runtime.creds")
		values["PEERGO_CORE_SEEDING_EVIDENCE_NATS_PROVISION_CREDENTIALS_FILE"] = filepath.Join(directory, "seeding-provision.creds")
		values["PEERGO_CORE_SEEDING_EVIDENCE_NATS_ROOT_CA_FILE"] = filepath.Join(directory, "seeding-root-ca.pem")
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}
