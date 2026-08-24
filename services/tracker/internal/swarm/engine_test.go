package swarm

import (
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/peergo/peergo/services/tracker/internal/protocol"
)

func TestEngineUpsertsSamplesTransitionsAndStopsPeers(t *testing.T) {
	t.Parallel()
	engine := testEngine(t, 10, 10)
	now := time.Date(2026, 8, 8, 22, 0, 0, 0, time.UTC)
	first := testRequest("user-a", "192.0.2.1:6881", 100, now)
	result, err := engine.Announce(first)
	if err != nil || result.Complete != 0 || result.Incomplete != 1 || len(result.Peers) != 0 {
		t.Fatalf("first result=%+v error=%v", result, err)
	}
	second := testRequest("user-b", "[2001:db8::2]:51413", 0, now.Add(time.Second))
	second.PeerID[0] = 2
	result, err = engine.Announce(second)
	if err != nil || result.Complete != 1 || result.Incomplete != 1 || len(result.Peers) != 1 ||
		result.Peers[0].Endpoint != first.Endpoint {
		t.Fatalf("second result=%+v error=%v", result, err)
	}
	first.Left = 0
	first.Now = now.Add(2 * time.Second)
	result, err = engine.Announce(first)
	if err != nil || result.Complete != 2 || result.Incomplete != 0 || len(result.Peers) != 1 {
		t.Fatalf("transition result=%+v error=%v", result, err)
	}
	second.Event = protocol.EventStopped
	second.NumWant = 0
	second.Now = now.Add(3 * time.Second)
	result, err = engine.Announce(second)
	if err != nil || result.Complete != 1 || result.Incomplete != 0 || len(result.Peers) != 0 {
		t.Fatalf("stop result=%+v error=%v", result, err)
	}
}

func TestEngineExpiresPeersAndEnforcesCapacity(t *testing.T) {
	t.Parallel()
	engine := testEngine(t, 1, 2)
	now := time.Date(2026, 8, 8, 22, 0, 0, 0, time.UTC)
	first := testRequest("user-a", "192.0.2.1:6881", 1, now)
	if _, err := engine.Announce(first); err != nil {
		t.Fatal(err)
	}
	second := testRequest("user-b", "192.0.2.2:6882", 1, now)
	second.InfoHash[0] = 2
	if _, err := engine.Announce(second); !errors.Is(err, ErrCapacity) {
		t.Fatalf("swarm capacity error = %v", err)
	}
	replacement := testRequest("user-c", "192.0.2.3:6883", 1, now.Add(3*time.Minute))
	replacement.PeerID[0] = 3
	if _, err := engine.Announce(replacement); err != nil {
		t.Fatalf("expired replacement error = %v", err)
	}
	swarms, peers := engine.Counts()
	if swarms != 1 || peers != 1 {
		t.Fatalf("counts = %d swarms, %d peers", swarms, peers)
	}
}

func TestEngineBackgroundSweepReleasesDormantSwarms(t *testing.T) {
	t.Parallel()
	engine := testEngine(t, 10, 10)
	now := time.Date(2026, 8, 8, 22, 0, 0, 0, time.UTC)
	for index := byte(1); index <= 3; index++ {
		request := testRequest("user-a", "192.0.2.1:6881", 1, now)
		request.InfoHash[0] = index
		if _, err := engine.Announce(request); err != nil {
			t.Fatal(err)
		}
	}
	if err := engine.Sweep(now.Add(3*time.Minute), 10); err != nil {
		t.Fatal(err)
	}
	if swarms, peers := engine.Counts(); swarms != 0 || peers != 0 {
		t.Fatalf("counts after sweep = %d swarms, %d peers", swarms, peers)
	}
}

