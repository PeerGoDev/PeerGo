package seedingreward

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"slices"

	"github.com/google/uuid"
)

type BacktestReport struct {
	PolicyRevision   string
	CalculationCount int64
	UserCount        int64
	EligibleItems    int64
	ZeroRewardCount  int64
	CappedCount      int64
	TotalReward      int64
	MedianReward     int64
	P95Reward        int64
	MaximumReward    int64
	Calculations     []CalculationResult
	ReportSHA256     [32]byte
}

type backtestCalculationDocument struct {
	UserID            string `json:"user_id"`
	WindowStart       string `json:"window_start"`
	CalculationSHA256 string `json:"calculation_sha256"`
	Reward            int64  `json:"reward"`
}

type backtestDocument struct {
	PolicyRevision   string                        `json:"policy_revision"`
	PolicySHA256     string                        `json:"policy_sha256"`
	CalculationCount int64                         `json:"calculation_count"`
	UserCount        int64                         `json:"user_count"`
	EligibleItems    int64                         `json:"eligible_items"`
	ZeroRewardCount  int64                         `json:"zero_reward_count"`
	CappedCount      int64                         `json:"capped_count"`
	TotalReward      int64                         `json:"total_reward"`
	MedianReward     int64                         `json:"median_reward"`
	P95Reward        int64                         `json:"p95_reward"`
	MaximumReward    int64                         `json:"maximum_reward"`
	Calculations     []backtestCalculationDocument `json:"calculations"`
}

type userWindowKey struct {
	UserID      uuid.UUID
	WindowStart int64
}

// Backtest evaluates an explicit signed policy against supplied immutable
// evidence without creating ledger transactions. The report digest makes an
// operator-reviewed 30-day replay reproducible before that policy is issued.
func Backtest(policy PolicyRevision, inputs []CalculationInput) (BacktestReport, error) {
	policy, _, err := NormalizePolicy(policy)
	if err != nil {
		return BacktestReport{}, err
	}
	ordered := slices.Clone(inputs)
	slices.SortFunc(ordered, func(left, right CalculationInput) int {
		if left.WindowStart.Before(right.WindowStart) {
			return -1
		}
		if left.WindowStart.After(right.WindowStart) {
			return 1
		}
		return bytes.Compare(left.UserID[:], right.UserID[:])
	})
	report := BacktestReport{PolicyRevision: policy.Revision, Calculations: make([]CalculationResult, 0, len(ordered))}
	users := make(map[uuid.UUID]struct{})
	seen := make(map[userWindowKey]struct{})
	rewards := make([]int64, 0, len(ordered))
	documents := make([]backtestCalculationDocument, 0, len(ordered))
	for _, input := range ordered {
		key := userWindowKey{UserID: input.UserID, WindowStart: canonicalTime(input.WindowStart).Unix()}
		if _, duplicate := seen[key]; duplicate {
			return BacktestReport{}, ErrInput
		}
		seen[key] = struct{}{}
		result, err := Calculate(policy, input)
		if err != nil {
			return BacktestReport{}, err
		}
		if report.TotalReward > math.MaxInt64-result.Reward {
			return BacktestReport{}, ErrInvariant
		}
		report.CalculationCount++
		report.EligibleItems += int64(result.EligibleTorrentCount)
		report.TotalReward += result.Reward
		if result.Reward == 0 {
			report.ZeroRewardCount++
		}
		if result.Capped {
			report.CappedCount++
		}
		users[input.UserID] = struct{}{}
		rewards = append(rewards, result.Reward)
		report.Calculations = append(report.Calculations, result)
		documents = append(documents, backtestCalculationDocument{
			UserID: input.UserID.String(), WindowStart: canonicalTime(input.WindowStart).Format(timeRFC3339Nano),
			CalculationSHA256: hex.EncodeToString(result.CalculationSHA256[:]), Reward: result.Reward,
		})
	}
	report.UserCount = int64(len(users))
	if len(rewards) > 0 {
		slices.Sort(rewards)
		report.MedianReward = rewards[(len(rewards)-1)/2]
		p95Index := (95*len(rewards) + 99) / 100
		report.P95Reward = rewards[p95Index-1]
		report.MaximumReward = rewards[len(rewards)-1]
	}
	document := backtestDocument{
		PolicyRevision: policy.Revision, PolicySHA256: hex.EncodeToString(policy.SnapshotSHA256[:]),
		CalculationCount: report.CalculationCount, UserCount: report.UserCount,
		EligibleItems: report.EligibleItems, ZeroRewardCount: report.ZeroRewardCount,
		CappedCount: report.CappedCount, TotalReward: report.TotalReward,
		MedianReward: report.MedianReward, P95Reward: report.P95Reward,
		MaximumReward: report.MaximumReward, Calculations: documents,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return BacktestReport{}, ErrInvariant
	}
	report.ReportSHA256 = sha256.Sum256(encoded)
	return report, nil
}

const timeRFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"
