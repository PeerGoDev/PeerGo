// Package operations owns read-only operational projections exposed to staff.
// It has no command for deleting, replaying or mutating queues.
package operations

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/contracts/go/settlementoperationsv1"
	"github.com/peergo/peergo/contracts/go/trackeroperationsv1"
	"github.com/peergo/peergo/services/core/internal/contracts/vaultoperations"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

var (
	ErrInput     = errors.New("operations query is invalid")
	ErrInvariant = errors.New("operations projection invariant failed")
)

type TrackerControlStatus struct {
	LastSequence     int64
	PendingEvents    int64
	RetryingEvents   int64
	EnabledTorrents  int64
	DisabledTorrents int64
	OldestPendingAt  *time.Time
	UpdatedAt        *time.Time
}

type SwarmProjectionStatus struct {
	SourceID          string
	RoutingEpoch      int64
	SnapshotSequence  int64
	ObservedAt        *time.Time
	AppliedAt         *time.Time
	CollectingRuns    int64
	LatestRunProgress string
}

type EvidenceWindowStatus struct {
	CollectingWindows int64
	CompleteWindows   int64
	LatestWindowStart *time.Time
	LatestWindowEnd   *time.Time
	LatestStatus      string
	LatestItemCount   int64
	LatestChunks      int32
	LatestReceived    int32
	MonthStartsAt     time.Time
	ExpectedThrough   time.Time
	ExpectedWindows   int64
	MissingWindows    int64
	OldestIncomplete  *time.Time
	Health            EvidenceHealth
}

// EvidenceHealth separates a recent ingestion delay from a broken historical
// chain. Workgroup contribution assessment consumes the same hourly evidence,
// but this operational status never mutates member results.
type EvidenceHealth string

const (
	EvidenceHealthHealthy     EvidenceHealth = "healthy"
	EvidenceHealthLagging     EvidenceHealth = "lagging"
	EvidenceHealthBroken      EvidenceHealth = "broken"
	EvidenceHealthUnavailable EvidenceHealth = "unavailable"
)

type ConsumerProjectionStatus struct {
	TrafficEntries   int64
	TrafficAppliedAt *time.Time
	HNREvents        int64
	HNRAppliedAt     *time.Time
}

type TrackerOverview struct {
	GeneratedAt time.Time
	Control     TrackerControlStatus
	Swarm       SwarmProjectionStatus
	Evidence    EvidenceWindowStatus
	Consumers   ConsumerProjectionStatus
}

type QueueStatus struct {
	ID              string
	Label           string
	Pending         int64
	Processing      int64
	Retrying        int64
	Dead            int64
	Completed       int64
	OldestPendingAt *time.Time
	LastErrorCode   string
	LastErrorAt     *time.Time
}

type WorkerOverview struct {
	GeneratedAt time.Time
	Queues      []QueueStatus
}

type StorageRuntime struct {
	BackendID             string
	Driver                string
	TorrentUploadMaxBytes int64
	ScreenshotMaxBytes    int64
	AvatarMaxBytes        int64
}

type StorageInventory struct {
	TorrentObjects           int64
	TorrentBytes             int64
	ScreenshotObjects        int64
	ScreenshotBytes          int64
	AvatarObjects            int64
	AvatarBytes              int64
	PreferredOnActiveBackend int64
	VerifiedOnOtherBackends  int64
	ActiveMigrations         int64
	FailedMigrationItems     int64
}

type StorageMigrationOverview struct {
	ID                   uuid.UUID
	Mode                 string
	Status               string
	ObjectKinds          []string
	SourceBackendID      string
	DestinationBackendID string
	TotalItems           int64
	PendingItems         int64
	VerifiedItems        int64
	FailedItems          int64
	DeletedItems         int64
	LastErrorCode        string
	CreatedAt            time.Time
	CutoverAt            *time.Time
	RetentionUntil       *time.Time
	CleanupApprovedAt    *time.Time
	CompletedAt          *time.Time
}

type ImageDerivativeOverview struct {
	PolicyVersion   string
	Pending         int64
	Processing      int64
	Retrying        int64
	Ready           int64
	Dead            int64
	SourceObjects   int64
	OutputObjects   int64
	OutputBytes     int64
	OldestPendingAt *time.Time
	LastErrorCode   string
	LastErrorAt     *time.Time
}

