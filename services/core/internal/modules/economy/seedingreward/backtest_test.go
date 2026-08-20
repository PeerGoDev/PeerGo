package seedingreward

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBacktestProducesCanonicalDistributionReport(t *testing.T) {
	policy := testPolicy()
	first := testCalculationInput()
	second := testCalculationInput()
	second.UserID = uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb015d")
	second.WindowStart = second.WindowStart.Add(time.Hour)
	second.WindowEnd = second.WindowEnd.Add(time.Hour)
	second.SnapshotObservedAt = second.SnapshotObservedAt.Add(time.Hour)
	second.Items[0].ActiveSeconds = 1800
	third := testCalculationInput()
	third.WindowStart = third.WindowStart.Add(time.Hour)
	third.WindowEnd = third.WindowEnd.Add(time.Hour)
	third.SnapshotObservedAt = third.SnapshotObservedAt.Add(time.Hour)
	third.Items[0].ActiveSeconds = 0

	report, err := Backtest(policy, []CalculationInput{second, first, third})
	if err != nil {
		t.Fatalf("Backtest() error = %v", err)
	}
	if report.CalculationCount != 3 || report.UserCount != 2 || report.ZeroRewardCount != 1 ||
		report.TotalReward <= 0 || report.P95Reward != report.MaximumReward || report.ReportSHA256 == ([32]byte{}) {
		t.Fatalf("report = %+v", report)
	}
	replayed, err := Backtest(policy, []CalculationInput{third, second, first})
	if err != nil || replayed.ReportSHA256 != report.ReportSHA256 {
		t.Fatalf("canonical backtest = %+v, %v", replayed, err)
	}
}

func TestBacktestRejectsDuplicateUserWindow(t *testing.T) {
	input := testCalculationInput()
	if _, err := Backtest(testPolicy(), []CalculationInput{input, input}); !errors.Is(err, ErrInput) {
		t.Fatalf("Backtest() error = %v, want ErrInput", err)
	}
}
