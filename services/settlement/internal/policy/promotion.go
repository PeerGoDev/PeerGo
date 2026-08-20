package policy

import (
	"fmt"
	"sort"
	"time"
)

// PromotionType is the stable business meaning of a public torrent promotion.
// Display labels and colors remain presentation concerns.
type PromotionType string

const (
	PromotionNormal                   PromotionType = "normal"
	PromotionFree                     PromotionType = "free"
	PromotionDoubleUpload             PromotionType = "double_upload"
	PromotionDoubleUploadFree         PromotionType = "double_upload_free"
	PromotionHalfDownload             PromotionType = "half_download"
	PromotionDoubleUploadHalfDownload PromotionType = "double_upload_half_download"
	PromotionThirtyPercentDownload    PromotionType = "thirty_percent_download"
)

var promotionFactors = map[PromotionType]Factors{
	PromotionNormal:                   {Upload: OneX, Download: OneX},
	PromotionFree:                     {Upload: OneX, Download: 0},
	PromotionDoubleUpload:             {Upload: 2 * OneX, Download: OneX},
	PromotionDoubleUploadFree:         {Upload: 2 * OneX, Download: 0},
	PromotionHalfDownload:             {Upload: OneX, Download: OneX / 2},
	PromotionDoubleUploadHalfDownload: {Upload: 2 * OneX, Download: OneX / 2},
	PromotionThirtyPercentDownload:    {Upload: OneX, Download: 3_000},
}

func (promotion PromotionType) factors() (Factors, error) {
	factors, ok := promotionFactors[promotion]
	if !ok {
		return Factors{}, fmt.Errorf("%w: unknown promotion %q", ErrInvalidRule, promotion)
	}
	return factors, nil
}

// PromotionScope records where a public rule was assigned. User-specific
// entitlements are represented by Benefits and never masquerade as torrent tags.
type PromotionScope string

const (
	ScopeGlobal   PromotionScope = "global"
	ScopeCategory PromotionScope = "category"
	ScopeTorrent  PromotionScope = "torrent"
	ScopeFeatured PromotionScope = "featured"
)

// PromotionRule is an immutable, time-bounded public promotion assignment.
// OverrideLowerScopes is allowed only for a global campaign and exists for
// explicit campaigns and PtYes-compatible global override behavior.
type PromotionRule struct {
	Rule                RuleRef
	Scope               PromotionScope
	Promotion           PromotionType
	Window              Window
	OverrideLowerScopes bool
}

func (rule PromotionRule) validate() error {
	if err := rule.Rule.validate(); err != nil {
		return err
	}
	if err := rule.Window.validate(); err != nil {
		return err
	}
	if _, err := rule.Promotion.factors(); err != nil {
		return err
	}

	expectedSource := map[PromotionScope]Source{
		ScopeGlobal:   SourceGlobalCampaign,
		ScopeCategory: SourceCategoryPromotion,
		ScopeTorrent:  SourceTorrentPromotion,
		ScopeFeatured: SourceFeaturedTorrent,
	}[rule.Scope]
	if expectedSource == "" || rule.Rule.Source != expectedSource {
		return fmt.Errorf("%w: scope %q cannot use source %q", ErrInvalidRule, rule.Scope, rule.Rule.Source)
	}
	if rule.OverrideLowerScopes && rule.Scope != ScopeGlobal {
		return fmt.Errorf("%w: only a global campaign can override lower scopes", ErrInvalidRule)
	}
	return nil
}

// PromotionMatch preserves the exact contribution of one public rule, including
// redundant favorable rules that still matched the settlement context.
type PromotionMatch struct {
	Rule      RuleRef
	Promotion PromotionType
	Factors   Factors
}

