package workgroupbenefitv1

import (
	"testing"
	"time"
)

func TestCommandRoundTrip(t *testing.T) {
	command := Command{
		SchemaVersion: SchemaVersion,
		TransitionID:  "0198f20a-6da8-7e51-9c64-111111111111",
		UserID:        "0198f20a-6da8-7e51-9c64-222222222222",
		GroupKind:     GroupRetention, Entitlement: EntitlementDownloadChargeExempt,
		Active: true, StateVersion: 1, EffectiveAt: time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC),
	}
	encoded, err := Encode(command)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || decoded != command {
		t.Fatalf("Decode() = %+v, %v", decoded, err)
	}
	if digest, err := SHA256(encoded); err != nil || digest == ([32]byte{}) {
		t.Fatalf("SHA256() = %x, %v", digest, err)
	}
}

func TestCommandRejectsWrongEntitlement(t *testing.T) {
	command := Command{
		SchemaVersion: SchemaVersion,
		TransitionID:  "0198f20a-6da8-7e51-9c64-111111111111",
		UserID:        "0198f20a-6da8-7e51-9c64-222222222222",
		GroupKind:     GroupRetention, Entitlement: "traffic.download.maybe_exempt",
		Active: true, StateVersion: 1, EffectiveAt: time.Now().UTC(),
	}
	if _, err := Encode(command); err == nil {
		t.Fatal("Encode() accepted an unknown entitlement")
	}
}
