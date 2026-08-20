package workgroups

import (
	"testing"
	"time"
)

func TestTaskTimelineStateUsesInclusiveTaskWindow(t *testing.T) {
	t.Parallel()
	startsAt := time.Date(2026, time.August, 18, 0, 0, 0, 0, time.UTC)
	dueAt := startsAt.Add(24 * time.Hour)
	tests := []struct {
		name string
		asOf time.Time
		want TaskTimelineState
	}{
		{name: "scheduled", asOf: startsAt.Add(-time.Microsecond), want: TaskScheduled},
		{name: "starts inclusive", asOf: startsAt, want: TaskOpen},
		{name: "due inclusive", asOf: dueAt, want: TaskOpen},
		{name: "closed", asOf: dueAt.Add(time.Microsecond), want: TaskClosed},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := taskTimelineState(startsAt, dueAt, testCase.asOf); got != testCase.want {
				t.Fatalf("taskTimelineState() = %s, want %s", got, testCase.want)
			}
		})
	}
}
