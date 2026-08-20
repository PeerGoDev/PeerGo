// Package swarmprojection owns Core's eventually consistent public projection
// of Tracker swarm facts. Active peer counts arrive as chunked full snapshots;
// lifetime completions arrive as retry-stable identities on durable announce
// events. Neither path reaches into the Tracker hot process synchronously.
package swarmprojection

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInput     = errors.New("Core swarm projection input is invalid")
	ErrConflict  = errors.New("Core swarm projection event conflicts with existing evidence")
	ErrInvariant = errors.New("Core swarm projection invariant failed")
)

type ApplyResult struct {
	EventID    uuid.UUID
	SnapshotID uuid.UUID
	Duplicate  bool
	Obsolete   bool
	Applied    bool
	Noop       bool
}

type SnapshotProjector interface {
	ApplySnapshot(context.Context, []byte, time.Time) (ApplyResult, error)
}

type CompletionProjector interface {
	ApplyCompletion(context.Context, []byte, time.Time) (ApplyResult, error)
}
