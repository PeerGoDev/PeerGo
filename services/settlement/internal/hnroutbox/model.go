// Package hnroutbox publishes privacy-minimized H&R obligation snapshots to
// Core. Raw Tracker evidence and policy provenance never cross this boundary.
package hnroutbox

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/settlementhnrv1"
)

var (
	ErrInput      = errors.New("Settlement H&R outbox input is invalid")
	ErrInvariant  = errors.New("Settlement H&R outbox invariant failed")
	ErrPublishAck = errors.New("Settlement H&R JetStream publish acknowledgement is invalid")
)

type PendingEvent struct {
	EventID    uuid.UUID
	LeaseToken uuid.UUID
	Attempts   int32
	Event      settlementhnrv1.Event
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
