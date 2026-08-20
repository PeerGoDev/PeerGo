// Package hnr derives one obligation's current progress from immutable raw
// Tracker intervals. It has no database or wall-clock dependency so replay and
// reconciliation use exactly the same rules as the live worker.
package hnr

import (
	"errors"
	"math/big"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/settlementhnrv1"
)

var ErrInvariant = errors.New("H&R progress invariant failed")

type Obligation struct {
	ID                       uuid.UUID
	CompletedAt              time.Time
	InitialUploaded          int64
	RawDownloaded            int64
	RequiredSeedSeconds      int64
	RequiredRatioBasisPoints int64
	MaxIntervalCreditSeconds int64
	State                    settlementhnrv1.State
	SeededSeconds            int64
	RawUploaded              int64
	RawRatioBasisPoints      int64
	LastEvidenceAt           time.Time
}

type RawInterval struct {
	EventID      uuid.UUID
	StartsAt     time.Time
	EndsAt       time.Time
	PreviousLeft int64
	CurrentLeft  int64
	RawUploaded  int64
}

type Progress struct {
	State               settlementhnrv1.State
	SeededSeconds       int64
	RawUploaded         int64
	RawRatioBasisPoints int64
	SatisfiedBy         *settlementhnrv1.SatisfiedBy
	SatisfiedAt         *time.Time
	LastEvidenceAt      time.Time
}

type creditedRange struct {
	startsAt time.Time
	endsAt   time.Time
}

// Evaluate merges capped seeding ranges before counting them. Two clients
// seeding the same torrent concurrently therefore contribute wall-clock time
// once, while their trustworthy raw uploaded byte deltas remain additive.
func Evaluate(obligation Obligation, intervals []RawInterval) (Progress, error) {
	if obligation.ID == uuid.Nil || obligation.CompletedAt.IsZero() || obligation.InitialUploaded < 0 ||
		obligation.RawDownloaded < 0 || obligation.RequiredSeedSeconds < 0 ||
		obligation.RequiredRatioBasisPoints < 0 || obligation.MaxIntervalCreditSeconds < 60 ||
		obligation.State != settlementhnrv1.StateTracking {
		return Progress{}, ErrInvariant
	}
	ordered := append([]RawInterval(nil), intervals...)
	sort.Slice(ordered, func(left, right int) bool {
		if !ordered[left].EndsAt.Equal(ordered[right].EndsAt) {
			return ordered[left].EndsAt.Before(ordered[right].EndsAt)
		}
		return ordered[left].EventID.String() < ordered[right].EventID.String()
	})

	ranges := make([]creditedRange, 0, len(ordered))
	rawUploaded := obligation.InitialUploaded
	lastEvidenceAt := obligation.CompletedAt
	var ratioSatisfiedAt *time.Time
	if ratioReached(rawUploaded, obligation.RawDownloaded, obligation.RequiredRatioBasisPoints) {
		value := obligation.CompletedAt.UTC().Round(0)
		ratioSatisfiedAt = &value
	}
	for _, interval := range ordered {
		if interval.EventID == uuid.Nil || interval.StartsAt.IsZero() || !interval.EndsAt.After(interval.StartsAt) ||
			interval.PreviousLeft < 0 || interval.CurrentLeft < 0 || interval.RawUploaded < 0 {
			return Progress{}, ErrInvariant
		}
		if !interval.EndsAt.After(obligation.CompletedAt) {
			continue
		}
		if interval.EndsAt.After(lastEvidenceAt) {
			lastEvidenceAt = interval.EndsAt
		}
		// Byte counters cannot be divided across a completion boundary without
		// inventing when bytes moved. Only wholly subsequent intervals count.
		if !interval.StartsAt.Before(obligation.CompletedAt) {
			var ok bool
			rawUploaded, ok = safeAdd(rawUploaded, interval.RawUploaded)
			if !ok {
				return Progress{}, ErrInvariant
			}
			if ratioSatisfiedAt == nil && ratioReached(rawUploaded, obligation.RawDownloaded, obligation.RequiredRatioBasisPoints) {
				value := interval.EndsAt.UTC().Round(0)
				ratioSatisfiedAt = &value
			}
		}
		if interval.PreviousLeft != 0 || interval.CurrentLeft != 0 {
			continue
		}
		startsAt := interval.StartsAt
		if startsAt.Before(obligation.CompletedAt) {
			startsAt = obligation.CompletedAt
		}
		endsAt := interval.EndsAt
		maximumEnd := startsAt.Add(time.Duration(obligation.MaxIntervalCreditSeconds) * time.Second)
		if endsAt.After(maximumEnd) {
			endsAt = maximumEnd
		}
		if endsAt.After(startsAt) {
			ranges = append(ranges, creditedRange{startsAt: startsAt, endsAt: endsAt})
		}
	}

	seededSeconds, seedSatisfiedAt, err := mergeRanges(ranges, obligation.RequiredSeedSeconds)
	if err != nil {
		return Progress{}, err
	}
	ratio := ratioBasisPoints(rawUploaded, obligation.RawDownloaded)
	result := Progress{
		State: settlementhnrv1.StateTracking, SeededSeconds: seededSeconds,
		RawUploaded: rawUploaded, RawRatioBasisPoints: ratio, LastEvidenceAt: lastEvidenceAt.UTC().Round(0),
	}
	chooseSatisfaction(&result, seedSatisfiedAt, ratioSatisfiedAt)
	return result, nil
}

