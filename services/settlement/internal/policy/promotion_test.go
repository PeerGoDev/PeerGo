package policy

import (
	"errors"
	"testing"
	"time"
)

func TestPtYesPromotionTypeMapsAllLegacyTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id        int
		promotion PromotionType
		factors   Factors
	}{
		{id: 1, promotion: PromotionNormal, factors: Factors{Upload: OneX, Download: OneX}},
		{id: 2, promotion: PromotionFree, factors: Factors{Upload: OneX, Download: 0}},
		{id: 3, promotion: PromotionDoubleUpload, factors: Factors{Upload: 2 * OneX, Download: OneX}},
		{id: 4, promotion: PromotionDoubleUploadFree, factors: Factors{Upload: 2 * OneX, Download: 0}},
		{id: 5, promotion: PromotionHalfDownload, factors: Factors{Upload: OneX, Download: 5_000}},
		{id: 6, promotion: PromotionDoubleUploadHalfDownload, factors: Factors{Upload: 2 * OneX, Download: 5_000}},
		{id: 7, promotion: PromotionThirtyPercentDownload, factors: Factors{Upload: OneX, Download: 3_000}},
	}

	for _, test := range tests {
		promotion, err := PtYesPromotionType(test.id)
		if err != nil {
			t.Fatalf("PtYesPromotionType(%d) error = %v", test.id, err)
		}
		if promotion != test.promotion {
			t.Fatalf("PtYesPromotionType(%d) = %q, want %q", test.id, promotion, test.promotion)
		}
		factors, err := promotion.factors()
		if err != nil {
			t.Fatalf("factors(%q) error = %v", promotion, err)
		}
		if factors != test.factors {
			t.Fatalf("factors(%q) = %+v, want %+v", promotion, factors, test.factors)
		}
	}
}

func TestMapPtYesTorrentPromotionPreservesTimeSemantics(t *testing.T) {
	t.Parallel()

	reference := testRule(SourceTorrentPromotion, "legacy-torrent-42", 1)
	activeUntil := testNow.Add(time.Hour)
	expiredAt := testNow.Add(-time.Second)

	tests := []struct {
		name     string
		typeID   int
		timeType int
		until    *time.Time
		state    PtYesMappingState
		hasRule  bool
	}{
		{name: "follow global", typeID: 4, timeType: PtYesTimeFollowGlobal, state: PtYesMappingFollowGlobal},
		{name: "normal is no assignment", typeID: 1, timeType: PtYesTimePermanent, state: PtYesMappingNormal},
		{name: "permanent", typeID: 3, timeType: PtYesTimePermanent, state: PtYesMappingActive, hasRule: true},
		{name: "active until", typeID: 2, timeType: PtYesTimeUntil, until: &activeUntil, state: PtYesMappingActive, hasRule: true},
		{name: "expired", typeID: 2, timeType: PtYesTimeUntil, until: &expiredAt, state: PtYesMappingExpired},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mapping, err := MapPtYesTorrentPromotion(reference, test.typeID, test.timeType, testNow, test.until)
			if err != nil {
				t.Fatalf("MapPtYesTorrentPromotion() error = %v", err)
			}
			if mapping.State != test.state || (mapping.Rule != nil) != test.hasRule {
				t.Fatalf("mapping = %+v, want state=%q hasRule=%t", mapping, test.state, test.hasRule)
			}
		})
	}
}

func TestMapPtYesTorrentPromotionRejectsTimedRuleWithoutEnd(t *testing.T) {
	t.Parallel()

	_, err := MapPtYesTorrentPromotion(testRule(SourceTorrentPromotion, "legacy-torrent-42", 1), 2, PtYesTimeUntil, testNow, nil)
	if !errors.Is(err, ErrInvalidPolicyWindow) {
		t.Fatalf("MapPtYesTorrentPromotion() error = %v, want ErrInvalidPolicyWindow", err)
	}
}

func TestMapPtYesTorrentPromotionRejectsUnknownTimeTypeEvenWhenNormal(t *testing.T) {
	t.Parallel()

	_, err := MapPtYesTorrentPromotion(testRule(SourceTorrentPromotion, "legacy-torrent-42", 1), 1, 99, testNow, nil)
	if !errors.Is(err, ErrInvalidRule) {
		t.Fatalf("MapPtYesTorrentPromotion() error = %v, want ErrInvalidRule", err)
	}
}

func TestMapPtYesGlobalPromotionPreservesScheduleAndOverride(t *testing.T) {
	t.Parallel()

	begin := testNow.Add(time.Hour)
	end := testNow.Add(3 * time.Hour)
	mapping, err := MapPtYesGlobalPromotion(
		testRule(SourceGlobalCampaign, "legacy-global", 1), 4, testNow, &begin, &end,
	)
	if err != nil {
		t.Fatalf("MapPtYesGlobalPromotion() error = %v", err)
	}
	if mapping.State != PtYesMappingScheduled || mapping.Rule == nil {
		t.Fatalf("mapping = %+v, want scheduled rule", mapping)
	}
	if !mapping.Rule.Window.StartsAt.Equal(begin) || mapping.Rule.Window.EndsAt == nil || !mapping.Rule.Window.EndsAt.Equal(end) {
		t.Fatalf("mapped window = %+v, want [%s,%s)", mapping.Rule.Window, begin, end)
	}
	if !mapping.Rule.OverrideLowerScopes {
		t.Fatal("mapped global promotion does not preserve PtYes override semantics")
	}
}

