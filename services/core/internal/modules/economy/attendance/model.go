// Package attendance owns PeerGo's daily attendance policy timeline and the
// atomic settlement that joins an attendance receipt to the magic and
// experience ledgers.
package attendance

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInput               = errors.New("attendance input is invalid")
	ErrPolicyNotFound      = errors.New("attendance policy was not found")
	ErrPolicyConflict      = errors.New("attendance policy conflicts with existing history")
	ErrDisabled            = errors.New("attendance is disabled")
	ErrModeDisabled        = errors.New("attendance mode is disabled")
	ErrAlreadyClaimed      = errors.New("attendance was already claimed for this date")
	ErrIdempotencyConflict = errors.New("attendance idempotency key was reused")
	ErrInvariant           = errors.New("attendance invariant failed")
)

type Mode string

const (
	ModeFixed  Mode = "fixed"
	ModeRandom Mode = "random"
)

type StreakMilestone struct {
	Days   int32 `json:"days"`
	Reward int64 `json:"reward"`
}

// PolicyRevision is a complete daily reward snapshot. All reward values are
// integer magic or integer experience; float64 never enters this boundary.
type PolicyRevision struct {
	Revision            string
	EffectiveFrom       time.Time
	CreatedAt           time.Time
	Enabled             bool
	DayBoundaryTimezone string
	FixedEnabled        bool
	FixedReward         int64
	RandomEnabled       bool
	RandomMin           int64
	RandomMax           int64
	StreakEnabled       bool
	StreakMilestones    []StreakMilestone
	ExperienceReward    int64
	SnapshotSHA256      [32]byte
}

type PublishedPolicy struct {
	Policy                  PolicyRevision
	IssuedBy                *uuid.UUID
	AuthorizationDecisionID *uuid.UUID
	Reason                  string
	Replayed                bool
}

type PolicyPage struct {
	Items                []PublishedPolicy
	Total                int64
	Limit                int
	Offset               int
	MinimumEffectiveFrom time.Time
}

type Record struct {
	ID                  uuid.UUID
	RequestID           uuid.UUID
	UserID              uuid.UUID
	AttendanceDate      string
	DayBoundaryTimezone string
	Mode                Mode
	BaseReward          int64
	StreakReward        int64
	TotalReward         int64
	ExperienceReward    int64
	CurrentStreak       int32
	TotalDays           int32
	LongestStreak       int32
	PolicyRevision      string
	PayloadSHA256       [32]byte
	MagicTransactionID  uuid.UUID
	ExperienceEntryID   *uuid.UUID
	OccurredAt          time.Time
	RecordedAt          time.Time
	Replayed            bool
}

type Overview struct {
	Policy        *PublishedPolicy
	ClaimedToday  bool
	Today         string
	CurrentStreak int32
	TotalDays     int32
	LongestStreak int32
	TodayRecord   *Record
	History       []Record
}

type ClaimCommand struct {
	RequestID uuid.UUID
	UserID    uuid.UUID
	Mode      Mode
	Now       time.Time
}

type PublishCommand struct {
	Policy                  PolicyRevision
	IssuedBy                uuid.UUID
	AuthorizationDecisionID uuid.UUID
	Reason                  string
	SnapshotJSON            []byte
}

type Repository interface {
	Overview(context.Context, uuid.UUID, time.Time, int) (Overview, error)
	Claim(context.Context, ClaimCommand) (Record, error)
	ListPolicies(context.Context, int, int) ([]PublishedPolicy, int64, error)
	PublishPolicy(context.Context, PublishCommand) (PublishedPolicy, error)
	LatestPolicy(context.Context) (PublishedPolicy, error)
}
