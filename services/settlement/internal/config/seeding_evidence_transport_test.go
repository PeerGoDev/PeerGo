package config

import (
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/settlementseedingv1"
)

func TestLoadSeedingEvidenceTransportUsesIndependentBoundedStream(t *testing.T) {
	setSeedingEvidenceTransportValues(t)
	dispatcher, err := LoadSeedingEvidenceDispatcher()
	if err != nil {
		t.Fatal(err)
	}
	provisioner, err := LoadSeedingEvidenceStreamProvisioner()
	if err != nil {
		t.Fatal(err)
	}
	if dispatcher.Stream != settlementseedingv1.DefaultStream || dispatcher.Subject != settlementseedingv1.DefaultSubject ||
		dispatcher.LeaseDuration != 30*time.Second || provisioner.Stream.Name != dispatcher.Stream ||
		provisioner.Stream.MaxMsgSize != settlementseedingv1.MaxEventBytes ||
		provisioner.Stream.Metadata["peergo.schema"] != settlementseedingv1.SchemaVersion {
		t.Fatalf("dispatcher=%+v stream=%+v", dispatcher, provisioner.Stream)
	}
}

func setSeedingEvidenceTransportValues(t *testing.T) {
	t.Helper()
	values := map[string]string{
		"PEERGO_ENV":                                                         "development",
		"PEERGO_TRACKER_DATABASE_URL":                                        "postgres://peergo_tracker:secret@db.internal:5432/peergo_tracker?sslmode=require",
		"PEERGO_SETTLEMENT_NATS_URLS":                                        "nats://127.0.0.1:4222",
		"PEERGO_SETTLEMENT_NATS_ROOT_CA_FILE":                                "",
		"PEERGO_SETTLEMENT_NATS_CONNECT_TIMEOUT":                             "2s",
		"PEERGO_SETTLEMENT_NATS_RECONNECT_WAIT":                              "1s",
		"PEERGO_SETTLEMENT_NATS_SEEDING_EVIDENCE_PUBLISH_CREDENTIALS_FILE":   "",
		"PEERGO_SETTLEMENT_NATS_SEEDING_EVIDENCE_PROVISION_CREDENTIALS_FILE": "",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STREAM":                          settlementseedingv1.DefaultStream,
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_SUBJECT":                         settlementseedingv1.DefaultSubject,
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_OUTBOX_LEASE_DURATION":           "30s",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_OUTBOX_IDLE_INTERVAL":            "500ms",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_OUTBOX_RETRY_BASE":               "1s",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_PUBLISH_TIMEOUT":                 "3s",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STARTUP_TIMEOUT":                 "10s",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_SHUTDOWN_TIMEOUT":                "10s",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STREAM_PROVISION_TIMEOUT":        "10s",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STREAM_MAX_BYTES":                "1073741824",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STREAM_MAX_AGE":                  "720h",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STREAM_DUPLICATE_WINDOW":         "10m",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STREAM_REPLICAS":                 "1",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}
