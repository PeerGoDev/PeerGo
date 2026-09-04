// Package haremreward settles Rousi-compatible invitation-tree rewards from
// PeerGo's immutable hourly seeding-reward calculations.
package haremreward

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInput     = errors.New("harem reward input is invalid")
	ErrInvariant = errors.New("harem reward invariant failed")
)

const MaximumSettlementBatch = 96

type Policy struct {
	Revision         string
	Enabled          bool
	RewardBPS        int32
	Depth            int16
	MinimumSeedCount int32
	HourlyCap        int64
	ActivityDays     int32
	SettlementHours  int16
	EffectiveFrom    time.Time
	SnapshotSHA256   [32]byte
	CreatedAt        time.Time
}

type Payout struct {
	InviterUserID        uuid.UUID
	EligibleInviteeCount int32
	EligibleInviteeHours int32
	SourceSeedingReward  int64
	CappedHourCount      int32
	Reward               int64
	PayloadSHA256        [32]byte
}

type Settlement struct {
	Processed                 bool
	WindowStart               time.Time
	WindowEnd                 time.Time
	PolicyRevision            string
	SourceCalculationCount    int64
	EligibleRelationshipCount int64
	RecipientCount            int
	TotalReward               int64
}

type Repository interface {
	SettleNext(context.Context, time.Time) (Settlement, error)
	MarkFailure(context.Context, time.Time, string) error
}
