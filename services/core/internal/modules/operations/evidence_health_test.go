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

func TestEvidenceCoveragePeriodStartsAtTrustedCutover(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 21, 6, 50, 0, 0, time.UTC)
	cutover := time.Date(2026, time.August, 21, 5, 0, 0, 0, time.UTC)
	closureDelay := 45 * time.Minute

	monthStart, coverageStart, expectedThrough, expected := evidenceCoveragePeriod(now, cutover, closureDelay)
	if want := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC); !monthStart.Equal(want) {
		t.Fatalf("month start = %s, want %s", monthStart, want)
	}
	if !coverageStart.Equal(cutover) || !expectedThrough.Equal(time.Date(2026, time.August, 21, 6, 0, 0, 0, time.UTC)) || expected != 1 {
		t.Fatalf("coverage = start:%s through:%s windows:%d", coverageStart, expectedThrough, expected)
	}
}

func TestEvidenceCoveragePeriodWaitsForClosureDelay(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 21, 6, 34, 0, 0, time.UTC)
	cutover := time.Date(2026, time.August, 21, 5, 0, 0, 0, time.UTC)

	_, coverageStart, expectedThrough, expected := evidenceCoveragePeriod(now, cutover, 45*time.Minute)
	if !coverageStart.Equal(cutover) || !expectedThrough.Equal(cutover) || expected != 0 {
		t.Fatalf("coverage = start:%s through:%s windows:%d", coverageStart, expectedThrough, expected)
	}
}

func TestEvidenceCoveragePeriodDoesNotExpectFutureCutover(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 21, 4, 34, 0, 0, time.UTC)
	cutover := time.Date(2026, time.August, 21, 5, 0, 0, 0, time.UTC)

	_, coverageStart, expectedThrough, expected := evidenceCoveragePeriod(now, cutover, 45*time.Minute)
	if !coverageStart.Equal(cutover) || !expectedThrough.Equal(time.Date(2026, time.August, 21, 3, 0, 0, 0, time.UTC)) || expected != 0 {
		t.Fatalf("coverage = start:%s through:%s windows:%d", coverageStart, expectedThrough, expected)
	}
}
