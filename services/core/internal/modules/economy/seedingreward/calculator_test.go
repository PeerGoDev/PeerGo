package seedingreward

import (
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCalculateIsCanonicalAndAddsBenefitsWithoutCompounding(t *testing.T) {
	policy := testPolicy()
	input := testCalculationInput()
	input.Items = append(input.Items, ItemInput{
		TorrentID: 9, SizeBytes: 20 << 30, PublishedAt: input.WindowEnd.Add(-8 * 7 * 24 * time.Hour),
		ActiveSeconds: 1800, RawUploadedBytes: 0, SnapshotSeeders: 7,
		TrackerEvidenceSHA256: testDigest("tracker-9"), MetadataSHA256: testDigest("metadata-9"),
	})
	input.Items[0], input.Items[1] = input.Items[1], input.Items[0]
	input.Benefits = BenefitInput{
		Revision: "benefit-v7", SnapshotSHA256: testDigest("benefit-v7"),
		VIPActive: true, MedalBonusBPS: 1_000, LevelBonusBPS: 500,
		LevelLinearTorrentBonus: 2,
	}

	result, err := Calculate(policy, input)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if result.Reward <= 0 || result.EligibleTorrentCount != 2 || result.Items[0].TorrentID != 9 || result.Items[1].TorrentID != 42 {
		t.Fatalf("result = %+v", result)
	}
	wantVIP, _ := mulDivRoundHalfUp(result.BaseRewardMilli, policy.VIPBonusBPS, 10_000)
	wantMedal, _ := mulDivRoundHalfUp(result.BaseRewardMilli, input.Benefits.MedalBonusBPS, 10_000)
	wantLevel, _ := mulDivRoundHalfUp(result.BaseRewardMilli, input.Benefits.LevelBonusBPS, 10_000)
	if result.VIPBonusMilli != wantVIP || result.MedalBonusMilli != wantMedal || result.LevelBonusMilli != wantLevel {
		t.Fatalf("bonuses compound or use a wrong base: %+v", result)
	}

	canonicalInput := input
	canonicalInput.Items = []ItemInput{input.Items[1], input.Items[0]}
	replayed, err := Calculate(policy, canonicalInput)
	if err != nil {
		t.Fatalf("canonical Calculate() error = %v", err)
	}
	if replayed.CalculationSHA256 != result.CalculationSHA256 || replayed.Reward != result.Reward {
		t.Fatalf("input order changed calculation: %x != %x", replayed.CalculationSHA256, result.CalculationSHA256)
	}
}

func TestCalculateProratesCurveAndLinearRewardByActiveSeconds(t *testing.T) {
	policy := testPolicy()
	fullInput := testCalculationInput()
	full, err := Calculate(policy, fullInput)
	if err != nil {
		t.Fatalf("full Calculate() error = %v", err)
	}
	halfInput := testCalculationInput()
	halfInput.Items[0].ActiveSeconds = 1800
	half, err := Calculate(policy, halfInput)
	if err != nil {
		t.Fatalf("half Calculate() error = %v", err)
	}
	if half.LinearRewardMilli != policy.PerTorrentHourlyMilli/2 {
		t.Fatalf("half linear reward = %d, want %d", half.LinearRewardMilli, policy.PerTorrentHourlyMilli/2)
	}
	if half.ValueScoreMicro*2 < full.ValueScoreMicro-1 || half.ValueScoreMicro*2 > full.ValueScoreMicro+1 {
		t.Fatalf("value score was not prorated: half=%d full=%d", half.ValueScoreMicro, full.ValueScoreMicro)
	}
	if half.CurveRewardMilli >= full.CurveRewardMilli || half.Reward >= full.Reward {
		t.Fatalf("partial activity did not reduce reward: half=%+v full=%+v", half, full)
	}
}

func TestCalculateCombinesOfficialAndUploadBoostAdditively(t *testing.T) {
	policy := testPolicy()
	plainInput := testCalculationInput()
	plain, err := Calculate(policy, plainInput)
	if err != nil {
		t.Fatalf("plain Calculate() error = %v", err)
	}
	boostedInput := testCalculationInput()
	boostedInput.Items[0].Official = true
	boostedInput.Items[0].RawUploadedBytes = 1
	boosted, err := Calculate(policy, boostedInput)
	if err != nil {
		t.Fatalf("boosted Calculate() error = %v", err)
	}
	// +100% official and +50% contribution means 2.5x, not PtYes's
	// order-dependent 2x*1.5=3x.
	want := plain.ValueScoreMicro * 25 / 10
	if boosted.ValueScoreMicro < want-2 || boosted.ValueScoreMicro > want+2 {
		t.Fatalf("boosted score = %d, want approximately %d", boosted.ValueScoreMicro, want)
	}
}

func TestCalculateExcludesIneligibleItemsAndAppliesIntegerCap(t *testing.T) {
	policy := testPolicy()
	policy.MaximumHourlyReward = 1
	input := testCalculationInput()
	input.Items = append(input.Items,
		ItemInput{
			TorrentID: 1, SizeBytes: policy.MinimumTorrentBytes - 1,
			PublishedAt: input.WindowStart.Add(-time.Hour), ActiveSeconds: 3600,
			TrackerEvidenceSHA256: testDigest("small-tracker"), MetadataSHA256: testDigest("small-metadata"),
		},
		ItemInput{
			TorrentID: 2, SizeBytes: policy.MinimumTorrentBytes,
			PublishedAt: input.WindowStart.Add(-time.Hour), ActiveSeconds: int64(policy.MinimumActiveSeconds - 1),
			TrackerEvidenceSHA256: testDigest("brief-tracker"), MetadataSHA256: testDigest("brief-metadata"),
		},
	)
	result, err := Calculate(policy, input)
	if err != nil {
		t.Fatalf("Calculate() error = %v", err)
	}
	if !result.Capped || result.Reward != 1 || result.UncappedReward <= result.Reward {
		t.Fatalf("cap result = %+v", result)
	}
	if result.Items[0].ExclusionReason != ExclusionTooSmall || result.Items[1].ExclusionReason != ExclusionTooBrief {
		t.Fatalf("exclusions = %+v", result.Items)
	}
	if result.ExperienceAmount != "0.02" {
		t.Fatalf("experience = %q, want 0.02", result.ExperienceAmount)
	}
}

func TestCalculateRejectsStaleSnapshotDuplicateTorrentAndBenefitOverflow(t *testing.T) {
	policy := testPolicy()
	tests := map[string]func(CalculationInput) CalculationInput{
		"stale snapshot": func(input CalculationInput) CalculationInput {
			input.SnapshotObservedAt = input.WindowEnd.Add(-time.Duration(policy.MaximumSnapshotAgeSeconds+1) * time.Second)
			return input
		},
		"duplicate torrent": func(input CalculationInput) CalculationInput {
			input.Items = append(input.Items, input.Items[0])
			return input
		},
		"benefit overflow": func(input CalculationInput) CalculationInput {
			input.Benefits.MedalBonusBPS = policy.MaximumMedalBonusBPS + 1
			return input
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Calculate(policy, mutate(testCalculationInput())); !errors.Is(err, ErrInput) {
				t.Fatalf("Calculate() error = %v, want ErrInput", err)
			}
		})
	}
}

