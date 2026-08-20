package trackerruntimepolicyv1

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestSignedPolicyRoundTripAndTamperRejection(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Sign(validSnapshot(), "test-key", privateKey)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	verified, err := Verify(artifact.Bytes, map[string]ed25519.PublicKey{"test-key": publicKey})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.Snapshot.Policy.Revision != "tracker-default-v1" || verified.Snapshot.ControlSequence != 1 {
		t.Fatalf("unexpected snapshot: %+v", verified.Snapshot)
	}
	tampered := append([]byte(nil), artifact.Bytes...)
	tampered[len(tampered)/2] ^= 1
	if _, err := Verify(tampered, map[string]ed25519.PublicKey{"test-key": publicKey}); err == nil {
		t.Fatal("tampered artifact was accepted")
	}
}

func TestPolicyRejectsAmbiguousClientRules(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := validSnapshot()
	snapshot.Policy.ClientMode = ClientModeAllowList
	snapshot.Policy.AllowedClients = []ClientRule{
		{Family: ClientFamilyQBittorrent, MinVersion: "4.6"},
		{Family: ClientFamilyQBittorrent, MinVersion: "4.5"},
	}
	if _, err := Sign(snapshot, "test-key", privateKey); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected invalid duplicate rules, got %v", err)
	}
}

func TestNormalizePolicyPreservesEmptyJSONArrays(t *testing.T) {
	policy := validSnapshot().Policy
	policy.AllowedClients = []ClientRule{}
	policy.Seedbox.Rules = []SeedboxRule{}
	normalized, err := NormalizePolicy(policy)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"allowed_clients":null`)) || bytes.Contains(encoded, []byte(`"rules":null`)) {
		t.Fatalf("normalized arrays encoded as null: %s", encoded)
	}
}

func validSnapshot() Snapshot {
	return Snapshot{
		GeneratedAt: time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC), ControlSequence: 1,
		Policy: Policy{
			Revision: "tracker-default-v1", AnnounceIntervalSeconds: 1800,
			MinAnnounceIntervalSeconds: 900, DefaultNumWant: 50, MaxNumWant: 100,
			ScrapeEnabled: true, MaxScrapeHashes: 50, ClientMode: ClientModeAllowAll,
			AllowedClients: []ClientRule{}, UserRequestsPerMinute: 30, UserBurst: 60,
			AddressRequestsPerMinute: 120, AddressBurst: 240,
		},
	}
}
