// Package settler turns immutable raw Tracker intervals into immutable final
// traffic settlements. It deliberately has a separate worker boundary from
// ingest: raw evidence is never held hostage by a missing policy snapshot.
package settler

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/settlement/internal/timeline"
)

var (
	ErrInput            = errors.New("Settlement policy worker input is invalid")
	ErrInvariant        = errors.New("Settlement policy worker invariant failed")
	ErrPolicyCoverage   = errors.New("Settlement policy coverage is not available")
	ErrTimelineConflict = errors.New("Settlement policy timeline revision conflicts with existing evidence")
)

type PendingWork struct {
	IntervalEventID uuid.UUID
	LeaseToken      uuid.UUID
	Attempts        int32
}

type WorkRepository interface {
	ClaimNext(context.Context, time.Time, time.Duration) (PendingWork, bool, error)
	Settle(context.Context, PendingWork, time.Time) error
	Release(context.Context, PendingWork, time.Time, string) error
}

type TimelineRepository interface {
	AppendRevision(context.Context, timeline.Revision, time.Time) (created bool, err error)
}
