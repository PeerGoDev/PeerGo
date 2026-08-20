package seedingreward

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/big"
	"slices"
	"strings"
	"time"
)

const (
	bytesPerGiB = float64(1 << 30)
	scoreScale  = float64(1_000_000)
	milliScale  = int64(1_000)
)

type calculationItemDocument struct {
	TorrentID             int64  `json:"torrent_id"`
	SizeBytes             int64  `json:"size_bytes"`
	PublishedAt           string `json:"published_at"`
	ActiveSeconds         int64  `json:"active_seconds"`
	RawUploadedBytes      int64  `json:"raw_uploaded_bytes"`
	SnapshotSeeders       int32  `json:"snapshot_seeders"`
	Official              bool   `json:"official"`
	TrackerEvidenceSHA256 string `json:"tracker_evidence_sha256"`
	MetadataSHA256        string `json:"metadata_sha256"`
	Eligible              bool   `json:"eligible"`
	ExclusionReason       string `json:"exclusion_reason,omitempty"`
	ValueScoreMicro       int64  `json:"value_score_micro"`
}

type calculationDocument struct {
	FormulaVersion       string                    `json:"formula_version"`
	PolicyRevision       string                    `json:"policy_revision"`
	PolicySHA256         string                    `json:"policy_sha256"`
	UserID               string                    `json:"user_id"`
	WindowStart          string                    `json:"window_start"`
	WindowEnd            string                    `json:"window_end"`
	WindowEvidenceSHA256 string                    `json:"window_evidence_sha256"`
	SnapshotID           string                    `json:"snapshot_id"`
	SnapshotSequence     int64                     `json:"snapshot_sequence"`
	SnapshotObservedAt   string                    `json:"snapshot_observed_at"`
	BenefitRevision      string                    `json:"benefit_revision"`
	BenefitSHA256        string                    `json:"benefit_sha256"`
	VIPActive            bool                      `json:"vip_active"`
	MedalBonusBPS        int64                     `json:"medal_bonus_bps"`
	LevelBonusBPS        int64                     `json:"level_bonus_bps"`
	LevelTorrentBonus    int32                     `json:"level_torrent_bonus"`
	Items                []calculationItemDocument `json:"items"`
	CurveRewardMilli     int64                     `json:"curve_reward_milli"`
	LinearRewardMilli    int64                     `json:"linear_reward_milli"`
	VIPBonusMilli        int64                     `json:"vip_bonus_milli"`
	MedalBonusMilli      int64                     `json:"medal_bonus_milli"`
	LevelBonusMilli      int64                     `json:"level_bonus_milli"`
	UncappedReward       int64                     `json:"uncapped_reward"`
	Reward               int64                     `json:"reward"`
	ExperienceAmount     string                    `json:"experience_amount"`
	Capped               bool                      `json:"capped"`
}

