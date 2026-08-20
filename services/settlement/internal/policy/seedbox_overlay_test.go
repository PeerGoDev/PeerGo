package policy

import (
	"testing"
	"time"
)

func TestApplySeedboxEvidenceComposesAfterPromotionAndVIPBenefits(t *testing.T) {
	start := time.Date(2026, 8, 18, 1, 0, 0, 0, time.UTC)
	base := testSnapshot(ProfilePeerGoV1, "baseline", ResolvedPromotion{
		Profile: ProfilePeerGoV1, Factors: Factors{Upload: OneX, Download: OneX},
	})
	slices := []PolicySlice{{StartsAt: start, EndsAt: start.Add(time.Minute), Snapshot: base}}
	evidence := &NetworkEvidence{
		PolicySequence: 7, PolicyRevision: "tracker-seedbox-v7", Class: NetworkClassSeedbox,
		RuleID: "member-box", UploadFactorBasisPoints: 5_000, DownloadFactorBasisPoints: 20_000,
		SpeedLimitBytesPerSecond: 100 << 20,
	}
	resolved, err := ApplySeedboxEvidence(slices, evidence)
	if err != nil {
		t.Fatal(err)
	}
	result, err := SettleInterval(IntervalRequest{
		StartsAt: start, EndsAt: start.Add(time.Minute), RawUploaded: 100, RawDownloaded: 100, Slices: resolved,
	})
	if err != nil || result.CreditedUploaded != 50 || result.ChargedDownloaded != 200 {
		t.Fatalf("seedbox result=%+v error=%v", result, err)
	}

	vip := base
	vip.Benefits.AccountTier = &FactorGrant{
		Rule:    RuleRef{Source: SourceVIP, ID: "vip", Version: 1},
		Factors: Factors{Upload: OneX, Download: 0},
	}
	resolved, err = ApplySeedboxEvidence([]PolicySlice{{StartsAt: start, EndsAt: start.Add(time.Minute), Snapshot: vip}}, evidence)
	if err != nil {
		t.Fatal(err)
	}
	result, err = SettleInterval(IntervalRequest{
		StartsAt: start, EndsAt: start.Add(time.Minute), RawUploaded: 100, RawDownloaded: 100, Slices: resolved,
	})
	if err != nil || result.CreditedUploaded != 50 || result.ChargedDownloaded != 0 {
		t.Fatalf("VIP seedbox result=%+v error=%v", result, err)
	}
}

func TestSeedboxFactorsDoNotReplaceTorrentPromotion(t *testing.T) {
	start := time.Date(2026, 8, 18, 2, 0, 0, 0, time.UTC)
	promotion, err := ResolvePromotion(ProfilePeerGoV1, start, []PromotionRule{
		testPromotionRule(
			SourceTorrentPromotion,
			ScopeTorrent,
			"box-promotion-2x-50",
			PromotionDoubleUploadHalfDownload,
			start.Add(-time.Hour),
			nil,
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := testSnapshot(ProfilePeerGoV1, "box-promotion", promotion)
	resolved, err := ApplySeedboxEvidence(
		[]PolicySlice{{StartsAt: start, EndsAt: start.Add(time.Minute), Snapshot: snapshot}},
		&NetworkEvidence{
			PolicySequence: 8, PolicyRevision: "tracker-seedbox-v8", Class: NetworkClassSeedbox,
			RuleID: "member-box", UploadFactorBasisPoints: 5_000,
			DownloadFactorBasisPoints: 20_000,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := SettleInterval(IntervalRequest{
		StartsAt: start, EndsAt: start.Add(time.Minute), RawUploaded: 100, RawDownloaded: 100, Slices: resolved,
	})
	if err != nil || result.CreditedUploaded != 100 || result.ChargedDownloaded != 100 {
		t.Fatalf("promoted seedbox result=%+v error=%v, want promotion 2x/0.5x then box 0.5x/2x", result, err)
	}
}
