// Package seedingoutbox reliably publishes closed, privacy-minimized hourly
// seeding evidence to Core. Raw Tracker evidence remains in Tracker Ledger.
package seedingoutbox

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/settlementseedingv1"
)

var (
	ErrInput      = errors.New("Settlement seeding evidence outbox input is invalid")
	ErrInvariant  = errors.New("Settlement seeding evidence outbox invariant failed")
	ErrPublishAck = errors.New("Settlement seeding evidence JetStream publish acknowledgement is invalid")
)

type PendingEvent struct {
	EventID    uuid.UUID
	LeaseToken uuid.UUID
	Attempts   int32
	Event      settlementseedingv1.Event
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