// Calculate evaluates one user and one closed UTC hour. It is side-effect
// free: preview, backfill audit and the ledger worker must all call this exact
// function so the same inputs produce the same canonical digest.
func Calculate(policy PolicyRevision, input CalculationInput) (CalculationResult, error) {
	policy, _, err := NormalizePolicy(policy)
	if err != nil {
		return CalculationResult{}, err
	}
	input, err = normalizeCalculationInput(input, policy)
	if err != nil {
		return CalculationResult{}, err
	}

	result := CalculationResult{
		PolicyRevision: policy.Revision, FormulaVersion: policy.FormulaVersion,
		Items: make([]ItemResult, 0, len(input.Items)),
	}
	documentItems := make([]calculationItemDocument, 0, len(input.Items))
	var activeSecondsForLinear int64
	for _, item := range input.Items {
		itemResult := ItemResult{TorrentID: item.TorrentID, ActiveSeconds: item.ActiveSeconds}
		switch {
		case item.SizeBytes < policy.MinimumTorrentBytes:
			itemResult.ExclusionReason = ExclusionTooSmall
		case item.ActiveSeconds < int64(policy.MinimumActiveSeconds):
			itemResult.ExclusionReason = ExclusionTooBrief
		default:
			itemResult.Eligible = true
			itemResult.ValueScoreMicro, err = itemValueScoreMicro(policy, input.WindowEnd, item)
			if err != nil {
				return CalculationResult{}, err
			}
			if result.ValueScoreMicro > math.MaxInt64-itemResult.ValueScoreMicro ||
				activeSecondsForLinear > math.MaxInt64-item.ActiveSeconds {
				return CalculationResult{}, ErrInvariant
			}
			result.ValueScoreMicro += itemResult.ValueScoreMicro
			activeSecondsForLinear += item.ActiveSeconds
			result.EligibleTorrentCount++
		}
		result.Items = append(result.Items, itemResult)
		documentItems = append(documentItems, itemDocument(item, itemResult))
	}

	result.CurveRewardMilli, err = curveRewardMilli(policy, result.ValueScoreMicro)
	if err != nil {
		return CalculationResult{}, err
	}
	linearLimit := int64(policy.BaseLinearTorrentLimit + input.Benefits.LevelLinearTorrentBonus)
	maximumLinearSeconds := linearLimit * int64(WindowDuration/time.Second)
	if activeSecondsForLinear > maximumLinearSeconds {
		activeSecondsForLinear = maximumLinearSeconds
	}
	result.LinearRewardMilli, err = mulDivRoundHalfUp(policy.PerTorrentHourlyMilli, activeSecondsForLinear, int64(WindowDuration/time.Second))
	if err != nil || result.CurveRewardMilli > math.MaxInt64-result.LinearRewardMilli {
		return CalculationResult{}, ErrInvariant
	}
	result.BaseRewardMilli = result.CurveRewardMilli + result.LinearRewardMilli
	if input.Benefits.VIPActive {
		result.VIPBonusMilli, err = mulDivRoundHalfUp(result.BaseRewardMilli, policy.VIPBonusBPS, 10_000)
		if err != nil {
			return CalculationResult{}, err
		}
	}
	result.MedalBonusMilli, err = mulDivRoundHalfUp(result.BaseRewardMilli, input.Benefits.MedalBonusBPS, 10_000)
	if err != nil {
		return CalculationResult{}, err
	}
	result.LevelBonusMilli, err = mulDivRoundHalfUp(result.BaseRewardMilli, input.Benefits.LevelBonusBPS, 10_000)
	if err != nil {
		return CalculationResult{}, err
	}
	totalMilli := new(big.Int).SetInt64(result.BaseRewardMilli)
	totalMilli.Add(totalMilli, big.NewInt(result.VIPBonusMilli))
	totalMilli.Add(totalMilli, big.NewInt(result.MedalBonusMilli))
	totalMilli.Add(totalMilli, big.NewInt(result.LevelBonusMilli))
	if !totalMilli.IsInt64() {
		return CalculationResult{}, ErrInvariant
	}
	result.UncappedReward, err = mulDivRoundHalfUp(totalMilli.Int64(), 1, milliScale)
	if err != nil {
		return CalculationResult{}, err
	}
	result.Reward = result.UncappedReward
	if result.Reward > policy.MaximumHourlyReward {
		result.Reward = policy.MaximumHourlyReward
		result.Capped = true
	}
	result.ExperienceAmount, err = basisPointAmount(result.Reward, policy.ExperiencePerMagicBPS)
	if err != nil {
		return CalculationResult{}, err
	}

	document := calculationDocument{
		FormulaVersion: policy.FormulaVersion, PolicyRevision: policy.Revision,
		PolicySHA256: hex.EncodeToString(policy.SnapshotSHA256[:]), UserID: input.UserID.String(),
		WindowStart: input.WindowStart.Format(time.RFC3339Nano), WindowEnd: input.WindowEnd.Format(time.RFC3339Nano),
		WindowEvidenceSHA256: hex.EncodeToString(input.WindowEvidenceSHA256[:]),
		SnapshotID:           input.SnapshotID.String(), SnapshotSequence: input.SnapshotSequence,
		SnapshotObservedAt: input.SnapshotObservedAt.Format(time.RFC3339Nano),
		BenefitRevision:    input.Benefits.Revision,
		BenefitSHA256:      hex.EncodeToString(input.Benefits.SnapshotSHA256[:]),
		VIPActive:          input.Benefits.VIPActive, MedalBonusBPS: input.Benefits.MedalBonusBPS,
		LevelBonusBPS:     input.Benefits.LevelBonusBPS,
		LevelTorrentBonus: input.Benefits.LevelLinearTorrentBonus, Items: documentItems,
		CurveRewardMilli: result.CurveRewardMilli, LinearRewardMilli: result.LinearRewardMilli,
		VIPBonusMilli: result.VIPBonusMilli, MedalBonusMilli: result.MedalBonusMilli,
		LevelBonusMilli: result.LevelBonusMilli, UncappedReward: result.UncappedReward,
		Reward: result.Reward, ExperienceAmount: result.ExperienceAmount, Capped: result.Capped,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return CalculationResult{}, ErrInvariant
	}
	result.CalculationSHA256 = sha256.Sum256(encoded)
	return result, nil
}

