// Package transferpolicy contains the arithmetic and bounded limits shared by
// member gifts and content tips. Product modules still own their independent
// immutable policy timelines and receipts.
package transferpolicy

import "errors"

const (
	MaximumAmount     = int64(1_000_000_000)
	MaximumDailyLimit = int64(1_000_000_000_000)
	MaximumFeeBPS     = int32(5_000)
)

var (
	ErrInput            = errors.New("transfer policy input is invalid")
	ErrNetAmountInvalid = errors.New("transfer fee leaves no positive recipient amount")
)

type Limits struct {
	MinimumAmount   int64
	MaximumAmount   int64
	DailyGrossLimit int64
	FeeBPS          int32
}

func (limits Limits) Valid() bool {
	return limits.MinimumAmount >= 1 && limits.MinimumAmount <= limits.MaximumAmount &&
		limits.MaximumAmount <= MaximumAmount &&
		limits.DailyGrossLimit >= limits.MaximumAmount && limits.DailyGrossLimit <= MaximumDailyLimit &&
		limits.FeeBPS >= 0 && limits.FeeBPS <= MaximumFeeBPS
}

// FeeFor rounds a non-zero fee upward. Splitting one transfer into many small
// requests therefore cannot make a configured fee disappear through rounding.
func FeeFor(amount int64, feeBPS int32) (int64, error) {
	if amount < 1 || amount > MaximumAmount || feeBPS < 0 || feeBPS > MaximumFeeBPS {
		return 0, ErrInput
	}
	if feeBPS == 0 {
		return 0, nil
	}
	fee := (amount*int64(feeBPS) + 9_999) / 10_000
	if fee < 1 || fee >= amount {
		return 0, ErrNetAmountInvalid
	}
	return fee, nil
}