func TestEngineMarksOnlyKnownCompletionTransitionsAndSnapshotsStableCounts(t *testing.T) {
	t.Parallel()
	engine := testEngine(t, 10, 10)
	now := time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC)
	leecher := testRequest("user-a", "192.0.2.1:6881", 100, now)
	leecher.Downloaded = 100
	if result, err := engine.Announce(leecher); err != nil || result.CompletionToken != ([32]byte{}) {
		t.Fatalf("initial leecher result=%+v error=%v", result, err)
	}
	leecher.Left = 0
	leecher.Downloaded = 1_000
	leecher.Event = protocol.EventCompleted
	leecher.Now = now.Add(time.Second)
	firstCompletion, err := engine.Announce(leecher)
	if err != nil || firstCompletion.CompletionToken == ([32]byte{}) {
		t.Fatalf("completion result=%+v error=%v", firstCompletion, err)
	}
	leecher.Now = now.Add(2 * time.Second)
	if result, err := engine.Announce(leecher); err != nil || result.CompletionToken != firstCompletion.CompletionToken {
		t.Fatalf("completion retry result=%+v error=%v", result, err)
	}
	leecher.Event = protocol.EventNone
	leecher.Left = 100
	leecher.Now = now.Add(3 * time.Second)
	if result, err := engine.Announce(leecher); err != nil || result.CompletionToken != ([32]byte{}) {
		t.Fatalf("new leecher cycle result=%+v error=%v", result, err)
	}
	leecher.Event = protocol.EventCompleted
	leecher.Left = 0
	leecher.Now = now.Add(4 * time.Second)
	if result, err := engine.Announce(leecher); err != nil || result.CompletionToken == ([32]byte{}) || result.CompletionToken == firstCompletion.CompletionToken {
		t.Fatalf("second completion cycle result=%+v error=%v", result, err)
	}
	firstSeenSeeder := testRequest("user-b", "192.0.2.2:6882", 0, now.Add(5*time.Second))
	firstSeenSeeder.PeerID[0] = 2
	firstSeenSeeder.Downloaded = 2_000
	firstSeenSeeder.Event = protocol.EventCompleted
	if result, err := engine.Announce(firstSeenSeeder); err != nil || result.CompletionToken != ([32]byte{}) {
		t.Fatalf("first-seen seeder result=%+v error=%v", result, err)
	}
	otherSwarm := testRequest("user-c", "192.0.2.3:6883", 1, now.Add(6*time.Second))
	otherSwarm.InfoHash[0] = 2
	otherSwarm.PeerID[0] = 3
	if _, err := engine.Announce(otherSwarm); err != nil {
		t.Fatal(err)
	}
	entries := engine.Snapshot()
	if len(entries) != 2 || entries[0].InfoHash[0] != 1 || entries[0].Seeders != 2 || entries[0].Leechers != 0 ||
		entries[1].InfoHash[0] != 2 || entries[1].Seeders != 0 || entries[1].Leechers != 1 {
		t.Fatalf("snapshot=%+v", entries)
	}
}

func TestEngineReturnsBoundedPrivacyMinimizedActivePeers(t *testing.T) {
	t.Parallel()
	engine := testEngine(t, 10, 10)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	for index, user := range []string{"user-a", "user-b", "user-c"} {
		endpoint := "192.0.2.1:6881"
		if user == "user-c" {
			endpoint = "[2001:db8::3]:6881"
		}
		request := testRequest(user, endpoint, int64(index), now.Add(time.Duration(index)*time.Second))
		request.PeerID[0] = byte(index + 1)
		request.Uploaded = int64(index + 10)
		request.Downloaded = int64(index + 20)
		request.Seedbox = user == "user-c"
		if _, err := engine.Announce(request); err != nil {
			t.Fatal(err)
		}
	}
	updated := testRequest("user-c", "[2001:db8::3]:6881", 2, now.Add(4*time.Second))
	updated.PeerID[0] = 3
	updated.Uploaded = 212
	updated.Downloaded = 122
	updated.Seedbox = true
	if _, err := engine.Announce(updated); err != nil {
		t.Fatal(err)
	}
	peers, truncated := engine.ActivePeers([20]byte{1}, now.Add(5*time.Second), 2)
	if !truncated || len(peers) != 2 || peers[0].UserID != "user-c" || peers[0].Uploaded != 212 ||
		peers[0].Downloaded != 122 || peers[0].UploadSpeed != 100 || peers[0].DownloadSpeed != 50 ||
		peers[0].AddressFamily != 6 || !peers[0].Seedbox {
		t.Fatalf("ActivePeers() = %+v, truncated=%v", peers, truncated)
	}
}

func testEngine(t *testing.T, maxSwarms int64, maxPeers int64) *Engine {
	t.Helper()
	engine, err := NewEngine(Config{
		ShardCount: 4, MaxSwarms: maxSwarms, MaxPeers: maxPeers,
		MaxPeersPerSwarm: 10, PeerTTL: 2 * time.Minute, SweepBudget: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func testRequest(user, endpoint string, left int64, now time.Time) Request {
	return Request{
		InfoHash: [20]byte{1}, UserID: user, PeerID: [20]byte{1},
		Endpoint: netip.MustParseAddrPort(endpoint), ClientFamily: "unknown",
		Left: left, NumWant: 50, Now: now,
	}
}