func normalizeCalculationInput(input CalculationInput, policy PolicyRevision) (CalculationInput, error) {
	input.WindowStart = canonicalTime(input.WindowStart)
	input.WindowEnd = canonicalTime(input.WindowEnd)
	input.SnapshotObservedAt = canonicalTime(input.SnapshotObservedAt)
	input.Benefits.Revision = strings.TrimSpace(input.Benefits.Revision)
	if input.UserID == uuidNil || input.WindowEvidenceSHA256 == ([32]byte{}) ||
		input.SnapshotID == uuidNil || input.SnapshotSequence < 1 ||
		input.WindowStart.IsZero() || input.WindowStart.Minute() != 0 || input.WindowStart.Second() != 0 || input.WindowStart.Nanosecond() != 0 ||
		input.WindowEnd.Sub(input.WindowStart) != WindowDuration || input.SnapshotObservedAt.IsZero() ||
		input.SnapshotObservedAt.After(input.WindowEnd) ||
		input.WindowEnd.Sub(input.SnapshotObservedAt) > time.Duration(policy.MaximumSnapshotAgeSeconds)*time.Second ||
		!revisionPattern.MatchString(input.Benefits.Revision) || input.Benefits.SnapshotSHA256 == ([32]byte{}) ||
		input.Benefits.MedalBonusBPS < 0 || input.Benefits.MedalBonusBPS > policy.MaximumMedalBonusBPS ||
		input.Benefits.LevelBonusBPS < 0 || input.Benefits.LevelBonusBPS > policy.MaximumLevelBonusBPS ||
		input.Benefits.LevelLinearTorrentBonus < 0 || input.Benefits.LevelLinearTorrentBonus > policy.MaximumLevelTorrentBonus {
		return CalculationInput{}, ErrInput
	}
	items := slices.Clone(input.Items)
	slices.SortFunc(items, func(left, right ItemInput) int {
		if left.TorrentID < right.TorrentID {
			return -1
		}
		if left.TorrentID > right.TorrentID {
			return 1
		}
		return 0
	})
	for index := range items {
		items[index].PublishedAt = canonicalTime(items[index].PublishedAt)
		item := items[index]
		if item.TorrentID < 1 || item.SizeBytes < 0 || item.SizeBytes > maximumTorrentBytes ||
			item.PublishedAt.IsZero() || item.PublishedAt.After(input.WindowEnd) ||
			item.ActiveSeconds < 0 || item.ActiveSeconds > int64(WindowDuration/time.Second) ||
			item.RawUploadedBytes < 0 || item.SnapshotSeeders < 0 ||
			item.TrackerEvidenceSHA256 == ([32]byte{}) || item.MetadataSHA256 == ([32]byte{}) ||
			(index > 0 && item.TorrentID == items[index-1].TorrentID) {
			return CalculationInput{}, ErrInput
		}
	}
	input.Items = items
	return input, nil
}

