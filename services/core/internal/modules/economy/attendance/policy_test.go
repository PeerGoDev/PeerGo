package attendance

import (
	"encoding/hex"
	"testing"
	"time"
)

func TestNormalizePolicyMatchesMigrationBaseline(t *testing.T) {
	t.Parallel()
	policy := PolicyRevision{
		Revision: "attendance-v1", EffectiveFrom: time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC),
		Enabled:   true, DayBoundaryTimezone: "Asia/Shanghai",
		FixedEnabled: true, FixedReward: 5,
		RandomEnabled: true, RandomMin: 1, RandomMax: 20,
		StreakEnabled: true, StreakMilestones: []StreakMilestone{
			{Days: 30, Reward: 20}, {Days: 7, Reward: 5}, {Days: 14, Reward: 10},
		},
		ExperienceReward: 5,
	}
	normalized, snapshot, err := NormalizePolicy(policy)
	if err != nil {
		t.Fatalf("NormalizePolicy() error = %v", err)
	}
	const wantDigest = "5654531bc468c7a49856105b37988d5fc85de5952c82f5d03ea31d963b6e4eda"
	if hex.EncodeToString(normalized.SnapshotSHA256[:]) != wantDigest {
		t.Fatalf("digest = %x, want %s; snapshot=%s", normalized.SnapshotSHA256, wantDigest, snapshot)
	}
	if normalized.StreakMilestones[0].Days != 7 || normalized.StreakMilestones[2].Days != 30 {
		t.Fatalf("milestones = %+v, want sorted", normalized.StreakMilestones)
	}
}

func TestNormalizePolicyRejectsUnsafeRewardShapes(t *testing.T) {
	t.Parallel()
	base := PolicyRevision{
		Revision: "attendance-test", EffectiveFrom: time.Now().UTC().Add(time.Hour),
		CreatedAt: time.Now().UTC(), Enabled: true, DayBoundaryTimezone: "Asia/Shanghai",
		FixedEnabled: true, FixedReward: 5, ExperienceReward: 5,
	}
	tests := []struct {
		name   string
		mutate func(*PolicyRevision)
	}{
		{name: "no mode", mutate: func(policy *PolicyRevision) { policy.FixedEnabled = false }},
		{name: "random reverse", mutate: func(policy *PolicyRevision) { policy.RandomEnabled, policy.RandomMin, policy.RandomMax = true, 20, 1 }},
		{name: "duplicate milestone", mutate: func(policy *PolicyRevision) {
			policy.StreakMilestones = []StreakMilestone{{Days: 7, Reward: 5}, {Days: 7, Reward: 10}}
		}},
		{name: "unknown timezone", mutate: func(policy *PolicyRevision) { policy.DayBoundaryTimezone = "Mars/Olympus" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := base
			test.mutate(&policy)
			if _, _, err := NormalizePolicy(policy); err == nil {
				t.Fatal("NormalizePolicy() error = nil")
			}
		})
	}
}
