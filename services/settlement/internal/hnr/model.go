package hnr

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/settlementhnrv1"
	"github.com/peergo/peergo/services/settlement/internal/hnrpolicy"
)

type State = settlementhnrv1.State
type SatisfiedBy = settlementhnrv1.SatisfiedBy

const (
	StateTracking  = settlementhnrv1.StateTracking
	StateSatisfied = settlementhnrv1.StateSatisfied
	StateExempt    = settlementhnrv1.StateExempt

	SatisfiedBySeedTime = settlementhnrv1.SatisfiedBySeedTime
	SatisfiedByRawRatio = settlementhnrv1.SatisfiedByRawRatio
	SatisfiedByExempt   = settlementhnrv1.SatisfiedByExempt
)

var (
	ErrInput            = errors.New("H&R worker input is invalid")
	ErrPolicyCoverage   = errors.New("H&R policy coverage is not available")
	ErrTimelineConflict = errors.New("H&R policy timeline revision conflicts with existing evidence")
)

type PendingWork struct {
	IntervalEventID uuid.UUID
	LeaseToken      uuid.UUID
	Attempts        int32
}

type WorkRepository interface {
	ClaimNext(context.Context, time.Time, time.Duration) (PendingWork, bool, error)
	Process(context.Context, PendingWork, time.Time) error
	Release(context.Context, PendingWork, time.Time, string) error
}

type TimelineRepository interface {
	AppendRevision(context.Context, hnrpolicy.Revision, time.Time) (created bool, err error)
}