type StorageOverview struct {
	GeneratedAt      time.Time
	Runtime          StorageRuntime
	Inventory        StorageInventory
	ImageDerivatives ImageDerivativeOverview
	Migrations       []StorageMigrationOverview
}

type VIPProfileStats struct {
	TotalUsers   int64
	ActiveVIP    int64
	PermanentVIP int64
	ExpiringVIP  int64
	ExpiredVIP   int64
}

type ProfileRules struct {
	DisplayNameMinCharacters int
	DisplayNameMaxCharacters int
	AvatarMinPixels          int
	AvatarMaxPixels          int
	AvatarMaxBytes           int64
	AvatarFormat             string
}

type VIPBenefits struct {
	SeedingRewardPolicyRevision string
	SeedingRewardBonusBPS       int64
	FreeDownloadEnabled         bool
	ShareRatioExempt            bool
	NewcomerAssessmentExempt    bool
	SpeedLimitExempt            bool
	SeedboxNoDiscount           bool
}

type VIPProfileOverview struct {
	GeneratedAt time.Time
	Stats       VIPProfileStats
	Profile     ProfileRules
	Benefits    VIPBenefits
}

// EmailOverview reuses the bounded cross-service contract instead of defining
// a second Core-only copy that could silently drift from Privacy Vault.
type EmailOverview = vaultoperations.EmailStatus

// TrackerRuntimeOverview is shared with Tracker so the service boundary has
// one validated shape and cannot drift into duplicate settings types.
type TrackerRuntimeOverview = trackeroperationsv1.Runtime
type SettlementSettingsOverview = settlementoperationsv1.Settings

type TorrentUploadRules struct {
	MetainfoMaxBytes       int64
	MaxFiles               int
	RequiredPrivate        bool
	SupportedProtocol      string
	DuplicateSwarmRejected bool
	InitialState           string
}

type TorrentScreenshotRules struct {
	MaxCount        int
	MaxBytesPerFile int64
	Formats         []string
	FirstIsCover    bool
}

type TorrentObjectRules struct {
	OriginalStoredImmutable     bool
	AnnounceRewrittenOnDownload bool
	LegacyImportProfile         string
	NewUploadProfile            string
}

type TorrentPurchaseRules struct {
	Enabled             bool
	CurrencyName        string
	WholeUnitsOnly      bool
	TaxBasisPoints      int64
	PolicyRevision      string
	PolicyEffectiveFrom *time.Time
	PricedTorrents      int64
	LegacyEntitlements  int64
	LiveEntitlements    int64
	PermanentAccess     bool
	AtomicSettlement    bool
	RefundConnected     bool
}

type TorrentRulesOverview struct {
	GeneratedAt             time.Time
	Upload                  TorrentUploadRules
	Screenshots             TorrentScreenshotRules
	Object                  TorrentObjectRules
	Purchase                TorrentPurchaseRules
	ActiveUploadPolicy      torrents.UploadPolicyRevision
	ScheduledUploadPolicies []torrents.UploadPolicyRevision
}

// EconomyTransactionCounts is a staff-safe aggregate of the immutable magic
// ledger. It intentionally carries no user, posting or source-reference data.
type EconomyTransactionCounts struct {
	LegacyOpening   int64
	SeedingReward   int64
	ActivityReward  int64
	TorrentPurchase int64
	MemberGift      int64
	Tip             int64
	Refund          int64
	Adjustment      int64
}

type ActivityRewardReadiness struct {
	LedgerSupported           bool
	DailyAttendanceConnected  bool
	RandomAttendanceConnected bool
	StreakRewardConnected     bool
	RetroactiveConnected      bool
	TorrentPublishConnected   bool
	InviteRewardConnected     bool
}

type MagicUsageRules struct {
	CurrencyName             string
	WholeUnitsOnly           bool
	PTCoinEnabled            bool
	MemberOverdraftAllowed   bool
	AppendOnlyLedger         bool
	TorrentPurchaseSupported bool
	TorrentPurchaseConnected bool
	MemberGiftConnected      bool
	ContentTipConnected      bool
	RefundSupported          bool
}

type EconomySettingsOverview struct {
	GeneratedAt  time.Time
	Activity     ActivityRewardReadiness
	Usage        MagicUsageRules
	Transactions EconomyTransactionCounts
}
