package hnr

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"
)

// IncrementalState is a replay checkpoint, not a second financial ledger.
// Ranges are an exact union; nonzero upload points retain the ordering needed
// to reproduce the first ratio-satisfaction instant after a late delivery.
type IncrementalState struct {
	ObligationID             uuid.UUID        `json:"obligation_id"`
	CompletedAt              time.Time        `json:"completed_at"`
	MaxIntervalCreditSeconds int64            `json:"max_interval_credit_seconds"`
	Cursors                  map[string]int64 `json:"cursors"`
	Ranges                   []activityRange  `json:"ranges"`
	Uploads                  []uploadPoint    `json:"uploads"`
	LastEvidenceAt           time.Time        `json:"last_evidence_at"`
}
type activityRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}
type uploadPoint struct {
	EventID uuid.UUID `json:"event_id"`
	At      time.Time `json:"at"`
	Bytes   int64     `json:"bytes"`
}
type SequencedInterval struct {
	RawInterval
	Stream   string
	Sequence int64
}

func AdvanceIncremental(o Obligation, previous *IncrementalState, delta []SequencedInterval) (IncrementalState, Progress, error) {
	if _, err := Evaluate(o, nil); err != nil {
		return IncrementalState{}, Progress{}, err
	}
	state := IncrementalState{ObligationID: o.ID, CompletedAt: o.CompletedAt, MaxIntervalCreditSeconds: o.MaxIntervalCreditSeconds,
		Cursors: map[string]int64{}, LastEvidenceAt: o.CompletedAt}
	if previous != nil {
		// A failed transaction must never mutate the caller's committed checkpoint.
		b, err := json.Marshal(previous)
		if err != nil {
			return state, Progress{}, ErrInvariant
		}
		if err = json.Unmarshal(b, &state); err != nil {
			return state, Progress{}, ErrInvariant
		}
		if state.ObligationID != o.ID || !state.CompletedAt.Equal(o.CompletedAt) ||
			state.MaxIntervalCreditSeconds != o.MaxIntervalCreditSeconds || state.Cursors == nil ||
			state.LastEvidenceAt.Before(o.CompletedAt) {
			return state, Progress{}, ErrInvariant
		}
	}
	ordered := append([]SequencedInterval(nil), delta...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Stream != ordered[j].Stream {
			return ordered[i].Stream < ordered[j].Stream
		}
		return ordered[i].Sequence < ordered[j].Sequence
	})
	for i, v := range ordered {
		if v.Stream == "" || v.Sequence < 1 || v.EventID == uuid.Nil || v.StartsAt.IsZero() ||
			!v.EndsAt.After(v.StartsAt) || v.PreviousLeft < 0 || v.CurrentLeft < 0 || v.RawUploaded < 0 {
			return state, Progress{}, ErrInvariant
		}
		if i > 0 && ordered[i-1].Stream == v.Stream && ordered[i-1].Sequence == v.Sequence &&
			ordered[i-1].RawInterval != v.RawInterval {
			return state, Progress{}, ErrInvariant
		}
		if v.Sequence <= state.Cursors[v.Stream] {
			continue
		}
		state.Cursors[v.Stream] = v.Sequence
		if !v.EndsAt.After(o.CompletedAt) {
			continue
		}
		if v.EndsAt.After(state.LastEvidenceAt) {
			state.LastEvidenceAt = v.EndsAt
		}
		if !v.StartsAt.Before(o.CompletedAt) && v.RawUploaded > 0 {
			state.Uploads = append(state.Uploads, uploadPoint{EventID: v.EventID, At: v.EndsAt, Bytes: v.RawUploaded})
		}
		if v.PreviousLeft != 0 || v.CurrentLeft != 0 {
			continue
		}
		start := v.StartsAt
		if start.Before(o.CompletedAt) {
			start = o.CompletedAt
		}
		end := v.EndsAt
		if cap := start.Add(time.Duration(o.MaxIntervalCreditSeconds) * time.Second); end.After(cap) {
			end = cap
		}
		if end.After(start) {
			state.Ranges = append(state.Ranges, activityRange{Start: start, End: end})
		}
	}
	sort.Slice(state.Ranges, func(i, j int) bool {
		if !state.Ranges[i].Start.Equal(state.Ranges[j].Start) {
			return state.Ranges[i].Start.Before(state.Ranges[j].Start)
		}
		return state.Ranges[i].End.Before(state.Ranges[j].End)
	})
	merged := make([]activityRange, 0, len(state.Ranges))
	for _, r := range state.Ranges {
		if r.Start.Before(o.CompletedAt) || !r.End.After(r.Start) {
			return state, Progress{}, ErrInvariant
		}
		if len(merged) == 0 || r.Start.After(merged[len(merged)-1].End) {
			merged = append(merged, r)
		} else if r.End.After(merged[len(merged)-1].End) {
			merged[len(merged)-1].End = r.End
		}
	}
	state.Ranges = merged
	sort.Slice(state.Uploads, func(i, j int) bool {
		if !state.Uploads[i].At.Equal(state.Uploads[j].At) {
			return state.Uploads[i].At.Before(state.Uploads[j].At)
		}
		return state.Uploads[i].EventID.String() < state.Uploads[j].EventID.String()
	})
	raw := o.InitialUploaded
	var ratioAt *time.Time
	if ratioReached(raw, o.RawDownloaded, o.RequiredRatioBasisPoints) {
		v := o.CompletedAt
		ratioAt = &v
	}
	for _, p := range state.Uploads {
		if p.EventID == uuid.Nil || p.At.Before(o.CompletedAt) || p.Bytes <= 0 {
			return state, Progress{}, ErrInvariant
		}
		var ok bool
		raw, ok = safeAdd(raw, p.Bytes)
		if !ok {
			return state, Progress{}, ErrInvariant
		}
		if ratioAt == nil && ratioReached(raw, o.RawDownloaded, o.RequiredRatioBasisPoints) {
			v := p.At
			ratioAt = &v
		}
	}
	ranges := make([]creditedRange, len(state.Ranges))
	for i, r := range state.Ranges {
		ranges[i] = creditedRange{startsAt: r.Start, endsAt: r.End}
	}
	seconds, seedAt, err := mergeRanges(ranges, o.RequiredSeedSeconds)
	if err != nil {
		return state, Progress{}, err
	}
	p := Progress{State: StateTracking, SeededSeconds: seconds, RawUploaded: raw,
		RawRatioBasisPoints: ratioBasisPoints(raw, o.RawDownloaded), LastEvidenceAt: state.LastEvidenceAt}
	chooseSatisfaction(&p, seedAt, ratioAt)
	return state, p, nil
}
