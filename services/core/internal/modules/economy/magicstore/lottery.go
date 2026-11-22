package magicstore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"time"

	"github.com/google/uuid"
)

// DrawTickets picks count distinct winning ticket numbers in 1..ticketCount.
// It is a partial Fisher-Yates shuffle driven by HMAC-SHA256(seed, round ID,
// step, attempt) with rejection sampling, so the result is unbiased and anyone
// holding the revealed seed can reproduce it exactly.
func DrawTickets(seed []byte, roundID uuid.UUID, ticketCount, count int) []int {
	if ticketCount < 1 || count < 1 {
		return nil
	}
	count = min(count, ticketCount)
	swapped := make(map[int]int, count*2)
	valueAt := func(index int) int {
		if value, found := swapped[index]; found {
			return value
		}
		return index + 1
	}
	winners := make([]int, 0, count)
	for step := range count {
		remaining := uint64(ticketCount - step)
		offset := uniformIndex(seed, roundID, step, remaining)
		target := step + int(offset)
		chosen := valueAt(target)
		swapped[target] = valueAt(step)
		swapped[step] = chosen
		winners = append(winners, chosen)
	}
	return winners
}

func uniformIndex(seed []byte, roundID uuid.UUID, step int, bound uint64) uint64 {
	// Reject the top partial bucket so every index is exactly equally likely.
	limit := ^uint64(0) - (^uint64(0) % bound)
	for attempt := uint32(0); ; attempt++ {
		mac := hmac.New(sha256.New, seed)
		mac.Write(roundID[:])
		var counter [8]byte
		binary.BigEndian.PutUint32(counter[:4], uint32(step))
		binary.BigEndian.PutUint32(counter[4:], attempt)
		mac.Write(counter[:])
		value := binary.BigEndian.Uint64(mac.Sum(nil)[:8])
		if value < limit {
			return value % bound
		}
	}
}

// TierPrize is the allocation for one prize tier of a drawn round.
type TierPrize struct {
	Label       string
	Winners     int
	PrizeEach   int64
	FilledCount int
}

// Allocation explains where every magic point of a round's pool goes.
type Allocation struct {
	Burned     int64
	PaidOut    int64
	CarriedOut int64
	Tiers      []TierPrize
}

// AllocatePrizes splits a round. The burn applies only to this round's sales;
// rollover from earlier rounds is never burned twice. Tier shares are taken
// from the remaining pool, unfilled tiers and integer remainders roll over.
func AllocatePrizes(carriedIn, salesTotal, burnBPS int64, tiers []LotteryTier, ticketsSold int) Allocation {
	pool := carriedIn + salesTotal
	burned := salesTotal * burnBPS / 10000
	if ticketsSold == 0 {
		burned = 0
	}
	distributable := pool - burned
	result := Allocation{Burned: burned, Tiers: make([]TierPrize, 0, len(tiers))}
	available := ticketsSold
	for _, tier := range tiers {
		filled := min(tier.Winners, available)
		available -= filled
		prizeEach := int64(0)
		if filled > 0 {
			prizeEach = distributable * tier.ShareBPS / 10000 / int64(tier.Winners)
		}
		result.PaidOut += prizeEach * int64(filled)
		result.Tiers = append(result.Tiers, TierPrize{
			Label: tier.Label, Winners: tier.Winners, PrizeEach: prizeEach, FilledCount: filled,
		})
	}
	result.CarriedOut = pool - burned - result.PaidOut
	return result
}

// EstimatedPrizes previews per-winner prizes if the round closed right now
// with every tier filled.
func EstimatedPrizes(carriedIn, salesTotal, burnBPS int64, tiers []LotteryTier) []int64 {
	distributable := carriedIn + salesTotal - salesTotal*burnBPS/10000
	result := make([]int64, len(tiers))
	for index, tier := range tiers {
		result[index] = distributable * tier.ShareBPS / 10000 / int64(tier.Winners)
	}
	return result
}

// NextDrawAt returns the first draw instant at drawHour local time that leaves
// at least ten minutes of sales after now.
func NextDrawAt(now time.Time, location *time.Location, drawHour int) time.Time {
	local := now.In(location)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), drawHour, 0, 0, 0, location)
	if candidate.Sub(now) < 10*time.Minute {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate.UTC()
}
