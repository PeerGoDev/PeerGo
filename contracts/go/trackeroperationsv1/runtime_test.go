package trackeroperationsv1

import (
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
)

func TestRuntimeValid(t *testing.T) {
	runtime := Runtime{
		GeneratedAt: time.Now().UTC(), PolicyGeneratedAt: time.Now().UTC(),
		PolicyControlSequence: 1, PolicyRevision: "tracker-default-v1",
		AnnounceIntervalSeconds:    1800,
		MinAnnounceIntervalSeconds: 900, DefaultNumWant: 50, MaxNumWant: 100,
		ScrapeEnabled: true, MaxScrapeHashes: 50,
		ClientMode: string(trackerruntimepolicyv1.ClientModeAllowAll), AllowedClients: []trackerruntimepolicyv1.ClientRule{},
		UserRequestsPerMinute: 30, UserBurst: 60, AddressRequestsPerMinute: 120, AddressBurst: 240,
		PeerTTLSeconds: 2100, MaxSwarms: 100000, MaxPeers: 1000000, MaxPeersPerSwarm: 100000,
	}
	if !runtime.Valid() {
		t.Fatal("expected runtime to be valid")
	}
	runtime.PeerTTLSeconds = runtime.AnnounceIntervalSeconds
	if runtime.Valid() {
		t.Fatal("peer TTL must exceed announce interval")
	}
}
