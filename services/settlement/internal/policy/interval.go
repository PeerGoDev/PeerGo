package policy

import (
	"fmt"
	"math/bits"
	"sort"
	"time"
)

// PolicySlice covers a contiguous portion of an announce interval. Settlement
// receives these slices from an immutable policy timeline, never from current
// mutable settings.
type PolicySlice struct {
	StartsAt time.Time
	EndsAt   time.Time
	Snapshot Snapshot
}

type IntervalRequest struct {
	StartsAt      time.Time
	EndsAt        time.Time
	RawUploaded   uint64
	RawDownloaded uint64
	Slices        []PolicySlice
}

type SegmentResult struct {
	StartsAt time.Time
	EndsAt   time.Time
	Result   Result
}

type IntervalResult struct {
	RawUploaded       uint64
	RawDownloaded     uint64
	CreditedUploaded  uint64
	ChargedDownloaded uint64
	Segments          []SegmentResult
}

// SettleInterval proportionally allocates an absolute-counter delta when a rule
// boundary falls between two announces. BT does not reveal byte timestamps, so
// exact allocation is impossible; duration weighting is deterministic, preserves
// totals, and is less gameable than charging the whole delta at the latest rule.
func SettleInterval(request IntervalRequest) (IntervalResult, error) {
	if request.StartsAt.IsZero() || !request.EndsAt.After(request.StartsAt) || len(request.Slices) == 0 {
		return IntervalResult{}, fmt.Errorf("%w: invalid settlement interval", ErrInvalidPolicyWindow)
	}

	durations := make([]uint64, len(request.Slices))
	cursor := request.StartsAt
	for index, slice := range request.Slices {
		if !slice.StartsAt.Equal(cursor) || !slice.EndsAt.After(slice.StartsAt) || slice.EndsAt.After(request.EndsAt) {
			return IntervalResult{}, fmt.Errorf("%w: policy slices must exactly and contiguously cover the interval", ErrInvalidPolicyWindow)
		}
		durations[index] = uint64(slice.EndsAt.Sub(slice.StartsAt))
		cursor = slice.EndsAt
	}
	if !cursor.Equal(request.EndsAt) {
		return IntervalResult{}, fmt.Errorf("%w: policy slices do not reach interval end", ErrInvalidPolicyWindow)
	}

	uploadShares := proportionalShares(request.RawUploaded, durations)
	downloadShares := proportionalShares(request.RawDownloaded, durations)
	result := IntervalResult{RawUploaded: request.RawUploaded, RawDownloaded: request.RawDownloaded, Segments: make([]SegmentResult, 0, len(request.Slices))}
	for index, slice := range request.Slices {
		segment, err := SettleDelta(slice.Snapshot, uploadShares[index], downloadShares[index])
		if err != nil {
			return IntervalResult{}, fmt.Errorf("settle policy slice %d: %w", index, err)
		}
		result.CreditedUploaded, err = safeAdd(result.CreditedUploaded, segment.CreditedUploaded)
		if err != nil {
			return IntervalResult{}, err
		}
		result.ChargedDownloaded, err = safeAdd(result.ChargedDownloaded, segment.ChargedDownloaded)
		if err != nil {
			return IntervalResult{}, err
		}
		result.Segments = append(result.Segments, SegmentResult{StartsAt: slice.StartsAt, EndsAt: slice.EndsAt, Result: segment})
	}
	return result, nil
}

type remainderRank struct {
	index     int
	remainder uint64
}

// proportionalShares uses largest remainders so segment bytes always add back
// to the exact raw delta. Equal remainders prefer the earlier interval.
func proportionalShares(total uint64, weights []uint64) []uint64 {
	shares := make([]uint64, len(weights))
	ranks := make([]remainderRank, len(weights))
	var denominator uint64
	for _, weight := range weights {
		denominator += weight
	}

	var allocated uint64
	for index, weight := range weights {
		high, low := bits.Mul64(total, weight)
		share, remainder := bits.Div64(high, low, denominator)
		shares[index] = share
		allocated += share
		ranks[index] = remainderRank{index: index, remainder: remainder}
	}
	sort.SliceStable(ranks, func(left, right int) bool { return ranks[left].remainder > ranks[right].remainder })
	remaining := total - allocated
	for offset := uint64(0); offset < remaining; offset++ {
		shares[ranks[offset%uint64(len(ranks))].index]++
	}
	return shares
}
