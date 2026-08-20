package policy

import (
	"testing"
	"time"
)

func TestApplyPromotionRulesSplitsBaselineAtCampaignBoundaries(t *testing.T) {
	t.Parallel()
	start := testNow
	campaignStart := start.Add(10 * time.Minute)
	campaignEnd := start.Add(20 * time.Minute)
	baseline := Snapshot{
		Revision:  RuleRef{Source: SourcePolicyRevision, ID: "baseline", Version: 1},
		Profile:   ProfilePeerGoV1,
		Promotion: ResolvedPromotion{Profile: ProfilePeerGoV1, Factors: Factors{Upload: OneX, Download: OneX}},
	}
	rule := testPromotionRule(SourceTorrentPromotion, ScopeTorrent, "torrent-free", PromotionFree, campaignStart, &campaignEnd)
	slices, err := ApplyPromotionRules([]PolicySlice{{StartsAt: start, EndsAt: start.Add(30 * time.Minute), Snapshot: baseline}}, []PromotionRule{rule})
	if err != nil {
		t.Fatalf("ApplyPromotionRules() error = %v", err)
	}
	if len(slices) != 3 || slices[0].Snapshot.Promotion.Factors.Download != OneX ||
		slices[1].Snapshot.Promotion.Factors.Download != 0 || slices[2].Snapshot.Promotion.Factors.Download != OneX {
		t.Fatalf("ApplyPromotionRules() slices = %+v, want normal/free/normal", slices)
	}
}

func TestApplyPromotionRulesGlobalOverrideTemporarilyReplacesTorrent(t *testing.T) {
	t.Parallel()
	start := testNow
	end := start.Add(time.Hour)
	torrent := testPromotionRule(SourceTorrentPromotion, ScopeTorrent, "torrent-double", PromotionDoubleUpload, start, &end)
	globalStart := start.Add(15 * time.Minute)
	globalEnd := start.Add(30 * time.Minute)
	global := testPromotionRule(SourceGlobalCampaign, ScopeGlobal, "global-free", PromotionFree, globalStart, &globalEnd)
	global.OverrideLowerScopes = true
	baseline := Snapshot{
		Revision: RuleRef{Source: SourcePolicyRevision, ID: "baseline", Version: 1}, Profile: ProfilePeerGoV1,
		Promotion: ResolvedPromotion{Profile: ProfilePeerGoV1, Factors: Factors{Upload: OneX, Download: OneX}},
	}
	slices, err := ApplyPromotionRules([]PolicySlice{{StartsAt: start, EndsAt: end, Snapshot: baseline}}, []PromotionRule{torrent, global})
	if err != nil {
		t.Fatalf("ApplyPromotionRules() error = %v", err)
	}
	if len(slices) != 3 || slices[0].Snapshot.Promotion.Factors.Upload != 2*OneX ||
		slices[1].Snapshot.Promotion.Factors.Download != 0 || slices[1].Snapshot.Promotion.Factors.Upload != OneX ||
		slices[2].Snapshot.Promotion.Factors.Upload != 2*OneX {
		t.Fatalf("ApplyPromotionRules() does not restore torrent rule: %+v", slices)
	}
}
