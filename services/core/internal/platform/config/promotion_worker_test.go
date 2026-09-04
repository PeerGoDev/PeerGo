package config

import (
	"strings"
	"testing"
	"time"
)

func setPromotionWorkerEnvironment(t *testing.T, controlURL string) {
	t.Helper()
	t.Setenv("PEERGO_ENV", "production")
	t.Setenv("PEERGO_CORE_DATABASE_URL", "postgres://core.example/peergo")
	t.Setenv("PEERGO_SETTLEMENT_CONTROL_URL", controlURL)
	t.Setenv("PEERGO_SETTLEMENT_CONTROL_SERVICE_TOKEN", strings.Repeat("t", 32))
}

func TestLoadPromotionWorkerAllowsProductionIPLoopbackHTTP(t *testing.T) {
	setPromotionWorkerEnvironment(t, "http://127.0.0.1:18085")

	settings, err := LoadPromotionWorker()
	if err != nil {
		t.Fatalf("load production loopback control URL: %v", err)
	}
	if settings.SettlementURL != "http://127.0.0.1:18085" {
		t.Fatalf("unexpected settlement URL %q", settings.SettlementURL)
	}
	if settings.WorkgroupEnforcementInterval != time.Hour || settings.WorkgroupEnforcementBatch != 500 {
		t.Fatalf("unexpected workgroup enforcement defaults: interval=%s batch=%d", settings.WorkgroupEnforcementInterval, settings.WorkgroupEnforcementBatch)
	}
}

func TestLoadPromotionWorkerValidatesWorkgroupEnforcementBounds(t *testing.T) {
	setPromotionWorkerEnvironment(t, "http://127.0.0.1:18085")
	t.Setenv("PEERGO_WORKGROUP_ENFORCEMENT_INTERVAL", "30s")
	if _, err := LoadPromotionWorker(); err == nil || !strings.Contains(err.Error(), "between 1m and 24h") {
		t.Fatalf("expected workgroup enforcement interval rejection, got %v", err)
	}
}

func TestLoadPromotionWorkerRejectsProductionRoutableHTTP(t *testing.T) {
	setPromotionWorkerEnvironment(t, "http://settlement.internal:8085")

	_, err := LoadPromotionWorker()
	if err == nil || !strings.Contains(err.Error(), "must use https") {
		t.Fatalf("expected production HTTP rejection, got %v", err)
	}
}

func TestLoadPromotionWorkerAllowsFixedSingleServerService(t *testing.T) {
	setPromotionWorkerEnvironment(t, "http://settlement-control-api:8085")
	t.Setenv("PEERGO_DEPLOYMENT_MODE", "single-server")
	if _, err := LoadPromotionWorker(); err != nil {
		t.Fatalf("single-server Settlement service rejected: %v", err)
	}
}