// Official and upload-contribution terms are independent additions to the
// same item base. This intentionally avoids PtYes's order-dependent compound
// multiplication when an item satisfies both conditions.
func itemValueScoreMicro(policy PolicyRevision, windowEnd time.Time, item ItemInput) (int64, error) {
	ageSeconds := windowEnd.Sub(item.PublishedAt).Seconds()
	timeFactor := 1 - math.Pow(10, -ageSeconds/float64(policy.AgeSaturationSeconds))
	seederCount := item.SnapshotSeeders
	if seederCount < 1 {
		seederCount = 1
	}
	seederFactor := 1 + math.Sqrt2*math.Pow(10, -float64(seederCount-1)/float64(policy.SeederDecay-1))
	base := timeFactor * (float64(item.SizeBytes) / bytesPerGiB) *
		(float64(policy.SizeMultiplierBPS) / 10_000) * seederFactor *
		(float64(item.ActiveSeconds) / float64(WindowDuration/time.Second))
	additiveBPS := int64(0)
	if item.Official {
		additiveBPS += policy.OfficialBonusBPS
	}
	if item.RawUploadedBytes > 0 {
		additiveBPS += policy.UploadContributionBonusBPS
	}
	score := base * (1 + float64(additiveBPS)/10_000)
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > float64(math.MaxInt64)/scoreScale {
		return 0, ErrInvariant
	}
	return int64(math.Floor(score*scoreScale + 0.5)), nil
}

func curveRewardMilli(policy PolicyRevision, scoreMicro int64) (int64, error) {
	score := float64(scoreMicro) / scoreScale
	scale := float64(policy.CurveScaleMilli) / float64(milliScale)
	reward := float64(policy.CurveHourlyCapMilli) * 2 / math.Pi * math.Atan(score/scale)
	if math.IsNaN(reward) || math.IsInf(reward, 0) || reward < 0 || reward > float64(math.MaxInt64) {
		return 0, ErrInvariant
	}
	return int64(math.Floor(reward + 0.5)), nil
}

func mulDivRoundHalfUp(left, right, divisor int64) (int64, error) {
	if left < 0 || right < 0 || divisor <= 0 {
		return 0, ErrInvariant
	}
	value := new(big.Int).Mul(big.NewInt(left), big.NewInt(right))
	value.Add(value, big.NewInt(divisor/2))
	value.Quo(value, big.NewInt(divisor))
	if !value.IsInt64() {
		return 0, ErrInvariant
	}
	return value.Int64(), nil
}

func basisPointAmount(magic, bps int64) (string, error) {
	if magic < 0 || bps < 0 {
		return "", ErrInvariant
	}
	value := new(big.Int).Mul(big.NewInt(magic), big.NewInt(bps))
	integer, fraction := new(big.Int), new(big.Int)
	integer.QuoRem(value, big.NewInt(10_000), fraction)
	if fraction.Sign() == 0 {
		return integer.String(), nil
	}
	text := fraction.String()
	text = strings.Repeat("0", 4-len(text)) + text
	text = strings.TrimRight(text, "0")
	return integer.String() + "." + text, nil
}

func itemDocument(item ItemInput, result ItemResult) calculationItemDocument {
	return calculationItemDocument{
		TorrentID: item.TorrentID, SizeBytes: item.SizeBytes,
		PublishedAt: item.PublishedAt.Format(time.RFC3339Nano), ActiveSeconds: item.ActiveSeconds,
		RawUploadedBytes: item.RawUploadedBytes, SnapshotSeeders: item.SnapshotSeeders,
		Official: item.Official, TrackerEvidenceSHA256: hex.EncodeToString(item.TrackerEvidenceSHA256[:]),
		MetadataSHA256: hex.EncodeToString(item.MetadataSHA256[:]), Eligible: result.Eligible,
		ExclusionReason: string(result.ExclusionReason), ValueScoreMicro: result.ValueScoreMicro,
	}
}

var uuidNil = [16]byte{}
