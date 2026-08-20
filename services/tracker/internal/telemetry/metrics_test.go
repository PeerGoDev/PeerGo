package telemetry

import (
	"testing"
	"time"

	"github.com/peergo/peergo/services/tracker/internal/httpserver"
	"github.com/peergo/peergo/services/tracker/internal/wal"
	"github.com/prometheus/client_golang/prometheus"
)

type swarmCountsFixture struct{ swarms, peers int64 }

func (fixture swarmCountsFixture) Counts() (int64, int64) { return fixture.swarms, fixture.peers }

type walStatsFixture struct{ stats wal.Stats }

func (fixture walStatsFixture) Stats() wal.Stats { return fixture.stats }

func TestMetricsExposeOnlyBoundedTrackerDimensions(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics, err := New(
		registry,
		swarmCountsFixture{swarms: 12, peers: 34},
		walStatsFixture{stats: wal.Stats{Bytes: 1024, UnacknowledgedBytes: 256, CapacityBytes: 4096}},
	)
	if err != nil {
		t.Fatal(err)
	}
	metrics.ObserveRequest(httpserver.RequestObservation{
		Action: "announce", Result: "ok", AddressFamily: "ipv4",
		ClientFamily: "qbittorrent", Event: "started", Duration: 25 * time.Millisecond,
	})

	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{
		"peergo_tracker_requests_total": false, "peergo_tracker_request_duration_seconds": false,
		"peergo_tracker_active_swarms": false, "peergo_tracker_active_peers": false,
		"peergo_tracker_wal_bytes": false, "peergo_tracker_wal_unacknowledged_bytes": false,
		"peergo_tracker_wal_capacity_bytes": false,
	}
	allowedLabels := map[string]bool{
		"action": true, "result": true, "address_family": true, "client_family": true,
		"event": true, "le": true,
	}
	for _, family := range families {
		name := family.GetName()
		if _, ok := wanted[name]; !ok {
			t.Fatalf("unexpected metric family %q", name)
		}
		wanted[name] = true
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if !allowedLabels[label.GetName()] {
					t.Fatalf("metric %q exposed unexpected label %q", name, label.GetName())
				}
			}
		}
	}
	for name, found := range wanted {
		if !found {
			t.Fatalf("metric family %q was not gathered", name)
		}
	}
}
