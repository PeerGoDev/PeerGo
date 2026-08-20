package control

import (
	"crypto/ed25519"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/trackercontrolv1"
)

func TestStoreVerifiesAndAtomicallyActivatesSnapshot(t *testing.T) {
	t.Parallel()
	privateKey := controlTestPrivateKey()
	store := controlTestStore(t, privateKey)
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	artifact := controlTestArtifact(t, privateKey, 4, now, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	result, err := store.LoadArtifact(artifact.Bytes, now)
	if err != nil || !result.Activated || result.Status.ControlSequence != 4 || result.Status.TorrentCount != 1 {
		t.Fatalf("LoadArtifact() = %+v, %v", result, err)
	}
	hash, _ := trackercontrolv1.DecodeInfoHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	torrent, found := store.LookupTorrent(hash)
	if !found || torrent.TorrentID != 1 {
		t.Fatalf("LookupTorrent() = %+v, %v", torrent, found)
	}
	if err := store.Ready(now.Add(time.Minute), 2*time.Minute); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
}

func TestStoreRejectsRollbackDivergenceAndFutureArtifact(t *testing.T) {
	t.Parallel()
	privateKey := controlTestPrivateKey()
	store := controlTestStore(t, privateKey)
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	current := controlTestArtifact(t, privateKey, 5, now, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if _, err := store.LoadArtifact(current.Bytes, now); err != nil {
		t.Fatal(err)
	}
	rollback := controlTestArtifact(t, privateKey, 4, now.Add(time.Second), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if _, err := store.LoadArtifact(rollback.Bytes, now.Add(time.Second)); !errors.Is(err, ErrSnapshotRollback) {
		t.Fatalf("rollback error = %v", err)
	}
	divergent := controlTestArtifact(t, privateKey, 5, now.Add(time.Second), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if _, err := store.LoadArtifact(divergent.Bytes, now.Add(time.Second)); !errors.Is(err, ErrSnapshotDivergence) {
		t.Fatalf("divergence error = %v", err)
	}
	futureStore := controlTestStore(t, privateKey)
	future := controlTestArtifact(t, privateKey, 6, now.Add(2*time.Minute), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if _, err := futureStore.LoadArtifact(future.Bytes, now); !errors.Is(err, ErrSnapshotFromFuture) {
		t.Fatalf("future error = %v", err)
	}
}

func TestStoreRefreshesSameStateAndRetainsStaleLookup(t *testing.T) {
	t.Parallel()
	privateKey := controlTestPrivateKey()
	store := controlTestStore(t, privateKey)
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	first := controlTestArtifact(t, privateKey, 4, now, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if _, err := store.LoadArtifact(first.Bytes, now); err != nil {
		t.Fatal(err)
	}
	refresh := controlTestArtifact(t, privateKey, 4, now.Add(time.Minute), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	result, err := store.LoadArtifact(refresh.Bytes, now.Add(time.Minute))
	if err != nil || !result.Activated || !result.Status.GeneratedAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("refresh = %+v, %v", result, err)
	}
	if err := store.Ready(now.Add(10*time.Minute), 2*time.Minute); !errors.Is(err, ErrSnapshotStale) {
		t.Fatalf("stale Ready() error = %v", err)
	}
	hash, _ := trackercontrolv1.DecodeInfoHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if _, found := store.LookupTorrent(hash); !found {
		t.Fatal("stale readiness destructively removed the lookup view")
	}
}

func TestStoreReadersObserveOnlyCompleteSnapshots(t *testing.T) {
	privateKey := controlTestPrivateKey()
	store := controlTestStore(t, privateKey)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	first := controlTestArtifact(t, privateKey, 1, base, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if _, err := store.LoadArtifact(first.Bytes, base); err != nil {
		t.Fatal(err)
	}
	hashA, _ := trackercontrolv1.DecodeInfoHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	hashB, _ := trackercontrolv1.DecodeInfoHash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	var wait sync.WaitGroup
	for reader := 0; reader < 16; reader++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 500; iteration++ {
				view := store.current.Load()
				_, hasA := view.torrents[hashA]
				_, hasB := view.torrents[hashB]
				if !view.status.Loaded || len(view.torrents) != 1 || hasA == hasB {
					t.Errorf("reader observed partial view: status=%+v count=%d A=%v B=%v", view.status, len(view.torrents), hasA, hasB)
					return
				}
			}
		}()
	}
	for sequence := int64(2); sequence <= 20; sequence++ {
		hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		if sequence%2 == 0 {
			hash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}
		artifact := controlTestArtifact(t, privateKey, sequence, base.Add(time.Duration(sequence)*time.Second), hash)
		if _, err := store.LoadArtifact(artifact.Bytes, base.Add(time.Duration(sequence)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	wait.Wait()
}

func controlTestStore(t *testing.T, privateKey ed25519.PrivateKey) *Store {
	t.Helper()
	store, err := NewStore(map[string]ed25519.PublicKey{"active": privateKey.Public().(ed25519.PublicKey)}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func controlTestPrivateKey() ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = 0x41
	}
	return ed25519.NewKeyFromSeed(seed)
}

func controlTestArtifact(t *testing.T, privateKey ed25519.PrivateKey, sequence int64, generatedAt time.Time, infoHash string) trackercontrolv1.SignedArtifact {
	t.Helper()
	artifact, err := trackercontrolv1.Sign(trackercontrolv1.Snapshot{
		GeneratedAt: generatedAt, ControlSequence: sequence,
		Torrents: []trackercontrolv1.Torrent{{
			TorrentID:  1,
			InfoHashV1: infoHash, TotalSizeBytes: 42, TorrentVersion: sequence, ControlSequence: sequence,
		}},
	}, "active", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}
