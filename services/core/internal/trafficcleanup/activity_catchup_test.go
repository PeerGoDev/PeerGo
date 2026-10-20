package trafficcleanup

import (
	"testing"
	"time"
)

func TestActivityCatchupKeepsUnrelatedRetentionCadence(t *testing.T) {
	cases := []struct {
		name   string
		result Result
		want   time.Duration
	}{
		{"idle", Result{}, time.Minute},
		{"traffic backlog", Result{Entries: 5000}, time.Minute},
		{"exact cache backfill", Result{ActivityCached: 128}, 10 * time.Second},
		{"lossless history packing", Result{EvidencePacked: 1500}, 10 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextMaintenanceDelay(time.Minute, tc.result, 5000); got != tc.want {
				t.Fatalf("delay=%s want=%s", got, tc.want)
			}
		})
	}
	if got := nextMaintenanceDelay(15*time.Second, Result{Entries: 5000}, 5000); got != 30*time.Second {
		t.Fatalf("traffic backoff changed: %s", got)
	}
	if got := (Result{ActivityCached: 128}).Total(); got != 128 {
		t.Fatalf("cache progress omitted: %d", got)
	}
}
