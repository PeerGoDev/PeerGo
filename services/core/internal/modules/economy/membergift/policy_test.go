package membergift

import (
	"testing"
	"time"
)

func TestFeeForUsesCeilingAndKeepsNetPositive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		amount  int64
		feeBPS  int32
		want    int64
		wantErr error
	}{
		{name: "free", amount: 100, feeBPS: 0, want: 0},
		{name: "exact", amount: 100, feeBPS: 500, want: 5},
		{name: "ceiling", amount: 101, feeBPS: 500, want: 6},
		{name: "net must remain positive", amount: 1, feeBPS: 1, wantErr: ErrAmountOutOfRange},
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

func TestNormalizePolicyProducesStableSnapshot(t *testing.T) {
	t.Parallel()
	policy, snapshot, err := NormalizePolicy(PolicyRevision{
		Revision: "member-gift-test", Enabled: true,
		MinimumAmount: 10, MaximumAmount: 10_000, DailyGrossLimit: 20_000,
		FeeBPS: 250, CreatedAt: time.Date(2026, time.August, 17, 8, 0, 0, 123456789, time.UTC),
	})
	if err != nil {
		t.Fatalf("NormalizePolicy() error = %v", err)
	}
	if len(snapshot) == 0 || policy.SnapshotSHA256 == ([32]byte{}) || policy.CreatedAt.Nanosecond() != 123456000 {
		t.Fatalf("policy=%+v snapshot=%s", policy, snapshot)
	}
}
