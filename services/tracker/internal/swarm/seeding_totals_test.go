package swarm

import (
	"testing"
	"time"
)

func TestSeedingSwarmsByUserCountsDistinctCompleteOnlineTorrents(t *testing.T) {
	t.Parallel()
	engine := testEngine(t, 220, 230)
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	announce := func(request Request) {
		t.Helper()
		if _, err := engine.Announce(request); err != nil {
			t.Fatal(err)
		}
	}
	expired := testRequest("user-a", "192.0.2.1:6881", 0, now.Add(-3*time.Minute))
	expired.InfoHash[0] = 210
	announce(expired)
	incomplete := testRequest("user-a", "192.0.2.1:6881", 1, now)
	incomplete.InfoHash[0] = 211
	announce(incomplete)

	for i := byte(1); i <= 205; i++ {
		request := testRequest("user-a", "192.0.2.1:6881", 0, now)
		request.InfoHash[0] = i
		announce(request)
	}
	secondClient := testRequest("user-a", "[2001:db8::1]:6882", 0, now)
	secondClient.PeerID[0] = 2
	announce(secondClient)
	announce(testRequest("user-b", "192.0.2.2:6881", 0, now))
	announce(testRequest("unrequested-user", "192.0.2.3:6881", 0, now))

	totals, err := engine.SeedingSwarmsByUser(t.Context(), []string{"user-a", "user-b", "offline"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(totals) != 3 || len(totals["user-a"]) != 205 || len(totals["user-b"]) != 1 || len(totals["offline"]) != 0 {
		t.Fatalf("unexpected totals: users=%d a=%d b=%d offline=%d", len(totals), len(totals["user-a"]), len(totals["user-b"]), len(totals["offline"]))
	}
	for _, hash := range totals["user-a"] {
		if hash[0] > 205 {
			t.Fatalf("counted expired or incomplete swarm: %d", hash[0])
		}
	}
}
