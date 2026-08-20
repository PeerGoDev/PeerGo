package legacymedals

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/contracts/go/workgroupbenefitv1"
)

func TestDecimalToBPSPreservesRousiPrecision(t *testing.T) {
	for input, want := range map[string]int64{
		"0":      0,
		"0.005":  50,
		"0.0233": 233,
		"1.0":    10000,
	} {
		got, err := decimalToBPS(input)
		if err != nil {
			t.Fatalf("decimalToBPS(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("decimalToBPS(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestUnifiedPeriodicRewardUsesIntegerMagic(t *testing.T) {
	row := sourceMedal{
		LegacyID: 1, Name: "工作组", GetType: 4,
		PriceText: "0", UploadBonusText: "0", DownloadBonusText: "0", MagicBonusText: "0",
		RewardMagicText: "600000", RewardCreditsText: "2.5",
	}
	if err := row.normalize(1); err != nil {
		t.Fatalf("normalize(): %v", err)
	}
	if row.PeriodicRewardMagic != 612500 {
		t.Fatalf("PeriodicRewardMagic = %d, want 612500", row.PeriodicRewardMagic)
	}
}

func TestCalculateBenefitsMatchesRousiWearAndWorkgroupRules(t *testing.T) {
	cutover := time.Date(2026, time.August, 14, 11, 1, 23, 0, time.UTC)
	medals := []sourceMedal{
		{LegacyID: 1, MagicBonusBPS: 1000},
		{LegacyID: 2, MagicBonusBPS: 500},
		{LegacyID: 3, MagicBonusBPS: 300, IsWorkgroup: true},
	}
	holdings := []sourceHolding{
		{LegacyID: 1, LegacyUserID: 7, LegacyMedalID: 1, Status: 2},
		{LegacyID: 2, LegacyUserID: 7, LegacyMedalID: 2, Status: 1},
		{LegacyID: 3, LegacyUserID: 7, LegacyMedalID: 3, Status: 1},
		{LegacyID: 4, LegacyUserID: 8, LegacyMedalID: 1, Status: 2, ExpiresAt: pgtype.Timestamptz{Time: cutover, Valid: true}},
	}
	benefits, err := calculateBenefits([]int64{7, 8}, medals, holdings, sourceSettings{MaximumMagicBonusBPS: 1200}, cutover)
	if err != nil {
		t.Fatalf("calculateBenefits(): %v", err)
	}
	if benefits[0].ActiveContributingMedals != 2 || benefits[0].UncappedMagicBonusBPS != 1300 || benefits[0].MagicBonusBPS != 1200 {
		t.Fatalf("user 7 benefit = %#v", benefits[0])
	}
	if benefits[1].ActiveContributingMedals != 0 || benefits[1].MagicBonusBPS != 0 {
		t.Fatalf("user 8 benefit = %#v", benefits[1])
	}
}

func TestNormalizeJSONArrayTreatsBlankAsEmptyArray(t *testing.T) {
	blank := "  "
	got, err := normalizeJSONArray(&blank)
	if err != nil {
		t.Fatalf("normalizeJSONArray(): %v", err)
	}
	if got != "[]" {
		t.Fatalf("normalizeJSONArray(blank) = %q", got)
	}
}

func TestCalculateWorkgroupMembershipsCollapsesReviewedRousiMedals(t *testing.T) {
	cutover := time.Date(2026, time.August, 14, 11, 1, 23, 0, time.UTC)
	userID := uuid.MustParse("8d2cb6c3-ea91-4d18-93f5-7ef47e1a0301")
	medals := []sourceMedal{
		{LegacyID: 17, Name: "转种组", IsWorkgroup: true},
		{LegacyID: 18, Name: "保种组", IsWorkgroup: true},
		{LegacyID: 21, Name: "官种组", IsWorkgroup: true},
	}
	holdings := []sourceHolding{
		{LegacyID: 101, LegacyUserID: 7, LegacyMedalID: 21, Status: 1, CreatedAt: pgtype.Timestamptz{Time: cutover.Add(-24 * time.Hour), Valid: true}},
		{LegacyID: 99, LegacyUserID: 7, LegacyMedalID: 17, Status: 1, CreatedAt: pgtype.Timestamptz{Time: cutover.Add(-48 * time.Hour), Valid: true}},
		{LegacyID: 102, LegacyUserID: 7, LegacyMedalID: 18, Status: 1},
	}

	memberships, err := calculateWorkgroupMemberships(medals, holdings, map[int64]uuid.UUID{7: userID}, cutover)
	if err != nil {
		t.Fatalf("calculateWorkgroupMemberships(): %v", err)
	}
	if len(memberships) != 2 {
		t.Fatalf("memberships = %d, want 2", len(memberships))
	}
	reseed, retention := memberships[0], memberships[1]
	if reseed.GroupKind != "reseed" || reseed.StartedAt != cutover.Add(-48*time.Hour) {
		t.Fatalf("reseed membership = %#v", reseed)
	}
	if got := reseed.LegacyUserMedalIDs; len(got) != 2 || got[0] != 99 || got[1] != 101 {
		t.Fatalf("reseed origins = %v, want [99 101]", got)
	}
	if reseed.CommandJSON != nil || len(reseed.CommandSHA256) != 0 {
		t.Fatalf("reseed unexpectedly created an accounting command")
	}
	if retention.GroupKind != "retention" || retention.CommandJSON == nil || len(retention.CommandSHA256) != 32 {
		t.Fatalf("retention membership = %#v", retention)
	}
	command, err := workgroupbenefitv1.Decode([]byte(*retention.CommandJSON))
	if err != nil {
		t.Fatalf("decode retention command: %v", err)
	}
	if command.UserID != userID.String() || !command.Active || command.EffectiveAt != cutover {
		t.Fatalf("retention command = %#v", command)
	}

	retry, err := calculateWorkgroupMemberships(medals, holdings, map[int64]uuid.UUID{7: userID}, cutover)
	if err != nil || retry[0].MembershipID != reseed.MembershipID || retry[0].TransitionID != reseed.TransitionID {
		t.Fatalf("deterministic retry = %#v, err %v", retry, err)
	}
}

func TestCalculateWorkgroupMembershipsRejectsUnknownWorkgroupMedal(t *testing.T) {
	_, err := calculateWorkgroupMemberships(
		[]sourceMedal{{LegacyID: 22, Name: "未审核新工作组", IsWorkgroup: true}},
		nil,
		map[int64]uuid.UUID{},
		time.Date(2026, time.August, 14, 11, 1, 23, 0, time.UTC),
	)
	if err == nil || !strings.Contains(err.Error(), "no reviewed PeerGo mapping") {
		t.Fatalf("error = %v", err)
	}
}
