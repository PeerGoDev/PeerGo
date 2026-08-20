package legacyseedboxes

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBuildBindingsNormalizesReviewedPtYesAddressShapes(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	rows := []sourceRow{
		{
			LegacyID: 1, UserID: 10, IP: "http://152.53.39.61", Status: 1,
			CreatedAt: now, UpdatedAt: now.Add(-time.Minute),
		},
		{
			LegacyID: 2, UserID: 20, IP: "158.101.159.166",
			CIDR: "2603:c021:8017:e7dc:fed3:f3d5:ba31:58c8", Status: 1,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			LegacyID: 3, UserID: 30, IP: "185.203.56.35", CIDR: "255.255.255.240",
			Status: 1, CreatedAt: now, UpdatedAt: now,
		},
		{
			LegacyID: 4, UserID: 40, IP: "38.135.24.238", CIDR: "2602:f6f6:2:27ab::1/64",
			Status: 1, CreatedAt: now, UpdatedAt: now,
		},
	}
	mappings := map[int64]userMapping{}
	for _, legacyID := range []int64{10, 20, 30, 40} {
		mappings[legacyID] = userMapping{UserID: uuid.New(), NumericID: legacyID + 1_000}
	}

	bindings, enabled, err := buildBindings(rows, mappings)
	if err != nil {
		t.Fatal(err)
	}
	if enabled != 4 || len(bindings) != 6 {
		t.Fatalf("enabled=%d bindings=%d, want 4/6", enabled, len(bindings))
	}
	networks := make(map[string]bool, len(bindings))
	for _, item := range bindings {
		networks[item.Network] = true
	}
	for _, expected := range []string{
		"152.53.39.61/32",
		"158.101.159.166/32",
		"2603:c021:8017:e7dc:fed3:f3d5:ba31:58c8/128",
		"185.203.56.35/32",
		"38.135.24.238/32",
		"2602:f6f6:2:27ab::/64",
	} {
		if !networks[expected] {
			t.Fatalf("normalized bindings do not contain %q: %#v", expected, networks)
		}
	}
	if networks["255.255.255.240/32"] {
		t.Fatal("legacy dotted netmask became a Tracker address rule")
	}
}

func TestBuildBindingsRejectsAnActualAddressRange(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	userID := uuid.New()
	_, _, err := buildBindings([]sourceRow{{
		LegacyID: 1, UserID: 10, IPStart: "192.0.2.1", IPEnd: "192.0.2.9",
		Status: 1, CreatedAt: now, UpdatedAt: now,
	}}, map[int64]userMapping{10: {UserID: userID, NumericID: 1010}})
	if err == nil {
		t.Fatal("address range was accepted without an explicit range policy")
	}
}

func TestLegacyStandardSpeedLimitPreservesPtYesMbpsConversion(t *testing.T) {
	limit, err := legacyStandardSpeedLimit(map[string]string{
		"seedbox.non_seedbox_max_speed": "200",
	})
	if err != nil {
		t.Fatal(err)
	}
	if limit != 25*1024*1024 {
		t.Fatalf("standard speed limit = %d, want 200 Mbps / 8", limit)
	}

	limit, err = legacyStandardSpeedLimit(map[string]string{
		"seedbox.non_seedbox_max_speed": "0",
	})
	if err != nil || limit != 50*1024*1024/8 {
		t.Fatalf("default standard speed limit = %d, %v", limit, err)
	}
}
