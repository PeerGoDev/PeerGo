package economy

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

const (
	DefaultOverviewLimit = 30
	MaximumOverviewLimit = 100
)

// MagicStatementEntry is the member-side projection of one balanced magic
// transaction. Internal account IDs and counterparty postings stay private.
type MagicStatementEntry struct {
	LedgerSequence  int64
	TransactionType TransactionType
	EntryType       string
	Amount          int64
	BalanceAfter    int64
	SourceReference string
	PolicyRevision  string
	OccurredAt      time.Time
}

// ExperienceStatementEntry keeps PostgreSQL numeric values as canonical text;
// no float64 conversion is allowed in the HTTP or browser layers.
type ExperienceStatementEntry struct {
	EntrySequence  int64
	EntryType      string
	Amount         string
	BalanceAfter   string
	SourceKind     string
	PolicyRevision string
	LevelAfter     int16
	OccurredAt     time.Time
}

type LevelTarget struct {
	Level             int16
	MinimumExperience string
}

type ProgressOverview struct {
	Experience               string
	Level                    int16
	PolicyVersion            string
	CurrentMinimumExperience string
	Next                     *LevelTarget
	UpdatedAt                *time.Time
}

// SeedingRewardRules is the member-safe view of the policy that applies now.
// Issuer identities, authorization decisions and future revisions remain on
// the staff-only administration surface.
type SeedingRewardRules struct {
	Revision                   string
	FormulaVersion             string
	EffectiveFrom              time.Time
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
}

type LevelRule struct {
	Level             int16
	MinimumExperience string
	KarmaBonusBPS     int64
	SeedingCountBonus int32
}

// ContributionExperienceRules is the effective signed policy for experience
// sources that are settled outside attendance and hourly seeding receipts.
// Milli-experience keeps fractional Rousi-compatible rates exact end to end.
type ContributionExperienceRules struct {
	Revision                     string
	EffectiveFrom                time.Time
	ExperiencePerUploadGiBMilli  int64
	ExperiencePerTorrentMilli    int64
	ExperiencePerAccountDayMilli int64
}

type RuleOverview struct {
	SeedingReward          *SeedingRewardRules
	ContributionExperience ContributionExperienceRules
	LevelPolicyVersion     string
	Levels                 []LevelRule
}

// LatestSeedingRewardCalculation exposes the member's last immutable hourly
// receipt. It contains the calculation breakdown but no Tracker peer evidence
// or internal transaction identifiers.
type LatestSeedingRewardCalculation struct {
	WindowStart          time.Time
	WindowEnd            time.Time
	PolicyRevision       string
	EligibleTorrentCount int32
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
	CalculatedAt         time.Time
}

type Overview struct {
	MagicBalance        int64
	MagicUpdatedAt      *time.Time
	MagicEntries        []MagicStatementEntry
	Progress            ProgressOverview
	ExperienceEntries   []ExperienceStatementEntry
	Rules               RuleOverview
	LatestSeedingReward *LatestSeedingRewardCalculation
}

type OverviewRepository interface {
	Overview(context.Context, uuid.UUID, int) (Overview, error)
}

type SessionAuthenticator interface {
	CurrentSession(context.Context, string) (identity.WebSession, error)
}

// OverviewService authenticates the ordinary Web audience and derives the
// user ID from that session. The repository therefore cannot be used through
// this surface to inspect another member's private statement.
type OverviewService struct {
	authenticator SessionAuthenticator
	authorizer    authz.Authorizer
	repository    OverviewRepository
	now           func() time.Time
}

func NewOverviewService(authenticator SessionAuthenticator, authorizer authz.Authorizer, repository OverviewRepository, now func() time.Time) (*OverviewService, error) {
	if authenticator == nil || authorizer == nil || repository == nil {
		return nil, errors.New("economy overview service dependencies are required")
	}
	if now == nil {
		now = time.Now
	}
	return &OverviewService{authenticator: authenticator, authorizer: authorizer, repository: repository, now: now}, nil
}

func (service *OverviewService) MyOverview(ctx context.Context, cookieToken string, limit int) (Overview, error) {
	if limit < 1 || limit > MaximumOverviewLimit {
		return Overview{}, ErrInput
	}
	session, err := service.authenticator.CurrentSession(ctx, cookieToken)
	if err != nil {
		return Overview{}, err
	}
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionEconomyReadSelf, service.now().UTC()); err != nil {
		return Overview{}, err
	}
	return service.repository.Overview(ctx, session.User.ID, limit)
}
