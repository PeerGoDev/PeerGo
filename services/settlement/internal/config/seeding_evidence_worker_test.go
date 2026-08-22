package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadSeedingEvidenceWorkerRequiresSafeEventTimeWatermark(t *testing.T) {
	setSeedingEvidenceWorkerValues(t)
	settings, err := LoadSeedingEvidenceWorker()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ClosureDelay != 45*time.Minute || settings.MaxIntervalCredit != 35*time.Minute {
		t.Fatalf("settings = %+v", settings)
	}

	t.Setenv("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_CLOSURE_DELAY", "30m")
	if _, err := LoadSeedingEvidenceWorker(); err == nil || !strings.Contains(err.Error(), "must be at least") {
		t.Fatalf("LoadSeedingEvidenceWorker(unsafe watermark) error = %v", err)
	}
}

func TestLoadSeedingEvidenceWorkerRequiresAuditableWholeSeconds(t *testing.T) {
	setSeedingEvidenceWorkerValues(t)
	t.Setenv("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_MAX_INTERVAL_CREDIT", "35m500ms")
	if _, err := LoadSeedingEvidenceWorker(); err == nil || !strings.Contains(err.Error(), "whole seconds") {
		t.Fatalf("LoadSeedingEvidenceWorker(fractional limit) error = %v", err)
	}
}

func setSeedingEvidenceWorkerValues(t *testing.T) {
	t.Helper()
	setConfigValues(t, "development")
	values := map[string]string{
		"PEERGO_TRACKER_SWARM_SNAPSHOT_STREAM":                   "PEERGO_TRACKER_SWARM_SNAPSHOT_V1",
		"PEERGO_TRACKER_SWARM_SNAPSHOT_SUBJECT":                  "peergo.tracker.swarm.snapshot.v1",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT":            "2026-08-22T00:00:00Z",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_CLOSURE_DELAY":       "45m",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_MAX_INTERVAL_CREDIT": "35m",
		"PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_MAX_CLOSURE_DELAY":   "15m",
		"PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_MAX_FUTURE_SKEW":     "2m",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_IDLE_INTERVAL":       "5s",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STARTUP_TIMEOUT":     "15s",
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE_SHUTDOWN_TIMEOUT":    "10s",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}
