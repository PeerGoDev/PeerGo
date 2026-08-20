package runtimepolicy

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
)

func TestStoreActivatesAndRejectsRollback(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(map[string]ed25519.PublicKey{"key": publicKey}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	load := func(sequence int64, generatedAt time.Time) error {
		artifact, err := trackerruntimepolicyv1.Sign(trackerruntimepolicyv1.Snapshot{
			GeneratedAt: generatedAt, ControlSequence: sequence,
			Policy: trackerruntimepolicyv1.Policy{
				Revision: "policy-v1", AnnounceIntervalSeconds: 1800, MinAnnounceIntervalSeconds: 900,
				DefaultNumWant: 50, MaxNumWant: 100, ScrapeEnabled: true, MaxScrapeHashes: 50,
				ClientMode: trackerruntimepolicyv1.ClientModeAllowAll, AllowedClients: []trackerruntimepolicyv1.ClientRule{},
				UserRequestsPerMinute: 30, UserBurst: 60, AddressRequestsPerMinute: 120, AddressBurst: 240,
				Seedbox: trackerruntimepolicyv1.SeedboxPolicy{
					UploadFactorBasisPoints: 5_000, DownloadFactorBasisPoints: 10_000,
					Rules: []trackerruntimepolicyv1.SeedboxRule{},
				},
			},
		}, "key", privateKey)
		if err != nil {
			return err
		}
		_, err = store.LoadArtifact(artifact.Bytes, now)
		return err
	}
	if err := load(2, now); err != nil {
		t.Fatalf("load current: %v", err)
	}
	if err := load(1, now.Add(time.Second)); err != ErrRollback {
		t.Fatalf("expected rollback, got %v", err)
	}
	policy, ok := store.CurrentPolicy()
	if !ok || policy.MaxNumWant != 100 {
		t.Fatalf("unexpected policy: %+v", policy)
	}
}
