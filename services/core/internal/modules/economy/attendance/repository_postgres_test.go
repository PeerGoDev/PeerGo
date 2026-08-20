package attendance

import "testing"

func TestStatisticsAfterOpeningContinuesOnlyFromYesterday(t *testing.T) {
	t.Parallel()
	opening := attendanceOpening{
		CurrentStreak: 7, TotalDays: 100, LongestStreak: 20,
		LastAttendanceDate: "2026-08-14",
	}
	current, total, longest, err := statisticsAfterOpening(opening, "2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	if current != 8 || total != 101 || longest != 20 {
		t.Fatalf("continued statistics = %d/%d/%d, want 8/101/20", current, total, longest)
	}

	current, total, longest, err = statisticsAfterOpening(opening, "2026-08-16")
	if err != nil {
		t.Fatal(err)
	}
	if current != 1 || total != 101 || longest != 20 {
		t.Fatalf("reset statistics = %d/%d/%d, want 1/101/20", current, total, longest)
	}
}

func TestApplyAttendanceOpeningBlocksSameDayDuplicate(t *testing.T) {
	t.Parallel()
	overview := Overview{}
	err := applyAttendanceOpening(&overview, attendanceOpening{
		CurrentStreak: 12, TotalDays: 80, LongestStreak: 30,
		LastAttendanceDate: "2026-08-14",
	}, "2026-08-14")
	if err != nil {
		t.Fatal(err)
	}
	if !overview.ClaimedToday || overview.CurrentStreak != 12 ||
		overview.TotalDays != 80 || overview.LongestStreak != 30 {
		t.Fatalf("overview = %+v", overview)
	}
}
