// Package hnrpolicyv1 defines the canonical H&R policy shared by Core's
// control plane and Settlement's immutable timeline. Keeping the wire model
// here prevents the administration API and the accounting worker from
// drifting into two subtly different definitions of the same rule.
package hnrpolicyv1

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
)

const (
	MaxPolicyBytes                   = 4 << 10
	MaxAssessmentWindowSeconds int64 = 10 * 365 * 24 * 60 * 60
	MaxGracePeriodSeconds      int64 = 365 * 24 * 60 * 60
)

var (
	ErrInvalid         = errors.New("H&R policy is invalid")
	ErrInvalidEncoding = errors.New("H&R policy encoding is invalid")
)

type Mode string

const (
	ModeDisabled Mode = "disabled"
	ModeExempt   Mode = "exempt"
	ModeEnforced Mode = "enforced"
)

type RuleRef struct {
	ID      string
	Version int64
}

type Policy struct {
	Rule                     RuleRef
	Mode                     Mode
	RequiredSeedSeconds      int64
	RequiredRatioBasisPoints int64
	AssessmentWindowSeconds  int64
	GracePeriodSeconds       int64
	MaxIntervalCreditSeconds int64
}

type wirePolicy struct {
	RuleID                   string `json:"rule_id"`
	RuleVersion              int64  `json:"rule_version"`
	Mode                     Mode   `json:"mode"`
	RequiredSeedSeconds      int64  `json:"required_seed_seconds"`
	RequiredRatioBasisPoints int64  `json:"required_ratio_basis_points"`
	AssessmentWindowSeconds  int64  `json:"assessment_window_seconds"`
	GracePeriodSeconds       int64  `json:"grace_period_seconds"`
	MaxIntervalCreditSeconds int64  `json:"max_interval_credit_seconds"`
}

func Validate(policy Policy) error {
	if strings.TrimSpace(policy.Rule.ID) == "" || strings.TrimSpace(policy.Rule.ID) != policy.Rule.ID ||
		len(policy.Rule.ID) > 128 || policy.Rule.Version < 1 || policy.RequiredSeedSeconds < 0 ||
		policy.RequiredRatioBasisPoints < 0 || policy.RequiredRatioBasisPoints > 1_000_000 ||
		policy.AssessmentWindowSeconds < 0 || policy.AssessmentWindowSeconds > MaxAssessmentWindowSeconds ||
		policy.GracePeriodSeconds < 0 || policy.GracePeriodSeconds > MaxGracePeriodSeconds ||
		policy.MaxIntervalCreditSeconds < 0 {
		return ErrInvalid
	}
	switch policy.Mode {
	case ModeDisabled, ModeExempt:
		if policy.RequiredSeedSeconds != 0 || policy.RequiredRatioBasisPoints != 0 ||
			policy.AssessmentWindowSeconds != 0 || policy.GracePeriodSeconds != 0 ||
			policy.MaxIntervalCreditSeconds != 0 {
			return fmt.Errorf("%w: non-enforced policy carries thresholds", ErrInvalid)
		}
	case ModeEnforced:
		if (policy.RequiredSeedSeconds == 0 && policy.RequiredRatioBasisPoints == 0) ||
			policy.AssessmentWindowSeconds < policy.RequiredSeedSeconds || policy.AssessmentWindowSeconds < 1 ||
			policy.MaxIntervalCreditSeconds < 60 || policy.MaxIntervalCreditSeconds > 24*60*60 {
			return fmt.Errorf("%w: enforced thresholds are inconsistent", ErrInvalid)
		}
	default:
		return ErrInvalid
	}
	return nil
}

func Encode(policy Policy) ([]byte, error) {
	if Validate(policy) != nil {
		return nil, ErrInvalidEncoding
	}
	wire := wirePolicy{
		RuleID: policy.Rule.ID, RuleVersion: policy.Rule.Version, Mode: policy.Mode,
		RequiredSeedSeconds: policy.RequiredSeedSeconds, RequiredRatioBasisPoints: policy.RequiredRatioBasisPoints,
		AssessmentWindowSeconds: policy.AssessmentWindowSeconds, GracePeriodSeconds: policy.GracePeriodSeconds,
		MaxIntervalCreditSeconds: policy.MaxIntervalCreditSeconds,
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return nil, ErrInvalidEncoding
	}
	encoded = append(encoded, '\n')
	if len(encoded) < 3 || len(encoded) > MaxPolicyBytes {
		return nil, ErrInvalidEncoding
	}
	return encoded, nil
}

func Decode(encoded []byte) (Policy, error) {
	if len(encoded) < 3 || len(encoded) > MaxPolicyBytes {
		return Policy{}, ErrInvalidEncoding
	}
	var wire wirePolicy
	if err := signedsnapshotv1.StrictJSON(encoded, &wire); err != nil {
		return Policy{}, ErrInvalidEncoding
	}
	policy := Policy{
		Rule: RuleRef{ID: wire.RuleID, Version: wire.RuleVersion}, Mode: wire.Mode,
		RequiredSeedSeconds: wire.RequiredSeedSeconds, RequiredRatioBasisPoints: wire.RequiredRatioBasisPoints,
		AssessmentWindowSeconds: wire.AssessmentWindowSeconds, GracePeriodSeconds: wire.GracePeriodSeconds,
		MaxIntervalCreditSeconds: wire.MaxIntervalCreditSeconds,
	}
	canonical, err := Encode(policy)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return Policy{}, ErrInvalidEncoding
	}
	return policy, nil
}

func SHA256(policy Policy) ([sha256.Size]byte, error) {
	encoded, err := Encode(policy)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}
