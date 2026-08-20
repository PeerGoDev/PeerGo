// Package hnrpolicy owns the immutable H&R policy evaluated at a trustworthy
// completion instant. It is deliberately separate from economic promotion
// policy: freeleech and credited bytes cannot imply an H&R exemption.
package hnrpolicy

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/hnrpolicyv1"
	"github.com/peergo/peergo/services/settlement/internal/timeline"
)

var (
	ErrInvalid    = errors.New("H&R policy is invalid")
	ErrNoCoverage = errors.New("H&R policy timeline has no coverage")
	ErrAmbiguous  = errors.New("H&R policy timeline is ambiguous")
)

type Mode = hnrpolicyv1.Mode
type RuleRef = hnrpolicyv1.RuleRef
type Policy = hnrpolicyv1.Policy

const (
	ModeDisabled = hnrpolicyv1.ModeDisabled
	ModeExempt   = hnrpolicyv1.ModeExempt
	ModeEnforced = hnrpolicyv1.ModeEnforced

	MaxAssessmentWindowSeconds = hnrpolicyv1.MaxAssessmentWindowSeconds
	MaxGracePeriodSeconds      = hnrpolicyv1.MaxGracePeriodSeconds
)

type Revision struct {
	ID          uuid.UUID
	Scope       timeline.Scope
	EffectiveAt time.Time
	Policy      Policy
}

type Context struct {
	UserID                 uuid.UUID
	TorrentID              int64
	TorrentControlSequence int64
	SubjectControlSequence int64
	At                     time.Time
}

func ValidatePolicy(policy Policy) error {
	if hnrpolicyv1.Validate(policy) != nil {
		return ErrInvalid
	}
	return nil
}

func ValidateRevision(revision Revision) error {
	if revision.ID == uuid.Nil || revision.EffectiveAt.IsZero() || validateScope(revision.Scope) != nil || ValidatePolicy(revision.Policy) != nil {
		return ErrInvalid
	}
	_, offset := revision.EffectiveAt.Zone()
	if offset != 0 {
		return ErrInvalid
	}
	return nil
}

func ValidateContext(context Context) error {
	if context.UserID == uuid.Nil || context.TorrentID < 1 || context.TorrentControlSequence < 1 ||
		context.SubjectControlSequence < 1 || context.At.IsZero() {
		return ErrInvalid
	}
	return nil
}

func validateScope(scope timeline.Scope) error {
	if scope.UserID != nil && *scope.UserID == uuid.Nil {
		return ErrInvalid
	}
	for _, value := range []*int64{scope.TorrentID, scope.TorrentControlSequence, scope.SubjectControlSequence} {
		if value != nil && *value < 1 {
			return ErrInvalid
		}
	}
	return nil
}
