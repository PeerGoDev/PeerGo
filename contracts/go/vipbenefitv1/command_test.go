package vipbenefitv1

import (
	"testing"
	"time"
)

func TestCommandRoundTrip(t *testing.T) {
	until := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	command := Command{
		SchemaVersion: SchemaVersion,
		TransitionID:  "0198f20a-6da8-7e51-9c64-111111111111",
		UserID:        "0198f20a-6da8-7e51-9c64-222222222222",
		Entitlement:   EntitlementDownloadChargeExempt,
		Enabled:       true,
		ActiveUntil:   &until,
		StateVersion:  4,
		EffectiveAt:   time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC),
	}
	encoded, err := Encode(command)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || decoded.ActiveUntil == nil || !decoded.ActiveUntil.Equal(until) ||
		decoded.TransitionID != command.TransitionID || decoded.StateVersion != command.StateVersion {
		t.Fatalf("Decode() = %+v, %v", decoded, err)
	}
	if digest, err := SHA256(encoded); err != nil || digest == ([32]byte{}) {
		t.Fatalf("SHA256() = %x, %v", digest, err)
	}
}

func TestCommandRejectsExpiryOnDisabledState(t *testing.T) {
	effectiveAt := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	command := Command{
		SchemaVersion: SchemaVersion,
		TransitionID:  "0198f20a-6da8-7e51-9c64-111111111111",
		UserID:        "0198f20a-6da8-7e51-9c64-222222222222",
		Entitlement:   EntitlementDownloadChargeExempt,
		Enabled:       false,
		ActiveUntil:   timePointer(effectiveAt.Add(time.Hour)),
		StateVersion:  1,
		EffectiveAt:   effectiveAt,
	}
	if _, err := Encode(command); err == nil {
		t.Fatal("Encode() accepted an expiry on a disabled state")
	}
}

func timePointer(value time.Time) *time.Time { return &value }
