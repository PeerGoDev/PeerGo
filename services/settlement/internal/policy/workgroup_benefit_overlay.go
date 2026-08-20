package policy

import (
	"fmt"
	"sort"
	"time"
)

// WorkgroupBenefitTransition is one immutable membership-state change. Rule
// identity and version are persisted into settlement evidence so an operator
// can explain exactly which workgroup transition made a download free.
type WorkgroupBenefitTransition struct {
	Rule        RuleRef
	Active      bool
	EffectiveAt time.Time
}

// ApplyWorkgroupBenefitTransitions overlays retention membership on already
// resolved public promotion slices. It deliberately evaluates the transition
// active at each segment start instead of consulting Core's current membership.
func ApplyWorkgroupBenefitTransitions(slices []PolicySlice, transitions []WorkgroupBenefitTransition) ([]PolicySlice, error) {
	if len(slices) == 0 {
		return nil, fmt.Errorf("%w: missing baseline policy slices", ErrInvalidPolicyWindow)
	}
	ordered := append([]WorkgroupBenefitTransition(nil), transitions...)
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left].EffectiveAt.Equal(ordered[right].EffectiveAt) {
			return ordered[left].Rule.Version < ordered[right].Rule.Version
		}
		return ordered[left].EffectiveAt.Before(ordered[right].EffectiveAt)
	})
	var previousVersion uint64
	for _, transition := range ordered {
		if transition.EffectiveAt.IsZero() || transition.Rule.Source != SourceUserGroup || transition.Rule.validate() != nil || transition.Rule.Version <= previousVersion {
			return nil, fmt.Errorf("%w: invalid workgroup benefit transition", ErrInvalidRule)
		}
		previousVersion = transition.Rule.Version
	}

	result := make([]PolicySlice, 0, len(slices)+len(ordered))
	for _, slice := range slices {
		if slice.StartsAt.IsZero() || !slice.EndsAt.After(slice.StartsAt) || slice.Snapshot.validate() != nil {
			return nil, fmt.Errorf("%w: invalid baseline policy slice", ErrInvalidPolicyWindow)
		}
		boundaries := []time.Time{slice.StartsAt, slice.EndsAt}
		for _, transition := range ordered {
			if transition.EffectiveAt.After(slice.StartsAt) && transition.EffectiveAt.Before(slice.EndsAt) {
				boundaries = append(boundaries, transition.EffectiveAt)
			}
		}
		sort.Slice(boundaries, func(left, right int) bool { return boundaries[left].Before(boundaries[right]) })
		for index := 0; index < len(boundaries)-1; index++ {
			if boundaries[index].Equal(boundaries[index+1]) {
				continue
			}
			snapshot := slice.Snapshot
			if current, ok := activeWorkgroupBenefitAt(boundaries[index], ordered); ok {
				if current.Active {
					snapshot.Benefits.Group = &FactorGrant{
						Rule:    current.Rule,
						Factors: Factors{Upload: OneX, Download: 0},
					}
				} else {
					snapshot.Benefits.Group = nil
				}
			}
			if err := snapshot.validate(); err != nil {
				return nil, fmt.Errorf("%w: overlay workgroup benefit: %v", ErrInvalidRule, err)
			}
			result = append(result, PolicySlice{StartsAt: boundaries[index], EndsAt: boundaries[index+1], Snapshot: snapshot})
		}
	}
	return result, nil
}

func activeWorkgroupBenefitAt(at time.Time, transitions []WorkgroupBenefitTransition) (WorkgroupBenefitTransition, bool) {
	var current WorkgroupBenefitTransition
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
