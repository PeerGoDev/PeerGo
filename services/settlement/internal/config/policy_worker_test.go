package config

import "testing"

func TestLoadPolicyWorkerRejectsMissingSchedulingBounds(t *testing.T) {
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_TRACKER_DATABASE_URL", "postgres://tracker.example/peergo_tracker")
	if _, err := LoadPolicyWorker(); err == nil {
		t.Fatal("LoadPolicyWorker() accepted missing scheduling configuration")
	}
}

func TestLoadHNRWorkerUsesIndependentSchedulingBounds(t *testing.T) {
	t.Setenv("PEERGO_ENV", "development")
	t.Setenv("PEERGO_TRACKER_DATABASE_URL", "postgres://tracker.example/peergo_tracker")
	t.Setenv("PEERGO_SETTLEMENT_HNR_LEASE_DURATION", "1m")
	t.Setenv("PEERGO_SETTLEMENT_HNR_IDLE_INTERVAL", "1s")
	t.Setenv("PEERGO_SETTLEMENT_HNR_RETRY_BASE", "2s")
	t.Setenv("PEERGO_SETTLEMENT_HNR_STARTUP_TIMEOUT", "10s")
	settings, err := LoadHNRWorker()
	if err != nil || settings.LeaseDuration.String() != "1m0s" || settings.RetryBase.String() != "2s" || settings.Concurrency != 1 {
		t.Fatalf("LoadHNRWorker() settings=%+v error=%v", settings, err)
	}
}

func TestLoadPolicyWorkerUsesBoundedConcurrency(t *testing.T) {
	t.Setenv("PEERGO_ENV", "production")
	t.Setenv("PEERGO_TRACKER_DATABASE_URL", "postgres://tracker.example/peergo_tracker")
	t.Setenv("PEERGO_SETTLEMENT_POLICY_LEASE_DURATION", "30s")
	t.Setenv("PEERGO_SETTLEMENT_POLICY_IDLE_INTERVAL", "500ms")
	t.Setenv("PEERGO_SETTLEMENT_POLICY_RETRY_BASE", "1s")
	t.Setenv("PEERGO_SETTLEMENT_POLICY_STARTUP_TIMEOUT", "15s")
	t.Setenv("PEERGO_SETTLEMENT_POLICY_CONCURRENCY", "8")

	settings, err := LoadPolicyWorker()
	if err != nil || settings.Concurrency != 8 {
		t.Fatalf("LoadPolicyWorker() settings=%+v error=%v", settings, err)
	}
}

func TestLoadPolicyWorkerRejectsConcurrencyOutsideBound(t *testing.T) {
	t.Setenv("PEERGO_ENV", "production")
	t.Setenv("PEERGO_TRACKER_DATABASE_URL", "postgres://tracker.example/peergo_tracker")
	t.Setenv("PEERGO_SETTLEMENT_POLICY_LEASE_DURATION", "30s")
	t.Setenv("PEERGO_SETTLEMENT_POLICY_IDLE_INTERVAL", "500ms")
	t.Setenv("PEERGO_SETTLEMENT_POLICY_RETRY_BASE", "1s")
	t.Setenv("PEERGO_SETTLEMENT_POLICY_STARTUP_TIMEOUT", "15s")
	t.Setenv("PEERGO_SETTLEMENT_POLICY_CONCURRENCY", "33")

	if _, err := LoadPolicyWorker(); err == nil {
		t.Fatal("LoadPolicyWorker() accepted concurrency above its safety bound")
	}
}
