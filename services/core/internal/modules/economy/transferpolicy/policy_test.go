package transferpolicy

import "testing"

func TestFeeForRoundsUpAndKeepsPositiveNet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		amount  int64
		feeBPS  int32
		want    int64
		wantErr error
	}{
		{name: "free", amount: 100, feeBPS: 0, want: 0},
		{name: "round upward", amount: 101, feeBPS: 100, want: 2},
		{name: "whole percent", amount: 100, feeBPS: 100, want: 1},
		{name: "no recipient amount", amount: 1, feeBPS: 1, wantErr: ErrNetAmountInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := FeeFor(test.amount, test.feeBPS)
			if err != test.wantErr || got != test.want {
				t.Fatalf("FeeFor(%d, %d) = (%d, %v), want (%d, %v)", test.amount, test.feeBPS, got, err, test.want, test.wantErr)
			}
		})
	}
}

func TestLimitsRejectsDailyLimitBelowSingleTransfer(t *testing.T) {
	t.Parallel()
	if (Limits{MinimumAmount: 1, MaximumAmount: 100, DailyGrossLimit: 99}).Valid() {
		t.Fatal("limits with daily cap below maximum transfer must be invalid")
	}
}
