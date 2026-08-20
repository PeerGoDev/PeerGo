// Package trackercontrol owns Core's ordered Tracker control outbox and the
// allowlist projection from which signed snapshots will be generated.
package trackercontrol

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/trackerevent"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

var (
	ErrProjectionInput           = errors.New("Tracker control projection input is invalid")
	ErrProjectionStateConflict   = errors.New("Tracker control projection state conflict")
	ErrSnapshotProjection        = errors.New("Tracker snapshot projection is invalid")
	ErrSnapshotProjectionPending = errors.New("Tracker snapshot projection has pending control events")
)

type PendingEvent struct {
	Sequence   int64
	LeaseToken uuid.UUID
	Attempts   int32
	Event      trackerevent.Event
}

type AllowlistEntry struct {
	TorrentID          torrents.TorrentID
	InfoHashV1         torrents.InfoHashV1
	TotalSizeBytes     int64
	CompletedDownloads int64
	TorrentVersion     int64
	ControlSequence    int64
	UpdatedAt          time.Time
}

type ProjectionStatus struct {
	LastSequence  int64
	UpdatedAt     *time.Time
	PendingEvents int64
}

// ProjectionSnapshot is one transactionally consistent view of the global
// projection cursor and all currently enabled torrents. Snapshot builders must
// not combine a cursor and rows read in separate PostgreSQL snapshots.
type ProjectionSnapshot struct {
	ControlSequence     int64
	ProjectionUpdatedAt *time.Time
	PendingEvents       int64
	Torrents            []AllowlistEntry
}

type SnapshotSource interface {
	ReadSnapshot(ctx context.Context) (ProjectionSnapshot, error)
}

// SubjectAllowlistEntry deliberately contains no passkey, profile field or
// restriction reason. Only the effective download gate crosses this boundary;
// Tracker needs it to reject leeching while still accepting seed announces.
type SubjectAllowlistEntry struct {
	UserID             uuid.UUID
	NumericUserID      int64
	LookupHMAC         [32]byte
	CredentialVersion  int64
	DownloadRestricted bool
}

type SubjectProjectionSnapshot struct {
	ControlSequence int64
	GeneratedAt     time.Time
	Subjects        []SubjectAllowlistEntry
}

type SubjectSnapshotSource interface {
	ReadSubjectSnapshot(context.Context, time.Time) (SubjectProjectionSnapshot, error)
}
