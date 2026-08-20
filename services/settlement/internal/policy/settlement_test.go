package policy

import (
	"errors"
	"testing"
	"time"
)

func TestSettlePeerGoCombinesBenefitsWithoutArbitraryStacking(t *testing.T) {
	t.Parallel()

	promotionRule := testPromotionRule(SourceTorrentPromotion, ScopeTorrent, "torrent-2x-50", PromotionDoubleUploadHalfDownload, testNow.Add(-time.Hour), nil)
	promotion, err := ResolvePromotion(ProfilePeerGoV1, testNow, []PromotionRule{promotionRule})
	if err != nil {
		t.Fatalf("ResolvePromotion() error = %v", err)
	}
	personal := testRule(SourcePersonalFreeleech, "personal-free-7", 1)
	token := testRule(SourceFreeleechToken, "token-user-7-torrent-9", 1)
	snapshot := testSnapshot(ProfilePeerGoV1, "traffic-policy-user-7-torrent-9", promotion)
	snapshot.Benefits = Benefits{
		Group:             &FactorGrant{Rule: testRule(SourceUserGroup, "group-power-user", 3), Factors: Factors{Upload: 15_000, Download: 7_500}},
		AccountTier:       &FactorGrant{Rule: testRule(SourceVIP, "vip-gold", 2), Factors: Factors{Upload: 25_000, Download: 5_000}},
		PersonalFreeleech: &personal,
		FreeleechToken:    &token,
		Uploader:          &FactorGrant{Rule: testRule(SourceUploader, "uploader-default", 4), Factors: Factors{Upload: 20_000, Download: OneX}},
		Medal:             &FactorGrant{Rule: testRule(SourceMedal, "medal-seeder", 2), Factors: Factors{Upload: 15_000, Download: 8_000}},
	}
	snapshot.Seedbox = &SeedboxPenalty{Rule: testRule(SourceSeedbox, "seedbox-tier-a", 5), UploadFactor: 5_000}

	result, err := SettleDelta(snapshot, 100, 100)
	if err != nil {
		t.Fatalf("SettleDelta() error = %v", err)
	}
	if result.CreditedUploaded != 125 || result.ChargedDownloaded != 0 {
		t.Fatalf("SettleDelta() credited=%d charged=%d, want 125/0", result.CreditedUploaded, result.ChargedDownloaded)
	}
	if len(result.Applications) != 8 {
		t.Fatalf("SettleDelta() applications = %d, want 8", len(result.Applications))
	}
}

func TestSettlePeerGoDoesNotMultiplyPromotionAndMedal(t *testing.T) {
	t.Parallel()

	promotion, err := ResolvePromotion(ProfilePeerGoV1, testNow, []PromotionRule{
		testPromotionRule(SourceTorrentPromotion, ScopeTorrent, "torrent-double", PromotionDoubleUpload, testNow.Add(-time.Hour), nil),
	})
	if err != nil {
		t.Fatalf("ResolvePromotion() error = %v", err)
	}
	snapshot := testSnapshot(ProfilePeerGoV1, "peergo-no-stack", promotion)
	snapshot.Benefits.Medal = &FactorGrant{Rule: testRule(SourceMedal, "medal-150", 1), Factors: Factors{Upload: 15_000, Download: OneX}}

	result, err := SettleDelta(snapshot, 100, 0)
	if err != nil {
		t.Fatalf("SettleDelta() error = %v", err)
	}
	if result.CreditedUploaded != 200 {
		t.Fatalf("SettleDelta() credited upload = %d, want 200 rather than multiplied 300", result.CreditedUploaded)
	}
}

func TestSettleRejectsPromotionWithoutMatchingProvenance(t *testing.T) {
	t.Parallel()

	snapshot := testSnapshot(ProfilePeerGoV1, "unexplained-free", ResolvedPromotion{
		Profile: ProfilePeerGoV1,
		Factors: Factors{Upload: OneX, Download: 0},
	})
	_, err := SettleDelta(snapshot, 100, 100)
	if !errors.Is(err, ErrInvalidRule) {
		t.Fatalf("SettleDelta() error = %v, want ErrInvalidRule", err)
	}
}

func TestSettlePtYesPreservesLegacyOrderAndRounding(t *testing.T) {
	t.Parallel()

	promotion, err := ResolvePromotion(ProfilePtYesV1, testNow, []PromotionRule{
		testPromotionRule(SourceTorrentPromotion, ScopeTorrent, "legacy-2x-50", PromotionDoubleUploadHalfDownload, testNow.Add(-time.Hour), nil),
	})
	if err != nil {
		t.Fatalf("ResolvePromotion() error = %v", err)
	}
	snapshot := testSnapshot(ProfilePtYesV1, "ptyes-cutover-v1", promotion)
	snapshot.Benefits.Uploader = &FactorGrant{Rule: testRule(SourceUploader, "legacy-uploader", 1), Factors: Factors{Upload: 30_000, Download: OneX}}
	snapshot.Benefits.Medal = &FactorGrant{Rule: testRule(SourceMedal, "legacy-medal", 1), Factors: Factors{Upload: 15_000, Download: 5_000}}
	snapshot.Seedbox = &SeedboxPenalty{Rule: testRule(SourceSeedbox, "legacy-seedbox", 1), UploadFactor: 5_000}

	result, err := SettleDelta(snapshot, 100, 100)
	if err != nil {
		t.Fatalf("SettleDelta() error = %v", err)
	}
	if result.CreditedUploaded != 225 || result.ChargedDownloaded != 25 {
		t.Fatalf("SettleDelta() credited=%d charged=%d, want 225/25", result.CreditedUploaded, result.ChargedDownloaded)
	}
	if got := result.Applications[2].Operation; got != OperationMultiply {
		t.Fatalf("medal operation = %q, want %q", got, OperationMultiply)
	}
}

