// Package traffic owns Core's user-facing projection of immutable Settlement
// results. It never derives policy factors or reaches into Tracker Ledger; the
// event is already a final, evidenced accounting result.
package traffic

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInput             = errors.New("Core traffic projection input is invalid")
	ErrConflict          = errors.New("Core traffic settlement event conflicts with existing inbox evidence")
	ErrInvariant         = errors.New("Core traffic projection invariant failed")
	ErrNotFound          = errors.New("Core traffic resource was not found")
	ErrHNRNotAppealable  = errors.New("H&R obligation is not currently appealable")
	ErrHNRAppealExists   = errors.New("H&R obligation already has an appeal")
	ErrHNRAppealResolved = errors.New("H&R appeal is already resolved")
	ErrSelfTarget        = errors.New("staff action cannot target the same user")
	ErrIdempotency       = errors.New("idempotency key conflicts with existing H&R appeal")
)

const (
	DefaultOverviewLimit = 20
	MaximumOverviewLimit = 50
	// MaximumTorrentActivity keeps the compact per-torrent projection bounded
	// for account/profile and catalog progress views. The rows already exist as
	// current cumulative totals; this read never creates another history stream.
	MaximumTorrentActivity = 500
)

type ExplanationStatus string

const (
	// ExplanationNotAvailable identifies retained settlement events produced
	// before the additive public explanation projection existed.
	ExplanationNotAvailable    ExplanationStatus = "not_available"
	ExplanationComplete        ExplanationStatus = "complete"
	ExplanationTooManySegments ExplanationStatus = "too_many_segments"
)

type ApplyResult struct {
	EventID   uuid.UUID
	Duplicate bool
}

type Projector interface {
	Apply(context.Context, []byte, time.Time) (ApplyResult, error)
}

// Totals is the exact Core projection of final Settlement results. Values use
// int64 internally; the HTTP adapter serializes them as decimal text so Web
// clients never round ledger bytes through JavaScript Number.
type Totals struct {
	RawUploaded         int64
	RawDownloaded       int64
	CreditedUploaded    int64
	ChargedDownloaded   int64
	EntryCount          int64
	LastSettledAt       *time.Time
	ProjectionUpdatedAt *time.Time
}

// Entry is one immutable final settlement joined to the Core-owned torrent.
type Entry struct {
	ID                uuid.UUID
	TorrentID         int64
	TorrentTitle      string
	IntervalStartedAt time.Time
	IntervalEndedAt   time.Time
	RawUploaded       int64
	RawDownloaded     int64
	CreditedUploaded  int64
	ChargedDownloaded int64
	SettledAt         time.Time
	Explanation       Explanation
}

// Explanation contains only public-safe final byte values. Policy identities,
// applications, digests and Tracker evidence intentionally have no Core type.
type Explanation struct {
	Status       ExplanationStatus
	SegmentCount int32
	Segments     []ExplanationSegment
}

type ExplanationSegment struct {
	Index             int32
	StartsAt          time.Time
	EndsAt            time.Time
	RawUploaded       int64
	RawDownloaded     int64
	CreditedUploaded  int64
	ChargedDownloaded int64
}

type Overview struct {
	Totals          Totals
	Entries         []Entry
	TorrentActivity []TorrentActivity
}

// TorrentActivity is the current cumulative, user-owned view of one torrent.
// It is derived from traffic.user_torrent_totals and the immutable H&R
// completion projection, so displaying progress does not retain announce or
// peer-session detail in Core.
type TorrentActivity struct {
	TorrentID        int64
	TorrentTitle     string
	TotalSizeBytes   int64
	RawUploaded      int64
	RawDownloaded    int64
	ProgressBasisPts int
	Completed        bool
	LastSettledAt    time.Time
}

type OverviewRepository interface {
	Overview(context.Context, uuid.UUID, int) (Overview, error)
}
