// Package trafficoutbox owns delivery of immutable Settlement results to Core.
// The source event stays in PostgreSQL until JetStream storage has confirmed a
// publish, and Core owns a separate inbox for the final idempotency boundary.
package trafficoutbox

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
)

var (
	ErrInput      = errors.New("Settlement traffic outbox input is invalid")
	ErrInvariant  = errors.New("Settlement traffic outbox invariant failed")
	ErrPublishAck = errors.New("Settlement traffic JetStream publish acknowledgement is invalid")
)

type PendingEvent struct {
	EventID    uuid.UUID
	LeaseToken uuid.UUID
	Attempts   int32
	Event      settlementtrafficv1.Event
	Payload    []byte
}

func (event PendingEvent) RetryAttempt() int32 { return event.Attempts }

type Repository interface {
	ClaimNext(context.Context, time.Time, time.Duration) (PendingEvent, bool, error)
	MarkPublished(context.Context, PendingEvent, time.Time) error
	Release(context.Context, PendingEvent, time.Time, string) error
}

type Publisher interface {
	Publish(context.Context, PendingEvent) error
}
