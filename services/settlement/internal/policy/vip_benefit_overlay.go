package policy

import (
	"fmt"
	"sort"
	"time"
)

// VIPBenefitTransition is one immutable account-tier state change. ActiveUntil
// is evaluated at interval time, so an expired VIP never requires a mutable
// current-state lookup or a best-effort expiry job.
type VIPBenefitTransition struct {
	Rule        RuleRef
	Enabled     bool
	ActiveUntil *time.Time
	EffectiveAt time.Time
}

// ApplyVIPBenefitTransitions overlays VIP free-download state after public and
// workgroup policies. It changes charged download only; raw download remains
// available to H&R and anti-abuse domains as independent evidence.
func ApplyVIPBenefitTransitions(slices []PolicySlice, transitions []VIPBenefitTransition) ([]PolicySlice, error) {
	if len(slices) == 0 {
		return nil, fmt.Errorf("%w: missing baseline policy slices", ErrInvalidPolicyWindow)
	}
	ordered := append([]VIPBenefitTransition(nil), transitions...)
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left].EffectiveAt.Equal(ordered[right].EffectiveAt) {
			return ordered[left].Rule.Version < ordered[right].Rule.Version
		}
		return ordered[left].EffectiveAt.Before(ordered[right].EffectiveAt)
	})
	var previousVersion uint64
	for _, transition := range ordered {
		if transition.EffectiveAt.IsZero() || transition.Rule.Source != SourceVIP ||
			transition.Rule.validate() != nil || transition.Rule.Version <= previousVersion ||
			(transition.ActiveUntil != nil && (!transition.Enabled || transition.ActiveUntil.IsZero())) {
			return nil, fmt.Errorf("%w: invalid VIP benefit transition", ErrInvalidRule)
		}
		previousVersion = transition.Rule.Version
	}

	result := make([]PolicySlice, 0, len(slices)+len(ordered)*2)
	for _, slice := range slices {
		if slice.StartsAt.IsZero() || !slice.EndsAt.After(slice.StartsAt) || slice.Snapshot.validate() != nil {
			return nil, fmt.Errorf("%w: invalid baseline policy slice", ErrInvalidPolicyWindow)
		}
		boundaries := []time.Time{slice.StartsAt, slice.EndsAt}
		for _, transition := range ordered {
			if transition.EffectiveAt.After(slice.StartsAt) && transition.EffectiveAt.Before(slice.EndsAt) {
				boundaries = append(boundaries, transition.EffectiveAt)
			}
			if transition.ActiveUntil != nil && transition.ActiveUntil.After(slice.StartsAt) && transition.ActiveUntil.Before(slice.EndsAt) {
				boundaries = append(boundaries, *transition.ActiveUntil)
			}
		}
		sort.Slice(boundaries, func(left, right int) bool { return boundaries[left].Before(boundaries[right]) })
		for index := 0; index < len(boundaries)-1; index++ {
			if boundaries[index].Equal(boundaries[index+1]) {
				continue
			}
			snapshot := slice.Snapshot
			if current, ok := vipBenefitAt(boundaries[index], ordered); ok {
				if current.Enabled && (current.ActiveUntil == nil || boundaries[index].Before(*current.ActiveUntil)) {
					snapshot.Benefits.AccountTier = &FactorGrant{
						Rule: current.Rule, Factors: Factors{Upload: OneX, Download: 0},
					}
				} else {
					snapshot.Benefits.AccountTier = nil
				}
			}
			if err := snapshot.validate(); err != nil {
				return nil, fmt.Errorf("%w: overlay VIP benefit: %v", ErrInvalidRule, err)
			}
			result = append(result, PolicySlice{StartsAt: boundaries[index], EndsAt: boundaries[index+1], Snapshot: snapshot})
		}
	}
	return result, nil
}

func vipBenefitAt(at time.Time, transitions []VIPBenefitTransition) (VIPBenefitTransition, bool) {
	var current VIPBenefitTransition
	found := false
	for _, transition := range transitions {
		if transition.EffectiveAt.After(at) {
			break
		}
		current = transition
		found = true
	}
	return current, found
}
