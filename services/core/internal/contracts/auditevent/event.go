// Package auditevent defines the immutable event envelope shared by Core
// domain transactions and the audit outbox adapter. Event payload ownership
// remains with the module that builds the reviewed event contract.
package auditevent

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/google/uuid"
)

const MaxPayloadBytes = 1 << 20

// Event is the immutable transport unit committed to Core's audit outbox.
// Payload must be canonical JSON whose envelope matches these metadata fields.
type Event struct {
	ID            uuid.UUID
	Type          string
	SchemaVersion string
	OccurredAt    time.Time
	Payload       []byte
	PayloadSHA256 [sha256.Size]byte
}

// Appender is the smallest transaction-aware boundary needed by a domain
// mutation. Implementations validate the event contract before persistence.
type Appender interface {
	Append(context.Context, Event) error
}
