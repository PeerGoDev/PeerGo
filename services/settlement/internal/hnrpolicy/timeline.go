package hnrpolicy

import (
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/settlement/internal/timeline"
)

// ResolveAt chooses the latest revision in each matching selector and then the
// unique most-specific selector. Equal-specificity overlaps fail closed.
func ResolveAt(context Context, revisions []Revision) (Revision, error) {
	if ValidateContext(context) != nil {
		return Revision{}, ErrInvalid
	}
	candidates := make([]Revision, 0, len(revisions))
	for _, revision := range revisions {
		if ValidateRevision(revision) != nil {
			return Revision{}, ErrInvalid
		}
		if revision.EffectiveAt.After(context.At) || !matches(revision.Scope, context) {
			continue
		}
		candidates = append(candidates, revision)
	}
	if len(candidates) == 0 {
		return Revision{}, ErrNoCoverage
	}
	sort.Slice(candidates, func(left, right int) bool {
		if specificity(candidates[left].Scope) != specificity(candidates[right].Scope) {
			return specificity(candidates[left].Scope) > specificity(candidates[right].Scope)
		}
		if !candidates[left].EffectiveAt.Equal(candidates[right].EffectiveAt) {
			return candidates[left].EffectiveAt.After(candidates[right].EffectiveAt)
		}
		return candidates[left].ID.String() < candidates[right].ID.String()
	})
	chosen := candidates[0]
	chosenSpecificity := specificity(chosen.Scope)
	for _, candidate := range candidates[1:] {
		candidateSpecificity := specificity(candidate.Scope)
		if candidateSpecificity < chosenSpecificity {
			break
		}
		if sameScope(candidate.Scope, chosen.Scope) {
			if candidate.EffectiveAt.Equal(chosen.EffectiveAt) && candidate.ID != chosen.ID {
				return Revision{}, fmt.Errorf("%w: duplicate effective instant", ErrAmbiguous)
			}
			continue
		}
		return Revision{}, fmt.Errorf("%w: selectors of specificity %d overlap", ErrAmbiguous, chosenSpecificity)
	}
	return chosen, nil
}

func matches(scope timeline.Scope, context Context) bool {
	return (scope.UserID == nil || *scope.UserID == context.UserID) &&
		(scope.TorrentID == nil || *scope.TorrentID == context.TorrentID) &&
		(scope.TorrentControlSequence == nil || *scope.TorrentControlSequence == context.TorrentControlSequence) &&
		(scope.SubjectControlSequence == nil || *scope.SubjectControlSequence == context.SubjectControlSequence)
}

func specificity(scope timeline.Scope) int {
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

func sameScope(left, right timeline.Scope) bool {
	return sameUUID(left.UserID, right.UserID) && sameInt64(left.TorrentID, right.TorrentID) &&
		sameInt64(left.TorrentControlSequence, right.TorrentControlSequence) &&
		sameInt64(left.SubjectControlSequence, right.SubjectControlSequence)
}

func sameUUID(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
