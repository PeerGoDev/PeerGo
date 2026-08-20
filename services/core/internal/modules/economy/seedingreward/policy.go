package seedingreward

import (
	"crypto/sha256"
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

const (
	maximumBasisPoints         = int64(100_000)
	maximumCurveMilli          = int64(10_000_000_000)
	maximumCurveScaleMilli     = int64(10_000_000_000_000)
	maximumTorrentBytes        = int64(1 << 60)
	maximumHourlyReward        = int64(1_000_000_000)
	maximumLinearTorrentLimit  = int32(100_000)
	maximumSnapshotAgeSeconds  = int32(24 * 60 * 60)
	maximumAgeSaturationSecond = int64(10 * 365 * 24 * 60 * 60)
)

var revisionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type policySnapshotDocument struct {
	Revision                   string `json:"revision"`
	FormulaVersion             string `json:"formula_version"`
	EffectiveFrom              string `json:"effective_from"`
	CurveHourlyCapMilli        int64  `json:"curve_hourly_cap_milli"`
	AgeSaturationSeconds       int64  `json:"age_saturation_seconds"`
	SeederDecay                int32  `json:"seeder_decay"`
	CurveScaleMilli            int64  `json:"curve_scale_milli"`
	SizeMultiplierBPS          int64  `json:"size_multiplier_bps"`
	OfficialBonusBPS           int64  `json:"official_bonus_bps"`
	UploadContributionBonusBPS int64  `json:"upload_contribution_bonus_bps"`
	PerTorrentHourlyMilli      int64  `json:"per_torrent_hourly_milli"`
	BaseLinearTorrentLimit     int32  `json:"base_linear_torrent_limit"`
	MaximumLevelTorrentBonus   int32  `json:"maximum_level_torrent_bonus"`
	MinimumTorrentBytes        int64  `json:"minimum_torrent_bytes"`
	MinimumActiveSeconds       int32  `json:"minimum_active_seconds"`
	MaximumSnapshotAgeSeconds  int32  `json:"maximum_snapshot_age_seconds"`
	VIPBonusBPS                int64  `json:"vip_bonus_bps"`
	MaximumMedalBonusBPS       int64  `json:"maximum_medal_bonus_bps"`
	MaximumLevelBonusBPS       int64  `json:"maximum_level_bonus_bps"`
	MaximumHourlyReward        int64  `json:"maximum_hourly_reward"`
	ExperiencePerMagicBPS      int64  `json:"experience_per_magic_bps"`
}

func NormalizePolicy(policy PolicyRevision) (PolicyRevision, []byte, error) {
	policy.Revision = strings.TrimSpace(policy.Revision)
	policy.FormulaVersion = strings.TrimSpace(policy.FormulaVersion)
	policy.EffectiveFrom = canonicalTime(policy.EffectiveFrom)
	policy.CreatedAt = canonicalTime(policy.CreatedAt)
	if !validPolicy(policy) {
		return PolicyRevision{}, nil, ErrInput
	}
	document := policySnapshotDocument{
		Revision: policy.Revision, FormulaVersion: policy.FormulaVersion,
		EffectiveFrom:        policy.EffectiveFrom.Format(time.RFC3339Nano),
		CurveHourlyCapMilli:  policy.CurveHourlyCapMilli,
		AgeSaturationSeconds: policy.AgeSaturationSeconds, SeederDecay: policy.SeederDecay,
		CurveScaleMilli: policy.CurveScaleMilli, SizeMultiplierBPS: policy.SizeMultiplierBPS,
		OfficialBonusBPS:           policy.OfficialBonusBPS,
		UploadContributionBonusBPS: policy.UploadContributionBonusBPS,
		PerTorrentHourlyMilli:      policy.PerTorrentHourlyMilli,
		BaseLinearTorrentLimit:     policy.BaseLinearTorrentLimit,
		MaximumLevelTorrentBonus:   policy.MaximumLevelTorrentBonus,
		MinimumTorrentBytes:        policy.MinimumTorrentBytes,
		MinimumActiveSeconds:       policy.MinimumActiveSeconds,
		MaximumSnapshotAgeSeconds:  policy.MaximumSnapshotAgeSeconds,
		VIPBonusBPS:                policy.VIPBonusBPS, MaximumMedalBonusBPS: policy.MaximumMedalBonusBPS,
		MaximumLevelBonusBPS:  policy.MaximumLevelBonusBPS,
		MaximumHourlyReward:   policy.MaximumHourlyReward,
		ExperiencePerMagicBPS: policy.ExperiencePerMagicBPS,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return PolicyRevision{}, nil, ErrInvariant
	}
	digest := sha256.Sum256(encoded)
	if policy.SnapshotSHA256 != ([32]byte{}) && policy.SnapshotSHA256 != digest {
		return PolicyRevision{}, nil, ErrPolicyConflict
	}
	policy.SnapshotSHA256 = digest
	return policy, encoded, nil
}

func validPolicy(policy PolicyRevision) bool {
	return revisionPattern.MatchString(policy.Revision) &&
		policy.FormulaVersion == FormulaVersion &&
		!policy.EffectiveFrom.IsZero() && !policy.CreatedAt.IsZero() &&
		policy.EffectiveFrom.Unix()%int64(WindowDuration/time.Second) == 0 &&
		policy.CreatedAt.Before(policy.EffectiveFrom) &&
		policy.CurveHourlyCapMilli > 0 && policy.CurveHourlyCapMilli <= maximumCurveMilli &&
		policy.AgeSaturationSeconds >= int64(WindowDuration/time.Second) &&
		policy.AgeSaturationSeconds <= maximumAgeSaturationSecond &&
		policy.SeederDecay > 1 && policy.SeederDecay <= 1_000 &&
		policy.CurveScaleMilli > 0 && policy.CurveScaleMilli <= maximumCurveScaleMilli &&
		withinBPS(policy.SizeMultiplierBPS) && policy.SizeMultiplierBPS > 0 &&
		withinBPS(policy.OfficialBonusBPS) && withinBPS(policy.UploadContributionBonusBPS) &&
		policy.PerTorrentHourlyMilli >= 0 && policy.PerTorrentHourlyMilli <= maximumCurveMilli &&
		policy.BaseLinearTorrentLimit >= 0 && policy.BaseLinearTorrentLimit <= maximumLinearTorrentLimit &&
		policy.MaximumLevelTorrentBonus >= 0 && policy.MaximumLevelTorrentBonus <= maximumLinearTorrentLimit &&
		policy.MinimumTorrentBytes >= 0 && policy.MinimumTorrentBytes <= maximumTorrentBytes &&
		policy.MinimumActiveSeconds > 0 && policy.MinimumActiveSeconds <= int32(WindowDuration/time.Second) &&
		policy.MaximumSnapshotAgeSeconds > 0 && policy.MaximumSnapshotAgeSeconds <= maximumSnapshotAgeSeconds &&
		withinBPS(policy.VIPBonusBPS) && withinBPS(policy.MaximumMedalBonusBPS) &&
		withinBPS(policy.MaximumLevelBonusBPS) &&
		policy.MaximumHourlyReward > 0 && policy.MaximumHourlyReward <= maximumHourlyReward &&
		withinBPS(policy.ExperiencePerMagicBPS)
}

func withinBPS(value int64) bool { return value >= 0 && value <= maximumBasisPoints }

func canonicalTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}