func TestNormalizePolicyProducesCanonicalDigestAndRejectsConflict(t *testing.T) {
	policy := testPolicy()
	normalized, snapshot, err := NormalizePolicy(policy)
	if err != nil {
		t.Fatalf("NormalizePolicy() error = %v", err)
	}
	if len(snapshot) == 0 || normalized.SnapshotSHA256 == ([32]byte{}) {
		t.Fatal("canonical policy snapshot was not produced")
	}
	policy.SnapshotSHA256 = testDigest("wrong")
	if _, _, err := NormalizePolicy(policy); !errors.Is(err, ErrPolicyConflict) {
		t.Fatalf("NormalizePolicy() error = %v, want ErrPolicyConflict", err)
	}
}

func testPolicy() PolicyRevision {
	effective := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	return PolicyRevision{
		Revision: "peergo-seeding-v1", FormulaVersion: FormulaVersion,
		EffectiveFrom: effective, CreatedAt: effective.Add(-time.Hour),
		CurveHourlyCapMilli: 100_000, AgeSaturationSeconds: int64((4 * 7 * 24 * time.Hour) / time.Second),
		SeederDecay: 7, CurveScaleMilli: 300_000, SizeMultiplierBPS: 10_000,
		OfficialBonusBPS: 10_000, UploadContributionBonusBPS: 5_000,
		PerTorrentHourlyMilli: 500, BaseLinearTorrentLimit: 60, MaximumLevelTorrentBonus: 55,
		MinimumTorrentBytes: 53_687_091, MinimumActiveSeconds: 300, MaximumSnapshotAgeSeconds: 600,
		VIPBonusBPS: 2_000, MaximumMedalBonusBPS: 2_000, MaximumLevelBonusBPS: 2_000,
		MaximumHourlyReward: 500, ExperiencePerMagicBPS: 200,
	}
}

func testCalculationInput() CalculationInput {
	windowStart := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	return CalculationInput{
		UserID:      uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb015b"),
		WindowStart: windowStart, WindowEnd: windowStart.Add(time.Hour),
		WindowEvidenceSHA256: testDigest("window"),
		SnapshotID:           uuid.MustParse("019fcd83-57de-7240-a0d3-95908cdb015c"),
		SnapshotSequence:     100, SnapshotObservedAt: windowStart.Add(55 * time.Minute),
		Benefits: BenefitInput{Revision: "benefit-none-v1", SnapshotSHA256: testDigest("benefit-none")},
		Items: []ItemInput{{
			TorrentID: 42, SizeBytes: 10 << 30, PublishedAt: windowStart.Add(-4 * 7 * 24 * time.Hour),
			ActiveSeconds: 3600, SnapshotSeeders: 1,
			TrackerEvidenceSHA256: testDigest("tracker-42"), MetadataSHA256: testDigest("metadata-42"),
		}},
	}
}

func testDigest(value string) [32]byte { return sha256.Sum256([]byte(value)) }
