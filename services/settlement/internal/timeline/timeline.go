// Package timeline resolves raw Tracker intervals to complete, immutable policy
// snapshots. It has no database dependency so the boundary rules are testable
// independently from persistence and worker retry behaviour.
package timeline

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/settlement/internal/policy"
)

var (
	ErrInvalidRevision = errors.New("Settlement policy timeline revision is invalid")
	ErrNoCoverage      = errors.New("Settlement policy timeline does not cover the raw interval")
	ErrAmbiguous       = errors.New("Settlement policy timeline resolves ambiguously")
)

// Scope is a materialized-policy selector. A nil field is an explicit
// wildcard, never an implicit policy default. More specific selectors win; a
// same-specificity tie fails closed and must be compiled into one exact policy
// revision by the control plane.
type Scope struct {
	UserID                 *uuid.UUID
	TorrentID              *int64
	TorrentControlSequence *int64
	SubjectControlSequence *int64
}

// Revision stores a complete policy Snapshot rather than one mutable rule.
// EffectiveAt is append-only ledger time; the revision remains valid until a
// newer revision with the identical Scope becomes effective.
type Revision struct {
	ID          uuid.UUID
	Scope       Scope
	EffectiveAt time.Time
	Snapshot    policy.Snapshot
}

type IntervalContext struct {
	UserID                 uuid.UUID
	TorrentID              int64
	TorrentControlSequence int64
	SubjectControlSequence int64
	StartsAt               time.Time
	EndsAt                 time.Time
}

// ValidateRevision is used by persistence adapters before an append-only
// timeline row is committed. Keeping it here prevents the CLI and worker from
// inventing separate selector-validation rules.
func ValidateRevision(revision Revision) error {
	return revision.validate()
}

func ResolveInterval(context IntervalContext, revisions []Revision) ([]policy.PolicySlice, error) {
	if err := context.validate(); err != nil {
		return nil, err
	}
	groups := make(map[string][]Revision)
	boundaries := []time.Time{context.StartsAt, context.EndsAt}
	for _, revision := range revisions {
		if err := revision.validate(); err != nil || !revision.Scope.matches(context) || !revision.EffectiveAt.Before(context.EndsAt) {
			if err != nil {
				return nil, err
			}
			continue
		}
		key := revision.Scope.key()
		groups[key] = append(groups[key], revision)
		if revision.EffectiveAt.After(context.StartsAt) {
			boundaries = append(boundaries, revision.EffectiveAt)
		}
	}
	for key := range groups {
		sort.Slice(groups[key], func(left, right int) bool {
			return groups[key][left].EffectiveAt.Before(groups[key][right].EffectiveAt)
		})
		for index := 1; index < len(groups[key]); index++ {
			if groups[key][index-1].EffectiveAt.Equal(groups[key][index].EffectiveAt) {
				return nil, fmt.Errorf("%w: duplicate effective instant for selector", ErrAmbiguous)
			}
		}
	}
	sort.Slice(boundaries, func(left, right int) bool { return boundaries[left].Before(boundaries[right]) })
	unique := boundaries[:0]
	for _, boundary := range boundaries {
		if len(unique) == 0 || !unique[len(unique)-1].Equal(boundary) {
			unique = append(unique, boundary)
		}
	}

	slices := make([]policy.PolicySlice, 0, len(unique)-1)
	for index := 0; index < len(unique)-1; index++ {
		chosen, err := chooseAt(unique[index], groups)
		if err != nil {
			return nil, err
		}
		slices = append(slices, policy.PolicySlice{StartsAt: unique[index], EndsAt: unique[index+1], Snapshot: chosen.Snapshot})
	}
	return slices, nil
}

func (context IntervalContext) validate() error {
	if context.UserID == uuid.Nil || context.TorrentID < 1 || context.TorrentControlSequence < 1 ||
		context.SubjectControlSequence < 1 || context.StartsAt.IsZero() || !context.EndsAt.After(context.StartsAt) {
		return ErrInvalidRevision
	}
	return nil
}

func (revision Revision) validate() error {
	if revision.ID == uuid.Nil || revision.EffectiveAt.IsZero() || revision.Scope.validate() != nil {
		return ErrInvalidRevision
	}
	if _, err := policy.EncodeSnapshot(revision.Snapshot); err != nil {
		return ErrInvalidRevision
	}
	return nil
}

func (scope Scope) validate() error {
	for _, value := range []*int64{scope.TorrentID, scope.TorrentControlSequence, scope.SubjectControlSequence} {
		if value != nil && *value < 1 {
			return ErrInvalidRevision
		}
	}
	if scope.UserID != nil && *scope.UserID == uuid.Nil {
		return ErrInvalidRevision
	}
	return nil
}

func (scope Scope) matches(context IntervalContext) bool {
	return (scope.UserID == nil || *scope.UserID == context.UserID) &&
		(scope.TorrentID == nil || *scope.TorrentID == context.TorrentID) &&
		(scope.TorrentControlSequence == nil || *scope.TorrentControlSequence == context.TorrentControlSequence) &&
		(scope.SubjectControlSequence == nil || *scope.SubjectControlSequence == context.SubjectControlSequence)
}

func (scope Scope) specificity() int {
	result := 0
	if scope.UserID != nil {
		result++
	}
	if scope.TorrentID != nil {
		result++
	}
	if scope.TorrentControlSequence != nil {
		result++
	}
	if scope.SubjectControlSequence != nil {
		result++
	}
	return result
}

func (scope Scope) key() string {
	parts := []string{optionalUUID(scope.UserID), optionalInt64(scope.TorrentID), optionalInt64(scope.TorrentControlSequence), optionalInt64(scope.SubjectControlSequence)}
	return strings.Join(parts, ":")
}

func optionalUUID(value *uuid.UUID) string {
	if value == nil {
		return "*"
	}
	return value.String()
}

func optionalInt64(value *int64) string {
	if value == nil {
		return "*"
	}
	return fmt.Sprintf("%d", *value)
}

func chooseAt(at time.Time, groups map[string][]Revision) (Revision, error) {
	var chosen *Revision
	chosenSpecificity := -1
	for _, group := range groups {
		index := sort.Search(len(group), func(index int) bool { return group[index].EffectiveAt.After(at) }) - 1
		if index < 0 {
			continue
		}
		candidate := group[index]
		specificity := candidate.Scope.specificity()
		if chosen == nil || specificity > chosenSpecificity {
			chosen = &candidate
			chosenSpecificity = specificity
			continue
		}
		if specificity == chosenSpecificity && chosen.ID != candidate.ID {
			return Revision{}, fmt.Errorf("%w: selectors of specificity %d overlap at %s", ErrAmbiguous, specificity, at.UTC().Format(time.RFC3339Nano))
		}
	}
	if chosen == nil {
		return Revision{}, fmt.Errorf("%w: no explicit revision at %s", ErrNoCoverage, at.UTC().Format(time.RFC3339Nano))
	}
	return *chosen, nil
}
