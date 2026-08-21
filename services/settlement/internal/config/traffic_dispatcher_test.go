package config

import (
	"strings"
	"testing"
)

func TestLoadTrafficDispatcherUsesBoundedConcurrency(t *testing.T) {
	setTrafficDispatcherValues(t)
	settings, err := LoadTrafficDispatcher()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Concurrency != 4 {
		t.Fatalf("Concurrency = %d", settings.Concurrency)
	}
}

func TestLoadTrafficDispatcherRejectsConcurrencyOutsideBound(t *testing.T) {
	setTrafficDispatcherValues(t)
	t.Setenv("PEERGO_SETTLEMENT_TRAFFIC_OUTBOX_CONCURRENCY", "33")
	if _, err := LoadTrafficDispatcher(); err == nil || !strings.Contains(err.Error(), "between 1 and 32") {
		t.Fatalf("LoadTrafficDispatcher() error = %v", err)
	}
}

func setTrafficDispatcherValues(t *testing.T) {
	t.Helper()
	values := map[string]string{
		"PEERGO_ENV":                                      "development",
		"PEERGO_TRACKER_DATABASE_URL":                     "postgres://peergo_tracker:secret@db.internal:5432/peergo_tracker?sslmode=require",
		"PEERGO_SETTLEMENT_NATS_URLS":                     "nats://127.0.0.1:4222",
		"PEERGO_SETTLEMENT_NATS_PUBLISH_CREDENTIALS_FILE": "",
		"PEERGO_SETTLEMENT_NATS_ROOT_CA_FILE":             "",
		"PEERGO_SETTLEMENT_NATS_CONNECT_TIMEOUT":          "2s",
		"PEERGO_SETTLEMENT_NATS_RECONNECT_WAIT":           "1s",
		"PEERGO_SETTLEMENT_TRAFFIC_STREAM":                "PEERGO_SETTLEMENT_TRAFFIC_V1",
		"PEERGO_SETTLEMENT_TRAFFIC_SUBJECT":               "peergo.settlement.traffic.v1",
		"PEERGO_SETTLEMENT_TRAFFIC_OUTBOX_LEASE_DURATION": "30s",
		"PEERGO_SETTLEMENT_TRAFFIC_OUTBOX_IDLE_INTERVAL":  "500ms",
		"PEERGO_SETTLEMENT_TRAFFIC_OUTBOX_RETRY_BASE":     "1s",
		"PEERGO_SETTLEMENT_TRAFFIC_OUTBOX_CONCURRENCY":    "4",
		"PEERGO_SETTLEMENT_TRAFFIC_PUBLISH_TIMEOUT":       "5s",
		"PEERGO_SETTLEMENT_TRAFFIC_STARTUP_TIMEOUT":       "10s",
		"PEERGO_SETTLEMENT_TRAFFIC_SHUTDOWN_TIMEOUT":      "10s",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}
