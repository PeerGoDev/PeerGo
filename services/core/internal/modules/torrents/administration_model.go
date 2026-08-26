package torrents

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/contracts/trackerevent"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

var (
	ErrTorrentAdministrationInput        = errors.New("torrent administration input is invalid")
	ErrManagedTorrentNotFound            = errors.New("managed torrent was not found")
	ErrManagedTorrentVersionConflict     = errors.New("managed torrent version changed")
	ErrManagedTorrentStateConflict       = errors.New("managed torrent state cannot perform the requested action")
	ErrManagedTorrentIdempotencyConflict = errors.New("torrent lifecycle idempotency key was reused")
	ErrManagedTorrentCategoryUnavailable = errors.New("managed torrent category is unavailable")
	ErrManagedTorrentObjectUnavailable   = errors.New("managed torrent object has no verified location")
	ErrManagedTorrentPeersUnavailable    = errors.New("managed torrent active peers are unavailable")
)

// ManagedTorrent is the bounded staff projection used by the torrent
// workbench. It intentionally excludes passkeys, email addresses, client IPs,
// object-store keys and settlement evidence.
type ManagedTorrent struct {
	ID                  TorrentID
	UploaderNumericID   int64
	UploaderUsername    string
	UploaderDisplayName string
	CategoryID          string
	CategoryName        string
	Title               string
	Subtitle            string
	TotalSizeBytes      int64
	PurchasePrice       int64
	State               State
	Version             int64
	Promotion           catalog.Promotion
	PromotionEndsAt     *time.Time
	Seeders             int
	Leechers            int
	Completed           int
	SubmittedAt         time.Time
	PublishedAt         *time.Time
	StateChangedAt      time.Time
	UpdatedAt           time.Time
}

type ManagedTorrentCategory struct {
	ID      string
	Name    string
	Enabled bool
}

type ManagedTorrentStateCounts struct {
	PendingReview int64
	Published     int64
	Rejected      int64
	Disabled      int64
	Deleted       int64
}

func (counts ManagedTorrentStateCounts) Total() int64 {
	return counts.PendingReview + counts.Published + counts.Rejected + counts.Disabled + counts.Deleted
}

type ManagedTorrentPage struct {
	Items       []ManagedTorrent
	Categories  []ManagedTorrentCategory
	StateCounts ManagedTorrentStateCounts
	Total       int64
	Limit       int
	Offset      int
}

// ManagedTorrentPeerTarget is the minimum persisted identity required to ask
// Tracker for a live swarm. It does not copy peer activity into Core storage.
type ManagedTorrentPeerTarget struct {
	TorrentID      TorrentID
	InfoHashV1     InfoHashV1
	TotalSizeBytes int64
	UploaderID     uuid.UUID
	Anonymous      bool
}

type ManagedTorrentPeerIdentity struct {
	UserID      uuid.UUID
	NumericID   int64
	Username    string
	DisplayName string
}

// ManagedTorrentPeer groups one user's currently active Tracker connections.
// Endpoint and session identifiers are intentionally absent.
type ManagedTorrentPeer struct {
	UserID              uuid.UUID
	NumericID           int64
	Username            string
	DisplayName         string
	ClientFamilies      []string
	AddressFamilies     []string
	ActiveConnections   int
	SeedingConnections  int
	LeechingConnections int
	ProgressBasisPoints int
	Uploaded            int64
	Downloaded          int64
	UploadSpeed         int64
	DownloadSpeed       int64
	LastAnnounce        time.Time
	Uploader            bool
	AnonymousUploader   bool
	Seedbox             bool
}

type ManagedTorrentPeerList struct {
	TorrentID        TorrentID
	Items            []ManagedTorrentPeer
	TotalConnections int
	Truncated        bool
	GeneratedAt      time.Time
}

// UserTrackerTask is a privacy-minimized, in-memory-only aggregate of the
// target user's live Tracker connections for one torrent. It deliberately has
// no endpoint, port, peer ID, passkey or durable session identifier.
type UserTrackerTask struct {
	TorrentID           TorrentID
	InfoHashV1          string
	TotalSizeBytes      int64
	ClientFamilies      []string
	AddressFamilies     []string
	ActiveConnections   int
	SeedingConnections  int
	LeechingConnections int
	ProgressBasisPoints int
	Uploaded            int64
	Downloaded          int64
	UploadSpeed         int64
	DownloadSpeed       int64
	LastAnnounce        time.Time
	Seedbox             bool
}

type UserTrackerActivity struct {
	Items            []UserTrackerTask
	TotalConnections int
	Truncated        bool
	GeneratedAt      time.Time
}

type ManagedTorrentQuery struct {
	Query      string
	State      State
	CategoryID string
	Limit      int
	Offset     int
}

type TorrentAvailabilityAction string

const (
	TorrentAvailabilityDisable         TorrentAvailabilityAction = "disable"
	TorrentAvailabilityRestore         TorrentAvailabilityAction = "restore"
	TorrentAvailabilityWithdrawRequest TorrentAvailabilityAction = "withdraw_request"
	TorrentAvailabilityWithdrawApprove TorrentAvailabilityAction = "withdraw_approve"
	TorrentAvailabilityWithdrawReject  TorrentAvailabilityAction = "withdraw_reject"
	TorrentAvailabilityReportDisable   TorrentAvailabilityAction = "report_disable"
)

type ChangeTorrentAvailabilityInput struct {
	ChangeID        uuid.UUID
	TorrentID       TorrentID
	ExpectedVersion int64
	Action          TorrentAvailabilityAction
	Reason          string
}

type ChangeTorrentAvailabilityCommand struct {
	ChangeTorrentAvailabilityInput
	ActorID       uuid.UUID
	OccurredAt    time.Time
	Authorization authz.Decision
}

type TorrentAvailabilityResult struct {
	ChangeID  uuid.UUID
	TorrentID TorrentID
	Action    TorrentAvailabilityAction
	State     State
	Version   int64
	ChangedAt time.Time
}

// TorrentLifecycleAuditState is the canonical state hashed into the external
// audit record. Human reasons remain in Core and leave it only as a digest.
type TorrentLifecycleAuditState struct {
	TorrentID       TorrentID `json:"torrent_id"`
	State           State     `json:"state"`
	Version         int64     `json:"version"`
	TrackerEligible bool      `json:"tracker_eligible"`
}

type TorrentLifecycleAuditInput struct {
	ChangeID      uuid.UUID
	ActorID       uuid.UUID
	Action        TorrentAvailabilityAction
	Reason        string
	OccurredAt    time.Time
	Authorization authz.Decision
	Before        TorrentLifecycleAuditState
	After         TorrentLifecycleAuditState
}

type TorrentLifecycleEventBuilder interface {
	BuildTorrentLifecycleEvent(TorrentLifecycleAuditInput) (auditevent.Event, error)
}

// TorrentLifecycleEligibilityInput carries only the immutable swarm identity
// and the resulting aggregate version needed by the Tracker allowlist.
type TorrentLifecycleEligibilityInput struct {
	Result         TorrentAvailabilityResult
	InfoHashV1     InfoHashV1
	TotalSizeBytes int64
}

type TorrentLifecycleEligibilityEventBuilder interface {
	BuildTorrentLifecycleEligibilityEvent(TorrentLifecycleEligibilityInput) (trackerevent.Event, error)
}
