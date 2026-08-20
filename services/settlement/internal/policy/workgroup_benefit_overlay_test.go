package policy

import (
	"testing"
	"time"
)

func TestApplyWorkgroupBenefitTransitionsUsesHistoricalMembershipWindow(t *testing.T) {
	t.Parallel()
	start := testNow
	joined := start.Add(10 * time.Minute)
	left := start.Add(20 * time.Minute)
	baseline := Snapshot{
		Revision:  RuleRef{Source: SourcePolicyRevision, ID: "baseline", Version: 1},
		Profile:   ProfilePeerGoV1,
		Promotion: ResolvedPromotion{Profile: ProfilePeerGoV1, Factors: Factors{Upload: OneX, Download: OneX}},
	}
	transitions := []WorkgroupBenefitTransition{
		{Rule: RuleRef{Source: SourceUserGroup, ID: "joined", Version: 1}, Active: true, EffectiveAt: joined},
		{Rule: RuleRef{Source: SourceUserGroup, ID: "left", Version: 2}, Active: false, EffectiveAt: left},
	}
	slices, err := ApplyWorkgroupBenefitTransitions([]PolicySlice{{StartsAt: start, EndsAt: start.Add(30 * time.Minute), Snapshot: baseline}}, transitions)
	if err != nil {
		t.Fatalf("ApplyWorkgroupBenefitTransitions() error = %v", err)
	}
	if len(slices) != 3 || slices[0].Snapshot.Benefits.Group != nil || slices[1].Snapshot.Benefits.Group == nil || slices[2].Snapshot.Benefits.Group != nil {
		t.Fatalf("slices = %+v, want normal/free/normal", slices)
	}
	result, err := SettleInterval(IntervalRequest{
		StartsAt: start, EndsAt: start.Add(30 * time.Minute), RawDownloaded: 300, Slices: slices,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ChargedDownloaded != 200 || len(result.Segments[1].Result.Applications) != 1 || result.Segments[1].Result.Applications[0].Rule.Source != SourceUserGroup {
		t.Fatalf("result = %+v, want only middle segment exempt with user_group evidence", result)
	}
}

func TestApplyWorkgroupBenefitTransitionsSupportsPtYesCutoverProfile(t *testing.T) {
	t.Parallel()
	start := testNow
	baseline := Snapshot{
		Revision:  RuleRef{Source: SourcePolicyRevision, ID: "ptyes", Version: 1},
		Profile:   ProfilePtYesV1,
		Promotion: ResolvedPromotion{Profile: ProfilePtYesV1, Factors: Factors{Upload: OneX, Download: OneX}},
	}
	slices, err := ApplyWorkgroupBenefitTransitions([]PolicySlice{{StartsAt: start, EndsAt: start.Add(time.Minute), Snapshot: baseline}}, []WorkgroupBenefitTransition{{
		Rule: RuleRef{Source: SourceUserGroup, ID: "retention", Version: 1}, Active: true, EffectiveAt: start,
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := SettleInterval(IntervalRequest{StartsAt: start, EndsAt: start.Add(time.Minute), RawDownloaded: 100, Slices: slices})
	if err != nil || result.ChargedDownloaded != 0 {
		t.Fatalf("result=%+v err=%v, want free legacy-profile download", result, err)
	}
}
