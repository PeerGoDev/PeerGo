// Package contenttip owns integer-magic tips on published torrents, visible
// social posts and visible comments. Targets remain strong typed values all the
// way to their dedicated foreign-key binding tables.
package contenttip

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInput                = errors.New("content tip input is invalid")
	ErrPolicyNotFound       = errors.New("content tip policy was not found")
	ErrPolicyConflict       = errors.New("content tip policy conflicts with existing history")
	ErrDisabled             = errors.New("content tips are disabled")
	ErrAmountOutOfRange     = errors.New("content tip amount is outside policy limits")
	ErrDailyLimit           = errors.New("content tip daily limit would be exceeded")
	ErrSelf                 = errors.New("member cannot tip own content")
	ErrTipperIneligible     = errors.New("content tipper is not eligible")
	ErrRecipientUnavailable = errors.New("content tip recipient is unavailable")
	ErrTargetUnavailable    = errors.New("content tip target is unavailable")
	ErrInsufficientBalance  = errors.New("content tip balance is insufficient")
	ErrIdempotencyConflict  = errors.New("content tip idempotency key was reused")
	ErrInvariant            = errors.New("content tip invariant failed")
)

const (
	DefaultHistoryLimit = 30
	DefaultPolicyLimit  = 30
	MaximumHistoryLimit = 100
	MaximumPolicyLimit  = 100
)

type TargetKind string

const (
	TargetTorrent TargetKind = "torrent"
	TargetPost    TargetKind = "post"
	TargetComment TargetKind = "comment"
)

type Target struct {
	Kind      TargetKind
	TorrentID int64
	PostID    uuid.UUID
	CommentID uuid.UUID
	Title     string
}

func TorrentTarget(id int64) Target     { return Target{Kind: TargetTorrent, TorrentID: id} }
func PostTarget(id uuid.UUID) Target    { return Target{Kind: TargetPost, PostID: id} }
func CommentTarget(id uuid.UUID) Target { return Target{Kind: TargetComment, CommentID: id} }

func (target Target) validReference() bool {
	switch target.Kind {
	case TargetTorrent:
		return target.TorrentID > 0 && target.PostID == uuid.Nil && target.CommentID == uuid.Nil
	case TargetPost:
		return target.TorrentID == 0 && target.PostID != uuid.Nil && target.CommentID == uuid.Nil
	case TargetComment:
		return target.TorrentID == 0 && target.PostID == uuid.Nil && target.CommentID != uuid.Nil
	default:
		return false
	}
}

type PolicyRevision struct {
	Revision        string
	Enabled         bool
	MinimumAmount   int64
	MaximumAmount   int64
	DailyGrossLimit int64
	FeeBPS          int32
	SnapshotSHA256  [32]byte
	CreatedAt       time.Time
}

type PublishedPolicy struct {
	Policy                  PolicyRevision
	IssuedBy                *uuid.UUID
	AuthorizationDecisionID *uuid.UUID
	Reason                  string
	Replayed                bool
}

type PolicyPage struct {
	Items  []PublishedPolicy
	Total  int64
	Limit  int
	Offset int
}

type Counterparty struct {
	NumericID   int64
	Username    string
	DisplayName string
}

type Direction string

const (
	DirectionSent     Direction = "sent"
	DirectionReceived Direction = "received"
)

type Tip struct {
	ID                 uuid.UUID
	RequestID          uuid.UUID
	TipperUserID       uuid.UUID
	RecipientUserID    uuid.UUID
	Direction          Direction
	Counterparty       Counterparty
	Target             Target
	GrossAmount        int64
	FeeAmount          int64
	NetAmount          int64
	PolicyRevision     string
	MagicTransactionID uuid.UUID
	OccurredAt         time.Time
	RecordedAt         time.Time
	Replayed           bool
}

type Overview struct {
	Policy         PublishedPolicy
	OutgoingToday  int64
	RemainingToday int64
	History        []Tip
}

type CreateCommand struct {
	RequestID   uuid.UUID
	TipperID    uuid.UUID
	Target      Target
	Amount      int64
	Now         time.Time
	DayStartsAt time.Time
	DayEndsAt   time.Time
}

type PublishCommand struct {
	Policy                  PolicyRevision
	IssuedBy                uuid.UUID
	AuthorizationDecisionID uuid.UUID
	Reason                  string
	SnapshotJSON            []byte
}

type Repository interface {
	Overview(context.Context, uuid.UUID, time.Time, time.Time, int) (Overview, error)
	Create(context.Context, CreateCommand) (Tip, error)
	ListPolicies(context.Context, int, int) ([]PublishedPolicy, int64, error)
	LatestPolicy(context.Context) (PublishedPolicy, error)
	PublishPolicy(context.Context, PublishCommand) (PublishedPolicy, error)
}