func (match PromotionMatch) validate() error {
	if err := match.Rule.validate(); err != nil {
		return err
	}
	switch match.Rule.Source {
	case SourceGlobalCampaign, SourceCategoryPromotion, SourceTorrentPromotion, SourceFeaturedTorrent:
	default:
		return fmt.Errorf("%w: source %q is not a public promotion", ErrInvalidRule, match.Rule.Source)
	}
	expected, err := match.Promotion.factors()
	if err != nil {
		return err
	}
	if match.Factors != expected {
		return fmt.Errorf("%w: promotion match factors do not match %q", ErrInvalidRule, match.Promotion)
	}
	return nil
}

// ResolvedPromotion is the public promotion effective at one instant. Matches
// preserve provenance even when two favorable rules produce the same cap.
type ResolvedPromotion struct {
	Profile Profile
	Factors Factors
	Matches []PromotionMatch
}

func (resolved ResolvedPromotion) validate() error {
	if err := resolved.Profile.validate(); err != nil {
		return err
	}
	if err := resolved.Factors.validate(); err != nil {
		return err
	}
	for _, match := range resolved.Matches {
		if err := match.validate(); err != nil {
			return err
		}
	}

	expected := Factors{Upload: OneX, Download: OneX}
	if resolved.Profile == ProfilePtYesV1 {
		if len(resolved.Matches) > 1 {
			return fmt.Errorf("%w: PtYes resolution has %d selected promotions", ErrAmbiguousPromotion, len(resolved.Matches))
		}
		if len(resolved.Matches) == 1 {
			expected = resolved.Matches[0].Factors
		}
	} else {
		for _, match := range resolved.Matches {
			expected = favorable(expected, match.Factors)
		}
	}
	if resolved.Factors != expected {
		return fmt.Errorf("%w: resolved promotion factors lack matching provenance", ErrInvalidRule)
	}
	return nil
}

// ResolvePromotion applies one profile's public-promotion precedence at an
// event time. It never reads current settings, which keeps delayed replay stable.
func ResolvePromotion(profile Profile, at time.Time, rules []PromotionRule) (ResolvedPromotion, error) {
	if err := profile.validate(); err != nil {
		return ResolvedPromotion{}, err
	}
	if at.IsZero() {
		return ResolvedPromotion{}, fmt.Errorf("%w: missing resolution time", ErrInvalidPolicyWindow)
	}

	active := make([]PromotionRule, 0, len(rules))
	for _, rule := range rules {
		if err := rule.validate(); err != nil {
			return ResolvedPromotion{}, err
		}
		if rule.Promotion != PromotionNormal && rule.Window.active(at) {
			active = append(active, rule)
		}
	}
	sortPromotionRules(active)

	if profile == ProfilePtYesV1 {
		return resolvePtYesPromotion(active)
	}
	return resolvePeerGoPromotion(active)
}

func resolvePtYesPromotion(active []PromotionRule) (ResolvedPromotion, error) {
	var global, torrent *PromotionRule
	for index := range active {
		rule := &active[index]
		switch rule.Scope {
		case ScopeGlobal:
			if global != nil {
				return ResolvedPromotion{}, fmt.Errorf("%w: global", ErrAmbiguousPromotion)
			}
			global = rule
		case ScopeTorrent:
			if torrent != nil {
				return ResolvedPromotion{}, fmt.Errorf("%w: torrent", ErrAmbiguousPromotion)
			}
			torrent = rule
		default:
			return ResolvedPromotion{}, fmt.Errorf("%w: PtYes profile cannot use %q promotion", ErrUnsupportedFeature, rule.Scope)
		}
	}

	selected := global
	if selected == nil {
		selected = torrent
	}
	if selected == nil {
		return ResolvedPromotion{Profile: ProfilePtYesV1, Factors: Factors{Upload: OneX, Download: OneX}}, nil
	}
	factors, err := selected.Promotion.factors()
	if err != nil {
		return ResolvedPromotion{}, err
	}
	return ResolvedPromotion{
		Profile: ProfilePtYesV1,
		Factors: factors,
		Matches: []PromotionMatch{{Rule: selected.Rule, Promotion: selected.Promotion, Factors: factors}},
	}, nil
}

