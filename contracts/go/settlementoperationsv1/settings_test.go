package settlementoperationsv1

import (
	"testing"
	"time"
)

func TestSettingsValidWithoutConfiguredPolicies(t *testing.T) {
	settings := Settings{
		GeneratedAt: time.Now().UTC(),
		Seedbox:     SeedboxPolicy{SettlementPrimitiveSupported: true},
	}
	if !settings.Valid() {
		t.Fatal("expected empty but well-formed settings to be valid")
	}
}
