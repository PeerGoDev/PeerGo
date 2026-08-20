// Package objectmigration coordinates verified storage changes across every
// immutable object owned by Core. Domain tables keep strong ownership and
// foreign keys; this package owns only the cross-domain migration manifest.
package objectmigration

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
)

const (
	DefaultBatchSize = 20
	MinimumRetention = 24 * time.Hour
	MaximumRetention = 365 * 24 * time.Hour
)

var (
	ErrInput         = errors.New("object storage migration input is invalid")
	ErrStateConflict = errors.New("object storage migration state conflict")
)

type Kind string

const (
	KindTorrent           Kind = "torrent"
	KindTorrentScreenshot Kind = "torrent_screenshot"
	KindAvatar            Kind = "avatar"
	KindImageDerivative   Kind = "image_derivative"
)

var AllKinds = []Kind{
	KindTorrent,
	KindTorrentScreenshot,
	KindAvatar,
	KindImageDerivative,
}

func (kind Kind) Valid() bool {
	switch kind {
	case KindTorrent, KindTorrentScreenshot, KindAvatar, KindImageDerivative:
		return true
	default:
		return false
	}
}

type Mode string

const (
	ModeReplicate Mode = "replicate"
	ModeMove      Mode = "move"
)

type PlanInput struct {
	ID                   uuid.UUID
	Mode                 Mode
	Kinds                []Kind
	SourceBackendID      objectstorage.BackendID
	DestinationBackendID objectstorage.BackendID
	RequestedBy          uuid.UUID
	OccurredAt           time.Time
}

type Plan struct {
	ID                   uuid.UUID
	Mode                 Mode
	Kinds                []Kind
	SourceBackendID      objectstorage.BackendID
	DestinationBackendID objectstorage.BackendID
	ObjectCount          int64
	CreatedAt            time.Time
}

// CopyTask contains a fully snapshotted immutable descriptor. The service does
// not need to know which domain table owns the UUID in order to copy bytes;
// the repository keeps the typed foreign key and validates it on every state
// transition.
type CopyTask struct {
	ItemID               uuid.UUID
	MigrationID          uuid.UUID
	Kind                 Kind
	ObjectID             uuid.UUID
	Descriptor           objectstorage.Descriptor
	SourceBackendID      objectstorage.BackendID
	SourceObjectKey      objectstorage.Key
	SourceVersionID      string
	DestinationBackendID objectstorage.BackendID
	DestinationObjectKey objectstorage.Key
	LeaseToken           uuid.UUID
	Attempts             int32
}

type CleanupTask struct {
	ItemID          uuid.UUID
	MigrationID     uuid.UUID
	Kind            Kind
	ObjectID        uuid.UUID
	SourceBackendID objectstorage.BackendID
	SourceObjectKey objectstorage.Key
	SourceVersionID string
	LeaseToken      uuid.UUID
	Attempts        int32
}

type VerifiedLocation struct {
	BackendID  objectstorage.BackendID
	ObjectKey  objectstorage.Key
	VersionID  string
	Descriptor objectstorage.Descriptor
	VerifiedAt time.Time
}

type Repository interface {
	Plan(context.Context, PlanInput) (Plan, error)
	ClaimCopyTasks(context.Context, uuid.UUID, time.Time, int32, time.Duration) ([]CopyTask, error)
	MarkCopyVerified(context.Context, CopyTask, VerifiedLocation) error
	ReleaseCopyTask(context.Context, CopyTask, time.Time, string) error
	RetryFailures(context.Context, uuid.UUID, time.Time) (int64, error)
	Cutover(context.Context, uuid.UUID, time.Time, time.Time) error
	ApproveCleanup(context.Context, uuid.UUID, uuid.UUID, time.Time) error
	ClaimCleanupTasks(context.Context, uuid.UUID, time.Time, int32, time.Duration) ([]CleanupTask, error)
	MarkSourceDeleted(context.Context, CleanupTask, time.Time) error
	ReleaseCleanupTask(context.Context, CleanupTask, time.Time, string) error
}