func resolvePeerGoPromotion(active []PromotionRule) (ResolvedPromotion, error) {
	var override *PromotionRule
	for index := range active {
		if !active[index].OverrideLowerScopes {
			continue
		}
		if override != nil {
			return ResolvedPromotion{}, fmt.Errorf("%w: global override", ErrAmbiguousPromotion)
		}
		override = &active[index]
	}
	if override != nil {
		factors, err := override.Promotion.factors()
		if err != nil {
			return ResolvedPromotion{}, err
		}
		return ResolvedPromotion{
			Profile: ProfilePeerGoV1,
			Factors: factors,
			Matches: []PromotionMatch{{Rule: override.Rule, Promotion: override.Promotion, Factors: factors}},
		}, nil
	}

	resolved := ResolvedPromotion{Profile: ProfilePeerGoV1, Factors: Factors{Upload: OneX, Download: OneX}}
	for _, rule := range active {
		factors, err := rule.Promotion.factors()
		if err != nil {
			return ResolvedPromotion{}, err
		}
		resolved.Factors = favorable(resolved.Factors, factors)
		resolved.Matches = append(resolved.Matches, PromotionMatch{Rule: rule.Rule, Promotion: rule.Promotion, Factors: factors})
	}
	return resolved, nil
}

func sortPromotionRules(rules []PromotionRule) {
	order := map[PromotionScope]int{ScopeGlobal: 0, ScopeCategory: 1, ScopeTorrent: 2, ScopeFeatured: 3}
	sort.Slice(rules, func(left, right int) bool {
		if order[rules[left].Scope] != order[rules[right].Scope] {
			return order[rules[left].Scope] < order[rules[right].Scope]
		}
		if rules[left].Rule.ID != rules[right].Rule.ID {
			return rules[left].Rule.ID < rules[right].Rule.ID
		}
		return rules[left].Rule.Version < rules[right].Rule.Version
	})
}

// PromotionWindows resolves every promotion interval that overlaps [start,end).
// User entitlements may add more boundaries before these windows become final
// Settlement policy slices.
func PromotionWindows(profile Profile, start, end time.Time, rules []PromotionRule) ([]ResolvedPromotionWindow, error) {
	if start.IsZero() || !end.After(start) {
		return nil, fmt.Errorf("%w: invalid requested interval", ErrInvalidPolicyWindow)
	}

	boundaries := []time.Time{start, end}
	for _, rule := range rules {
		if err := rule.validate(); err != nil {
			return nil, err
		}
		if rule.Window.StartsAt.After(start) && rule.Window.StartsAt.Before(end) {
			boundaries = append(boundaries, rule.Window.StartsAt)
		}
		if rule.Window.EndsAt != nil && rule.Window.EndsAt.After(start) && rule.Window.EndsAt.Before(end) {
			boundaries = append(boundaries, *rule.Window.EndsAt)
		}
	}
	sort.Slice(boundaries, func(left, right int) bool { return boundaries[left].Before(boundaries[right]) })

	unique := boundaries[:0]
	for _, boundary := range boundaries {
		if len(unique) == 0 || !unique[len(unique)-1].Equal(boundary) {
			unique = append(unique, boundary)
		}
	}

	windows := make([]ResolvedPromotionWindow, 0, len(unique)-1)
	for index := 0; index < len(unique)-1; index++ {
		resolved, err := ResolvePromotion(profile, unique[index], rules)
		if err != nil {
			return nil, err
		}
		windows = append(windows, ResolvedPromotionWindow{
			StartsAt:  unique[index],
			EndsAt:    unique[index+1],
			Promotion: resolved,
		})
	}
	return windows, nil
}

type ResolvedPromotionWindow struct {
	StartsAt  time.Time
	EndsAt    time.Time
	Promotion ResolvedPromotion
}
