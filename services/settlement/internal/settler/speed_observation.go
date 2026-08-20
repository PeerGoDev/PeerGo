package settler

import (
	"math/big"
	"time"

	"github.com/peergo/peergo/services/settlement/internal/policy"
)

const (
	speedOutcomeWithinLimit        = "within_limit"
	speedOutcomeExceeded           = "exceeded"
	speedOutcomeVIPExempt          = "vip_exempt"
	speedOutcomePartiallyVIPExempt = "partially_vip_exempt"
)

type speedObservation struct {
	IntervalEventID             [16]byte
	IntervalDurationNanoseconds int64
	RawUploaded                 int64
	AverageBytesPerSecond       int64
	Outcome                     string
	ObservedAt                  time.Time
}

// buildSpeedObservation turns one immutable counter interval into explainable
// speed evidence. A zero threshold means that observation is disabled. VIP is
// resolved from the historical benefit slices; no current account state is
// consulted and a mixed VIP/non-VIP interval is never mislabeled as fully
// exempt.
func buildSpeedObservation(raw rawInterval, slices []policy.PolicySlice, observedAt time.Time) (*speedObservation, error) {
	if raw.NetworkEvidence == nil || raw.NetworkEvidence.SpeedLimitBytesPerSecond == 0 {
		return nil, nil
	}
	duration := raw.EndsAt.Sub(raw.StartsAt)
	if raw.EventID == [16]byte{} || raw.RawUploaded < 0 || duration <= 0 || observedAt.IsZero() || len(slices) == 0 {
		return nil, ErrInvariant
	}
	average, err := ceilingBytesPerSecond(raw.RawUploaded, duration)
	if err != nil {
		return nil, err
	}
	outcome := speedOutcomeWithinLimit
	if average > raw.NetworkEvidence.SpeedLimitBytesPerSecond {
		vipSlices := 0
		for _, slice := range slices {
			if slice.Snapshot.Benefits.AccountTier != nil && slice.Snapshot.Benefits.AccountTier.Rule.Source == policy.SourceVIP {
				vipSlices++
			}
		}
		switch {
		case vipSlices == len(slices):
			outcome = speedOutcomeVIPExempt
		case vipSlices > 0:
			outcome = speedOutcomePartiallyVIPExempt
		default:
			outcome = speedOutcomeExceeded
		}
	}
	return &speedObservation{
		IntervalEventID: raw.EventID, IntervalDurationNanoseconds: duration.Nanoseconds(),
		RawUploaded: raw.RawUploaded, AverageBytesPerSecond: average,
		Outcome: outcome, ObservedAt: observedAt.UTC().Round(0),
	}, nil
}

func ceilingBytesPerSecond(uploaded int64, duration time.Duration) (int64, error) {
	if uploaded < 0 || duration <= 0 {
		return 0, ErrInvariant
	}
	numerator := new(big.Int).Mul(big.NewInt(uploaded), big.NewInt(int64(time.Second)))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, big.NewInt(duration.Nanoseconds()), remainder)
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, ErrInvariant
	}
	return quotient.Int64(), nil
}
