package attendance

import (
	"crypto/sha256"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	maximumReward     = int64(1_000_000)
	maximumMilestones = 32
)

var revisionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type policySnapshotDocument struct {
	Revision            string            `json:"revision"`
	EffectiveFrom       string            `json:"effective_from"`
	Enabled             bool              `json:"enabled"`
	DayBoundaryTimezone string            `json:"day_boundary_timezone"`
	FixedEnabled        bool              `json:"fixed_enabled"`
	FixedReward         int64             `json:"fixed_reward"`
	RandomEnabled       bool              `json:"random_enabled"`
	RandomMin           int64             `json:"random_min"`
	RandomMax           int64             `json:"random_max"`
	StreakEnabled       bool              `json:"streak_enabled"`
	StreakMilestones    []StreakMilestone `json:"streak_milestones"`
	ExperienceReward    int64             `json:"experience_reward"`
}

func NormalizePolicy(policy PolicyRevision) (PolicyRevision, []byte, error) {
	policy.Revision = strings.TrimSpace(policy.Revision)
	policy.DayBoundaryTimezone = strings.TrimSpace(policy.DayBoundaryTimezone)
	policy.EffectiveFrom = canonicalTime(policy.EffectiveFrom)
	policy.CreatedAt = canonicalTime(policy.CreatedAt)
	policy.StreakMilestones = slices.Clone(policy.StreakMilestones)
	slices.SortFunc(policy.StreakMilestones, func(left, right StreakMilestone) int {
		return int(left.Days - right.Days)
	})
	if !validPolicy(policy) {
		return PolicyRevision{}, nil, ErrInput
	}
	document := policySnapshotDocument{
		Revision: policy.Revision, EffectiveFrom: policy.EffectiveFrom.Format(time.RFC3339Nano),
		Enabled: policy.Enabled, DayBoundaryTimezone: policy.DayBoundaryTimezone,
		FixedEnabled: policy.FixedEnabled, FixedReward: policy.FixedReward,
		RandomEnabled: policy.RandomEnabled, RandomMin: policy.RandomMin, RandomMax: policy.RandomMax,
		StreakEnabled: policy.StreakEnabled, StreakMilestones: policy.StreakMilestones,
		ExperienceReward: policy.ExperienceReward,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return PolicyRevision{}, nil, ErrInvariant
	}
	digest := sha256.Sum256(encoded)
	if policy.SnapshotSHA256 != ([32]byte{}) && policy.SnapshotSHA256 != digest {
		return PolicyRevision{}, nil, ErrPolicyConflict
	}
	policy.SnapshotSHA256 = digest
	return policy, encoded, nil
}

func validPolicy(policy PolicyRevision) bool {
	if !revisionPattern.MatchString(policy.Revision) || policy.EffectiveFrom.IsZero() ||
		policy.CreatedAt.IsZero() || policy.CreatedAt.After(policy.EffectiveFrom) ||
		len(policy.DayBoundaryTimezone) > 64 || policy.DayBoundaryTimezone == "" ||
		len(policy.StreakMilestones) > maximumMilestones || policy.ExperienceReward < 0 ||
		policy.ExperienceReward > maximumReward ||
		(policy.Enabled && !policy.FixedEnabled && !policy.RandomEnabled) ||
		(policy.FixedEnabled && (policy.FixedReward < 1 || policy.FixedReward > maximumReward)) ||
		(policy.RandomEnabled && (policy.RandomMin < 1 || policy.RandomMin > policy.RandomMax || policy.RandomMax > maximumReward)) {
		return false
	}
	if _, err := time.LoadLocation(policy.DayBoundaryTimezone); err != nil {
		return false
	}
	for index, milestone := range policy.StreakMilestones {
		if milestone.Days < 2 || milestone.Days > 365 || milestone.Reward < 1 || milestone.Reward > maximumReward ||
			(index > 0 && milestone.Days == policy.StreakMilestones[index-1].Days) {
			return false
		}
	}
	return true
}

func canonicalTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func nextLocalMidnight(now time.Time, timezone string) (time.Time, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, ErrInput
	}
	local := now.In(location)
	next := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, location)
	return canonicalTime(next), nil
}
