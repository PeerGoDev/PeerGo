package policy

import (
	"testing"
	"time"
)

func TestApplyVIPBenefitTransitionsUsesGrantExpiryAndRenewalTimeline(t *testing.T) {
	t.Parallel()
	start := testNow
	firstExpiry := start.Add(10 * time.Minute)
	renewedAt := start.Add(20 * time.Minute)
	secondExpiry := start.Add(25 * time.Minute)
	baseline := Snapshot{
		Revision:  RuleRef{Source: SourcePolicyRevision, ID: "baseline", Version: 1},
		Profile:   ProfilePeerGoV1,
		Promotion: ResolvedPromotion{Profile: ProfilePeerGoV1, Factors: Factors{Upload: OneX, Download: OneX}},
	}
	transitions := []VIPBenefitTransition{
		{Rule: RuleRef{Source: SourceVIP, ID: "grant", Version: 1}, Enabled: true, ActiveUntil: &firstExpiry, EffectiveAt: start},
		{Rule: RuleRef{Source: SourceVIP, ID: "renew", Version: 2}, Enabled: true, ActiveUntil: &secondExpiry, EffectiveAt: renewedAt},
	}
	slices, err := ApplyVIPBenefitTransitions([]PolicySlice{{StartsAt: start, EndsAt: start.Add(30 * time.Minute), Snapshot: baseline}}, transitions)
	if err != nil {
		t.Fatal(err)
	}
	result, err := SettleInterval(IntervalRequest{
		StartsAt: start, EndsAt: start.Add(30 * time.Minute), RawDownloaded: 300, Slices: slices,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ChargedDownloaded != 150 {
		t.Fatalf("charged download = %d, want 150 for fifteen non-VIP minutes", result.ChargedDownloaded)
	}
}

func TestApplyVIPBenefitTransitionsSupportsPtYesCutoverProfile(t *testing.T) {
	t.Parallel()
	start := testNow
	baseline := Snapshot{
		Revision:  RuleRef{Source: SourcePolicyRevision, ID: "ptyes", Version: 1},
		Profile:   ProfilePtYesV1,
		Promotion: ResolvedPromotion{Profile: ProfilePtYesV1, Factors: Factors{Upload: OneX, Download: OneX}},
	}
	slices, err := ApplyVIPBenefitTransitions([]PolicySlice{{StartsAt: start, EndsAt: start.Add(time.Minute), Snapshot: baseline}}, []VIPBenefitTransition{{
		Rule: RuleRef{Source: SourceVIP, ID: "legacy-vip", Version: 3}, Enabled: true, EffectiveAt: start,
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := SettleInterval(IntervalRequest{StartsAt: start, EndsAt: start.Add(time.Minute), RawDownloaded: 100, Slices: slices})
	if err != nil || result.ChargedDownloaded != 0 {
		t.Fatalf("result=%+v err=%v, want VIP-free legacy-profile download", result, err)
	}
}