func mergeRanges(ranges []creditedRange, requiredSeconds int64) (int64, *time.Time, error) {
	if len(ranges) == 0 {
		return 0, nil, nil
	}
	sort.Slice(ranges, func(left, right int) bool {
		if !ranges[left].startsAt.Equal(ranges[right].startsAt) {
			return ranges[left].startsAt.Before(ranges[right].startsAt)
		}
		return ranges[left].endsAt.Before(ranges[right].endsAt)
	})
	merged := make([]creditedRange, 0, len(ranges))
	for _, current := range ranges {
		if len(merged) == 0 || current.startsAt.After(merged[len(merged)-1].endsAt) {
			merged = append(merged, current)
			continue
		}
		if current.endsAt.After(merged[len(merged)-1].endsAt) {
			merged[len(merged)-1].endsAt = current.endsAt
		}
	}
	var total int64
	var satisfiedAt *time.Time
	for _, current := range merged {
		seconds := int64(current.endsAt.Sub(current.startsAt) / time.Second)
		if seconds < 0 || total > int64(^uint64(0)>>1)-seconds {
			return 0, nil, ErrInvariant
		}
		if satisfiedAt == nil && requiredSeconds > 0 && total+seconds >= requiredSeconds {
			value := current.startsAt.Add(time.Duration(requiredSeconds-total) * time.Second).UTC().Round(0)
			satisfiedAt = &value
		}
		total += seconds
	}
	return total, satisfiedAt, nil
}

func chooseSatisfaction(result *Progress, seedAt, ratioAt *time.Time) {
	if seedAt == nil && ratioAt == nil {
		return
	}
	by := settlementhnrv1.SatisfiedBySeedTime
	at := seedAt
	if at == nil || (ratioAt != nil && ratioAt.Before(*at)) {
		by = settlementhnrv1.SatisfiedByRawRatio
		at = ratioAt
	}
	result.State = settlementhnrv1.StateSatisfied
	result.SatisfiedBy = &by
	result.SatisfiedAt = at
}

func ratioReached(uploaded, downloaded, required int64) bool {
	return required > 0 && downloaded > 0 && ratioBasisPoints(uploaded, downloaded) >= required
}

func ratioBasisPoints(uploaded, downloaded int64) int64 {
	if uploaded < 0 || downloaded <= 0 {
		return 0
	}
	numerator := new(big.Int).Mul(big.NewInt(uploaded), big.NewInt(10_000))
	result := new(big.Int).Quo(numerator, big.NewInt(downloaded))
	if !result.IsInt64() {
		return int64(^uint64(0) >> 1)
	}
	return result.Int64()
}

func safeAdd(left, right int64) (int64, bool) {
	if left < 0 || right < 0 || right > int64(^uint64(0)>>1)-left {
		return 0, false
	}
	return left + right, true
}