func TestMapPtYesGlobalPromotionTreatsPastEndAsEvidenceOnly(t *testing.T) {
	t.Parallel()

	end := testNow.Add(-time.Second)
	mapping, err := MapPtYesGlobalPromotion(testRule(SourceGlobalCampaign, "legacy-global", 1), 2, testNow, nil, &end)
	if err != nil {
		t.Fatalf("MapPtYesGlobalPromotion() error = %v", err)
	}
	if mapping.State != PtYesMappingExpired || mapping.Rule != nil {
		t.Fatalf("mapping = %+v, want expired evidence without active rule", mapping)
	}
}

func TestResolvePtYesPromotionUsesGlobalOverride(t *testing.T) {
	t.Parallel()

	global := testPromotionRule(SourceGlobalCampaign, ScopeGlobal, "summer-free", PromotionFree, testNow.Add(-time.Hour), nil)
	torrent := testPromotionRule(SourceTorrentPromotion, ScopeTorrent, "torrent-double", PromotionDoubleUpload, testNow.Add(-time.Hour), nil)

	resolved, err := ResolvePromotion(ProfilePtYesV1, testNow, []PromotionRule{torrent, global})
	if err != nil {
		t.Fatalf("ResolvePromotion() error = %v", err)
	}
	if resolved.Factors != (Factors{Upload: OneX, Download: 0}) {
		t.Fatalf("ResolvePromotion() factors = %+v, want free", resolved.Factors)
	}
	if len(resolved.Matches) != 1 || resolved.Matches[0].Rule != global.Rule {
		t.Fatalf("ResolvePromotion() matches = %+v, want global only", resolved.Matches)
	}
}

func TestResolvePeerGoPromotionMergesFavorableFactorsWithoutMultiplication(t *testing.T) {
	t.Parallel()

	rules := []PromotionRule{
		testPromotionRule(SourceTorrentPromotion, ScopeTorrent, "torrent-double", PromotionDoubleUpload, testNow.Add(-time.Hour), nil),
		testPromotionRule(SourceCategoryPromotion, ScopeCategory, "animation-half", PromotionHalfDownload, testNow.Add(-time.Hour), nil),
	}
	resolved, err := ResolvePromotion(ProfilePeerGoV1, testNow, rules)
	if err != nil {
		t.Fatalf("ResolvePromotion() error = %v", err)
	}
	if resolved.Factors != (Factors{Upload: 2 * OneX, Download: 5_000}) {
		t.Fatalf("ResolvePromotion() factors = %+v, want 2x/50%%", resolved.Factors)
	}
	if len(resolved.Matches) != 2 {
		t.Fatalf("ResolvePromotion() matches = %d, want 2", len(resolved.Matches))
	}
}

func TestResolvePeerGoGlobalOverrideIsExplicit(t *testing.T) {
	t.Parallel()

	global := testPromotionRule(SourceGlobalCampaign, ScopeGlobal, "maintenance-free", PromotionFree, testNow.Add(-time.Hour), nil)
	global.OverrideLowerScopes = true
	torrent := testPromotionRule(SourceTorrentPromotion, ScopeTorrent, "torrent-double", PromotionDoubleUpload, testNow.Add(-time.Hour), nil)

	resolved, err := ResolvePromotion(ProfilePeerGoV1, testNow, []PromotionRule{torrent, global})
	if err != nil {
		t.Fatalf("ResolvePromotion() error = %v", err)
	}
	if resolved.Factors != (Factors{Upload: OneX, Download: 0}) || len(resolved.Matches) != 1 || resolved.Matches[0].Rule != global.Rule {
		t.Fatalf("ResolvePromotion() = %+v, want explicit global override", resolved)
	}
}

func TestResolvePtYesPromotionRejectsAmbiguousGlobalCampaign(t *testing.T) {
	t.Parallel()

	rules := []PromotionRule{
		testPromotionRule(SourceGlobalCampaign, ScopeGlobal, "campaign-a", PromotionFree, testNow.Add(-time.Hour), nil),
		testPromotionRule(SourceGlobalCampaign, ScopeGlobal, "campaign-b", PromotionHalfDownload, testNow.Add(-time.Hour), nil),
	}
	_, err := ResolvePromotion(ProfilePtYesV1, testNow, rules)
	if !errors.Is(err, ErrAmbiguousPromotion) {
		t.Fatalf("ResolvePromotion() error = %v, want ErrAmbiguousPromotion", err)
	}
}

func TestPromotionWindowsUseClosedOpenBoundaries(t *testing.T) {
	t.Parallel()

	starts := testNow.Add(10 * time.Minute)
	ends := testNow.Add(20 * time.Minute)
	rule := testPromotionRule(SourceTorrentPromotion, ScopeTorrent, "short-free", PromotionFree, starts, &ends)
	windows, err := PromotionWindows(ProfilePeerGoV1, testNow, testNow.Add(30*time.Minute), []PromotionRule{rule})
	if err != nil {
		t.Fatalf("PromotionWindows() error = %v", err)
	}
	if len(windows) != 3 {
		t.Fatalf("PromotionWindows() count = %d, want 3", len(windows))
	}
	wantDownloads := []BasisPoints{OneX, 0, OneX}
	for index, want := range wantDownloads {
		if windows[index].Promotion.Factors.Download != want {
			t.Fatalf("window %d download factor = %d, want %d", index, windows[index].Promotion.Factors.Download, want)
		}
	}
}
