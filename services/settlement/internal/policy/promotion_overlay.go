package policy

import (
	"fmt"
	"time"
)

// ApplyPromotionRules overlays append-only public promotion facts on complete
// baseline snapshots. Baseline policy revisions still own user benefits and
// penalties; campaign boundaries only replace the public-promotion component.
func ApplyPromotionRules(slices []PolicySlice, rules []PromotionRule) ([]PolicySlice, error) {
	if len(slices) == 0 {
		return nil, fmt.Errorf("%w: missing baseline policy slices", ErrInvalidPolicyWindow)
	}
	result := make([]PolicySlice, 0, len(slices)+len(rules)*2)
	for _, slice := range slices {
		if slice.StartsAt.IsZero() || !slice.EndsAt.After(slice.StartsAt) || slice.Snapshot.validate() != nil {
			return nil, fmt.Errorf("%w: invalid baseline policy slice", ErrInvalidPolicyWindow)
		}
		windows, err := PromotionWindows(slice.Snapshot.Profile, slice.StartsAt, slice.EndsAt, rules)
		if err != nil {
			return nil, err
		}
		for _, window := range windows {
			snapshot, err := overlayResolvedPromotion(slice.Snapshot, window.Promotion, activeGlobalOverride(window.StartsAt, rules))
			if err != nil {
				return nil, err
			}
			result = append(result, PolicySlice{StartsAt: window.StartsAt, EndsAt: window.EndsAt, Snapshot: snapshot})
		}
	}
	return result, nil
}

func overlayResolvedPromotion(base Snapshot, overlay ResolvedPromotion, override bool) (Snapshot, error) {
	if err := base.validate(); err != nil || overlay.validate() != nil || base.Profile != overlay.Profile {
		return Snapshot{}, fmt.Errorf("%w: promotion overlay does not match baseline", ErrInvalidRule)
	}
	if len(overlay.Matches) == 0 {
		return base, nil
	}
	if override {
		base.Promotion = overlay
		return base, nil
	}
	if base.Profile == ProfilePtYesV1 && len(base.Promotion.Matches) != 0 {
		return Snapshot{}, fmt.Errorf("%w: PtYes promotion exists in both baseline and rule timeline", ErrAmbiguousPromotion)
	}
	base.Promotion.Factors = favorable(base.Promotion.Factors, overlay.Factors)
	base.Promotion.Matches = append(base.Promotion.Matches, overlay.Matches...)
	if err := base.Promotion.validate(); err != nil {
		return Snapshot{}, err
	}
	return base, nil
}

func activeGlobalOverride(at time.Time, rules []PromotionRule) bool {
	for _, rule := range rules {
		if rule.Scope == ScopeGlobal && rule.OverrideLowerScopes && rule.Window.active(at) {
			return true
		}
	}
	return false
}
