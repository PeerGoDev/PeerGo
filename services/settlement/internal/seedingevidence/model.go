// Package seedingevidence closes UTC-hour Tracker facts into immutable,
// replayable evidence. It does not calculate or credit magic points; reward
// policy, catalog metadata and user benefits belong to the later Core stage.
package seedingevidence

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const SchemaVersion = "seeding.evidence.v1"

var (
	ErrInput           = errors.New("seeding evidence input is invalid")
	ErrCoveragePending = errors.New("seeding evidence source coverage is pending")
	ErrConflict        = errors.New("seeding evidence source conflicts with committed facts")
	ErrInvariant       = errors.New("seeding evidence invariant failed")
	ErrEvidenceDrift   = errors.New("closed seeding evidence has late source facts")
)

type SnapshotDelivery struct {
	Stream        string
	Subject       string
	Sequence      uint64
	DeliveryCount uint64
	Payload       []byte
}

type SnapshotApplyResult struct {
	EventID    uuid.UUID
	SnapshotID uuid.UUID
	Duplicate  bool
	Complete   bool
}

type BuildResult struct {
	WindowStart              time.Time
	WindowEnd                time.Time
	AnnounceFenceSequence    int64
	SelectedSnapshotID       uuid.UUID
	SelectedSnapshotSequence int64
	SnapshotFenceID          uuid.UUID
	SnapshotFenceSequence    int64
	ItemCount                int32
	EvidenceSHA256           [32]byte
	BuiltAt                  time.Time
	Duplicate                bool
	AnomalyCount             int64
}

type WindowBuilder interface {
	NextWindowStart(context.Context, time.Time) (time.Time, error)
	BuildHour(context.Context, time.Time, time.Time) (BuildResult, error)
}

func IsPermanentSnapshotError(err error) bool {
	return errors.Is(err, ErrInput) || errors.Is(err, ErrConflict) || errors.Is(err, ErrInvariant)
}
