package policy

import "time"

var testNow = time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)

func testRule(source Source, id string, version uint64) RuleRef {
	return RuleRef{Source: source, ID: id, Version: version}
}

func testPromotionRule(source Source, scope PromotionScope, id string, promotion PromotionType, start time.Time, end *time.Time) PromotionRule {
	return PromotionRule{
		Rule:      testRule(source, id, 1),
		Scope:     scope,
		Promotion: promotion,
		Window:    Window{StartsAt: start, EndsAt: end},
	}
}

func testSnapshot(profile Profile, revision string, promotion ResolvedPromotion) Snapshot {
	return Snapshot{
		Revision:  testRule(SourcePolicyRevision, revision, 1),
		Profile:   profile,
		Promotion: promotion,
	}
}

func normalPromotion(profile Profile) ResolvedPromotion {
	return ResolvedPromotion{Profile: profile, Factors: Factors{Upload: OneX, Download: OneX}}
}
