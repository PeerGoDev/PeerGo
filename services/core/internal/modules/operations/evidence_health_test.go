package operations

import (
	"testing"
	"time"
)

func TestEvidenceWindowHealth(t *testing.T) {
	t.Parallel()
	monthStart := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	expectedThrough := monthStart.Add(3 * time.Hour)
	first := monthStart
	completeThrough := expectedThrough
	laggingThrough := monthStart.Add(2 * time.Hour)
	lateStart := monthStart.Add(time.Hour)

	tests := []struct {
		name       string
		expected   int64
		complete   int64
		collecting int64
		first      *time.Time
		last       *time.Time
		want       EvidenceHealth
	}{
		{name: "no elapsed window", expected: 0, want: EvidenceHealthHealthy},
		{name: "no evidence", expected: 3, want: EvidenceHealthUnavailable},
		{name: "complete contiguous coverage", expected: 3, complete: 3, first: &first, last: &completeThrough, want: EvidenceHealthHealthy},
		{name: "recent delay", expected: 3, complete: 2, first: &first, last: &laggingThrough, want: EvidenceHealthLagging},
		{name: "historical gap", expected: 3, complete: 2, first: &lateStart, last: &completeThrough, want: EvidenceHealthBroken},
		{name: "stuck collecting window", expected: 3, complete: 2, collecting: 1, first: &first, last: &laggingThrough, want: EvidenceHealthBroken},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := evidenceWindowHealth(
				monthStart,
				expectedThrough,
				test.expected,
				test.complete,
				test.collecting,
				test.first,
				test.last,
			)
			if got != test.want {
				t.Fatalf("evidenceWindowHealth() = %q, want %q", got, test.want)
			}
		})
	}
}
