package subjectcontrol

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerpasskeyv1"
	"github.com/peergo/peergo/contracts/go/trackersubjectcontrolv1"
)

func TestStoreAuthorizesOnlyMatchingCanonicalPasskey(t *testing.T) {
	t.Parallel()
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = 0x72
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	lookupKey := []byte("tracker-passkey-lookup-key-test-2026")
	passkey := "00112233445566778899aabbccddeeff"
	lookup, err := trackerpasskeyv1.LookupHMAC(lookupKey, passkey)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 21, 0, 0, 0, time.UTC)
	artifact, err := trackersubjectcontrolv1.Sign(trackersubjectcontrolv1.Snapshot{
		GeneratedAt: now, ControlSequence: 3,
		Subjects: []trackersubjectcontrolv1.Subject{{
			UserID:     "0198f20a-6da8-7e51-9c64-111111111111",
			LookupHMAC: lookupHex(lookup), CredentialVersion: 1,
		}},
	}, "active", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(map[string]ed25519.PublicKey{"active": privateKey.Public().(ed25519.PublicKey)}, lookupKey, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.LoadArtifact(artifact.Bytes, now)
	if err != nil || !result.Activated || result.Status.SubjectCount != 1 {
		t.Fatalf("LoadArtifact() = %+v, %v", result, err)
	}
	subject, found := store.LookupPasskey(passkey)
	if !found || subject.CredentialVersion != 1 {
		t.Fatalf("LookupPasskey() = %+v, %v", subject, found)
	}
	for _, rejected := range []string{"00112233445566778899AABBCCDDEEFF", "ffffffffffffffffffffffffffffffff", "short"} {
		if _, found := store.LookupPasskey(rejected); found {
			t.Fatalf("unexpected authorization for %q", rejected)
		}
	}
	if err := store.Ready(now.Add(2*time.Minute), time.Minute); !errors.Is(err, ErrSnapshotStale) {
		t.Fatalf("Ready() error = %v", err)
	}
	if _, found := store.LookupPasskey(passkey); !found {
		t.Fatal("stale readiness destructively removed the immutable view")
	}
}

func TestStoreAuthorizesAuditedPtYesPasskeyProfile(t *testing.T) {
	t.Parallel()
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = 0x73
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	lookupKey := []byte("tracker-passkey-lookup-key-test-2026")
	passkey := "PtYesLegacyPasskey2026ABCDEF1234"
	lookup, err := trackerpasskeyv1.LookupHMACAccepted(lookupKey, passkey)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC)
	artifact, err := trackersubjectcontrolv1.Sign(trackersubjectcontrolv1.Snapshot{
		GeneratedAt:     now,
		ControlSequence: 4,
		Subjects: []trackersubjectcontrolv1.Subject{{
			UserID:            "0198f20a-6da8-7e51-9c64-222222222222",
			LookupHMAC:        lookupHex(lookup),
			CredentialVersion: 1,
		}},
	}, "active", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(
		map[string]ed25519.PublicKey{"active": privateKey.Public().(ed25519.PublicKey)},
		lookupKey,
		time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadArtifact(artifact.Bytes, now); err != nil {
		t.Fatal(err)
	}
	if subject, found := store.LookupPasskey(passkey); !found || subject.CredentialVersion != 1 {
		t.Fatalf("LookupPasskey() = %+v, %v", subject, found)
	}
	for _, rejected := range []string{
		"PtYesLegacyPasskey2026ABCDEF123!",
		"ptyeslegacypasskey2026abcdef1234",
	} {
		if _, found := store.LookupPasskey(rejected); found {
			t.Fatalf("unexpected authorization for %q", rejected)
		}
	}
}

func lookupHex(lookup [32]byte) string {
	const digits = "0123456789abcdef"
	encoded := make([]byte, len(lookup)*2)
	for index, value := range lookup {
		encoded[index*2] = digits[value>>4]
		encoded[index*2+1] = digits[value&0x0f]
	}
	return string(encoded)
}