func TestSettlePtYesRejectsNewEntitlementSemantics(t *testing.T) {
	t.Parallel()

	snapshot := testSnapshot(ProfilePtYesV1, "ptyes-cutover-v1", normalPromotion(ProfilePtYesV1))
	snapshot.Benefits.AccountTier = &FactorGrant{Rule: testRule(SourceVIP, "new-vip", 1), Factors: Factors{Upload: 2 * OneX, Download: OneX}}

	_, err := SettleDelta(snapshot, 100, 100)
	if !errors.Is(err, ErrUnsupportedFeature) {
		t.Fatalf("SettleDelta() error = %v, want ErrUnsupportedFeature", err)
	}
}

func TestSpeedPenaltyOverridesFreeleechAndBonuses(t *testing.T) {
	t.Parallel()

	promotion, err := ResolvePromotion(ProfilePeerGoV1, testNow, []PromotionRule{
		testPromotionRule(SourceTorrentPromotion, ScopeTorrent, "torrent-2x-free", PromotionDoubleUploadFree, testNow.Add(-time.Hour), nil),
	})
	if err != nil {
		t.Fatalf("ResolvePromotion() error = %v", err)
	}
	personal := testRule(SourcePersonalFreeleech, "personal-free", 1)
	snapshot := testSnapshot(ProfilePeerGoV1, "speed-penalty-policy", promotion)
	snapshot.Benefits.PersonalFreeleech = &personal
	snapshot.Speed = &SpeedPenalty{
		Rule: testRule(SourceSpeedPenalty, "speed-observation-9", 1), SuppressUpload: true, DownloadFactor: 2 * OneX,
	}

	result, err := SettleDelta(snapshot, 100, 100)
	if err != nil {
		t.Fatalf("SettleDelta() error = %v", err)
	}
	if result.CreditedUploaded != 0 || result.ChargedDownloaded != 200 {
		t.Fatalf("SettleDelta() credited=%d charged=%d, want 0/200", result.CreditedUploaded, result.ChargedDownloaded)
	}
	if len(result.Applications) != 1 || result.Applications[0].Rule.Source != SourceSpeedPenalty {
		t.Fatalf("SettleDelta() applications = %+v, want penalty override only", result.Applications)
	}
}

func TestSettleIntervalSplitsTrafficAcrossPromotionBoundary(t *testing.T) {
	t.Parallel()

	middle := testNow.Add(30 * time.Minute)
	end := testNow.Add(time.Hour)
	first := testSnapshot(ProfilePeerGoV1, "normal-window", normalPromotion(ProfilePeerGoV1))
	freePromotion, err := ResolvePromotion(ProfilePeerGoV1, middle, []PromotionRule{
		testPromotionRule(SourceTorrentPromotion, ScopeTorrent, "free-window", PromotionFree, middle, &end),
	})
	if err != nil {
		t.Fatalf("ResolvePromotion() error = %v", err)
	}
	second := testSnapshot(ProfilePeerGoV1, "free-window", freePromotion)

	result, err := SettleInterval(IntervalRequest{
		StartsAt: testNow, EndsAt: end, RawUploaded: 101, RawDownloaded: 100,
		Slices: []PolicySlice{
			{StartsAt: testNow, EndsAt: middle, Snapshot: first},
			{StartsAt: middle, EndsAt: end, Snapshot: second},
		},
	})
	if err != nil {
		t.Fatalf("SettleInterval() error = %v", err)
	}
	if result.CreditedUploaded != 101 || result.ChargedDownloaded != 50 {
		t.Fatalf("SettleInterval() credited=%d charged=%d, want 101/50", result.CreditedUploaded, result.ChargedDownloaded)
	}
	if result.Segments[0].Result.RawUploaded != 51 || result.Segments[1].Result.RawUploaded != 50 {
		t.Fatalf("SettleInterval() upload shares = %d/%d, want 51/50", result.Segments[0].Result.RawUploaded, result.Segments[1].Result.RawUploaded)
	}
}

func TestSettleIntervalRejectsPolicyGap(t *testing.T) {
	t.Parallel()

	snapshot := testSnapshot(ProfilePeerGoV1, "normal", normalPromotion(ProfilePeerGoV1))
	_, err := SettleInterval(IntervalRequest{
		StartsAt: testNow, EndsAt: testNow.Add(time.Hour), RawDownloaded: 100,
		Slices: []PolicySlice{{StartsAt: testNow.Add(time.Minute), EndsAt: testNow.Add(time.Hour), Snapshot: snapshot}},
	})
	if !errors.Is(err, ErrInvalidPolicyWindow) {
		t.Fatalf("SettleInterval() error = %v, want ErrInvalidPolicyWindow", err)
	}
}
