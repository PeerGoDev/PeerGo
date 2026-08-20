// Package seedingreward calculates integer magic-point rewards from closed,
// immutable Tracker evidence. It owns neither live peer state nor account
// balances; callers persist the returned digest through economy's ledger.
package seedingreward

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	FormulaVersion = "nexus-atan-active-v1"
	WindowDuration = time.Hour
)

var (
	ErrInput            = errors.New("seeding reward input is invalid")
	ErrPolicyNotFound   = errors.New("seeding reward policy was not found")
	ErrPolicyConflict   = errors.New("seeding reward policy conflicts with existing history")
	ErrEvidenceConflict = errors.New("seeding reward evidence conflicts with an existing projection")
	ErrInvariant        = errors.New("seeding reward invariant failed")
)

type EvidenceApplyResult struct {
	EventID     uuid.UUID
	WindowStart time.Time
	Duplicate   bool
	Complete    bool
}

type EvidenceProjector interface {
	ApplyEvidence(context.Context, []byte, time.Time) (EvidenceApplyResult, error)
}

// PolicyRevision is a complete, immutable reward formula snapshot. Monetary
// coefficients use milli-magic or basis points; no persisted setting uses a
// floating-point amount.
type PolicyRevision struct {
	Revision                   string
	FormulaVersion             string
	EffectiveFrom              time.Time
	CreatedAt                  time.Time
	CurveHourlyCapMilli        int64
	AgeSaturationSeconds       int64
	SeederDecay                int32
	CurveScaleMilli            int64
	SizeMultiplierBPS          int64
	OfficialBonusBPS           int64
	UploadContributionBonusBPS int64
	PerTorrentHourlyMilli      int64
	BaseLinearTorrentLimit     int32
	MaximumLevelTorrentBonus   int32
	MinimumTorrentBytes        int64
	MinimumActiveSeconds       int32
	MaximumSnapshotAgeSeconds  int32
	VIPBonusBPS                int64
	MaximumMedalBonusBPS       int64
	MaximumLevelBonusBPS       int64
	MaximumHourlyReward        int64
	ExperiencePerMagicBPS      int64
	SnapshotSHA256             [32]byte
}

// ItemInput combines one closed Tracker item with Core-owned torrent metadata
// as it existed for this reward window.
type ItemInput struct {
	TorrentID             int64
	SizeBytes             int64
	PublishedAt           time.Time
	ActiveSeconds         int64
	RawUploadedBytes      int64
	SnapshotSeeders       int32
	Official              bool
	TrackerEvidenceSHA256 [32]byte
	MetadataSHA256        [32]byte
}

// BenefitInput is an immutable snapshot of Core-owned user benefits. Bonus
// rates are inputs only within policy caps; VIP uses the policy's fixed rate.
type BenefitInput struct {
	Revision                string
	SnapshotSHA256          [32]byte
	VIPActive               bool
	MedalBonusBPS           int64
	LevelBonusBPS           int64
	LevelLinearTorrentBonus int32
}

type CalculationInput struct {
	UserID               uuid.UUID
	WindowStart          time.Time
	WindowEnd            time.Time
	WindowEvidenceSHA256 [32]byte
	SnapshotID           uuid.UUID
	SnapshotSequence     int64
	SnapshotObservedAt   time.Time
	Benefits             BenefitInput
	Items                []ItemInput
}

type ExclusionReason string

const (
	ExclusionNone     ExclusionReason = ""
	ExclusionTooSmall ExclusionReason = "torrent_too_small"
	ExclusionTooBrief ExclusionReason = "active_time_too_short"
)

type ItemResult struct {
	TorrentID       int64
	Eligible        bool
	ExclusionReason ExclusionReason
	ValueScoreMicro int64
	ActiveSeconds   int64
}

type CalculationResult struct {
	PolicyRevision       string
	FormulaVersion       string
	EligibleTorrentCount int32
	ValueScoreMicro      int64
	CurveRewardMilli     int64
	LinearRewardMilli    int64
	BaseRewardMilli      int64
	VIPBonusMilli        int64
	MedalBonusMilli      int64
	LevelBonusMilli      int64
	UncappedReward       int64
	Reward               int64
	ExperienceAmount     string
	Capped               bool
	Items                []ItemResult
	CalculationSHA256    [32]byte
}
