package policy

import (
	"fmt"
	"time"
)

const (
	PtYesTimeFollowGlobal = 0
	PtYesTimePermanent    = 1
	PtYesTimeUntil        = 2
)

// PtYesPromotionType maps the seven legacy IDs without importing the old model
// or its float64 multipliers into the new accounting domain.
func PtYesPromotionType(typeID int) (PromotionType, error) {
	mapping := map[int]PromotionType{
		1: PromotionNormal,
		2: PromotionFree,
		3: PromotionDoubleUpload,
		4: PromotionDoubleUploadFree,
		5: PromotionHalfDownload,
		6: PromotionDoubleUploadHalfDownload,
		7: PromotionThirtyPercentDownload,
	}
	promotion, ok := mapping[typeID]
	if !ok {
		return "", fmt.Errorf("%w: unknown PtYes promotion type %d", ErrInvalidRule, typeID)
	}
	return promotion, nil
}

type PtYesMappingState string

const (
	PtYesMappingFollowGlobal PtYesMappingState = "follow_global"
	PtYesMappingNormal       PtYesMappingState = "normal"
	PtYesMappingActive       PtYesMappingState = "active"
	PtYesMappingScheduled    PtYesMappingState = "scheduled"
	PtYesMappingExpired      PtYesMappingState = "expired"
)

// PtYesMapping separates migration evidence from an active assignment. Expired
// and follow-global source rows remain auditable but do not create fake rules.
type PtYesMapping struct {
	State     PtYesMappingState
	Promotion PromotionType
	Rule      *PromotionRule
}

// MapPtYesTorrentPromotion maps one legacy torrent at the cutover instant.
// Historical settled traffic is deliberately not recomputed.
func MapPtYesTorrentPromotion(reference RuleRef, typeID, timeType int, cutover time.Time, until *time.Time) (PtYesMapping, error) {
	if err := reference.validate(); err != nil {
		return PtYesMapping{}, err
	}
	if reference.Source != SourceTorrentPromotion {
		return PtYesMapping{}, fmt.Errorf("%w: PtYes torrent promotion has source %q", ErrInvalidRule, reference.Source)
	}
	if cutover.IsZero() {
		return PtYesMapping{}, fmt.Errorf("%w: missing cutover", ErrInvalidPolicyWindow)
	}

	promotion, err := PtYesPromotionType(typeID)
	if err != nil {
		return PtYesMapping{}, err
	}
	switch timeType {
	case PtYesTimeFollowGlobal:
		return PtYesMapping{State: PtYesMappingFollowGlobal, Promotion: promotion}, nil
	case PtYesTimePermanent:
	case PtYesTimeUntil:
		if until == nil {
			return PtYesMapping{}, fmt.Errorf("%w: timed PtYes promotion has no end", ErrInvalidPolicyWindow)
		}
	default:
		return PtYesMapping{}, fmt.Errorf("%w: unknown PtYes promotion time type %d", ErrInvalidRule, timeType)
	}
	if promotion == PromotionNormal {
		return PtYesMapping{State: PtYesMappingNormal, Promotion: promotion}, nil
	}

	window := Window{StartsAt: cutover}
	switch timeType {
	case PtYesTimePermanent:
	case PtYesTimeUntil:
		if !until.After(cutover) {
			return PtYesMapping{State: PtYesMappingExpired, Promotion: promotion}, nil
		}
		end := *until
		window.EndsAt = &end
	}

	rule := PromotionRule{Rule: reference, Scope: ScopeTorrent, Promotion: promotion, Window: window}
	if err := rule.validate(); err != nil {
		return PtYesMapping{}, err
	}
	return PtYesMapping{State: PtYesMappingActive, Promotion: promotion, Rule: &rule}, nil
}

// MapPtYesGlobalPromotion converts the legacy begin/end settings into one
// explicit global override. A future begin remains scheduled; an already active
// campaign starts at cutover because historical balances are migration evidence,
// not input to a fresh replay.
func MapPtYesGlobalPromotion(reference RuleRef, typeID int, cutover time.Time, beginsAt, endsAt *time.Time) (PtYesMapping, error) {
	if err := reference.validate(); err != nil {
		return PtYesMapping{}, err
	}
	if reference.Source != SourceGlobalCampaign {
		return PtYesMapping{}, fmt.Errorf("%w: PtYes global promotion has source %q", ErrInvalidRule, reference.Source)
	}
	if cutover.IsZero() {
		return PtYesMapping{}, fmt.Errorf("%w: missing cutover", ErrInvalidPolicyWindow)
	}
	promotion, err := PtYesPromotionType(typeID)
	if err != nil {
		return PtYesMapping{}, err
	}
	if promotion == PromotionNormal {
		return PtYesMapping{State: PtYesMappingNormal, Promotion: promotion}, nil
	}
	if endsAt != nil && !endsAt.After(cutover) {
		return PtYesMapping{State: PtYesMappingExpired, Promotion: promotion}, nil
	}

	start := cutover
	state := PtYesMappingActive
	if beginsAt != nil && beginsAt.After(cutover) {
		start = *beginsAt
		state = PtYesMappingScheduled
	}
	if endsAt != nil && !endsAt.After(start) {
		return PtYesMapping{}, fmt.Errorf("%w: global promotion ends before it starts", ErrInvalidPolicyWindow)
	}

	window := Window{StartsAt: start}
	if endsAt != nil {
		end := *endsAt
		window.EndsAt = &end
	}
	rule := PromotionRule{
		Rule: reference, Scope: ScopeGlobal, Promotion: promotion, Window: window, OverrideLowerScopes: true,
	}
	if err := rule.validate(); err != nil {
		return PtYesMapping{}, err
	}
	return PtYesMapping{State: state, Promotion: promotion, Rule: &rule}, nil
}
