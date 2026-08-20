package membergift

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/peergo/peergo/services/core/internal/modules/economy/transferpolicy"
)

var revisionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type policySnapshotDocument struct {
	Revision        string `json:"revision"`
	Enabled         bool   `json:"enabled"`
	MinimumAmount   int64  `json:"minimum_amount"`
	MaximumAmount   int64  `json:"maximum_amount"`
	DailyGrossLimit int64  `json:"daily_gross_limit"`
	FeeBPS          int32  `json:"fee_bps"`
	CreatedAt       string `json:"created_at"`
}

func NormalizePolicy(policy PolicyRevision) (PolicyRevision, []byte, error) {
	policy.Revision = strings.TrimSpace(policy.Revision)
	policy.CreatedAt = canonicalTime(policy.CreatedAt)
	if !validPolicy(policy) {
		return PolicyRevision{}, nil, ErrInput
	}
	document := policySnapshotDocument{
		Revision: policy.Revision, Enabled: policy.Enabled,
		MinimumAmount: policy.MinimumAmount, MaximumAmount: policy.MaximumAmount,
		DailyGrossLimit: policy.DailyGrossLimit, FeeBPS: policy.FeeBPS,
		CreatedAt: policy.CreatedAt.Format(time.RFC3339Nano),
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return PolicyRevision{}, nil, ErrInvariant
	}
	digest := sha256.Sum256(encoded)
	if policy.SnapshotSHA256 != ([32]byte{}) && policy.SnapshotSHA256 != digest {
		return PolicyRevision{}, nil, ErrPolicyConflict
	}
	policy.SnapshotSHA256 = digest
	return policy, encoded, nil
}

func validPolicy(policy PolicyRevision) bool {
	return revisionPattern.MatchString(policy.Revision) && !policy.CreatedAt.IsZero() &&
		(transferpolicy.Limits{
			MinimumAmount: policy.MinimumAmount, MaximumAmount: policy.MaximumAmount,
			DailyGrossLimit: policy.DailyGrossLimit, FeeBPS: policy.FeeBPS,
		}).Valid()
}

// FeeFor rounds a non-zero fee upward. Splitting a gift into small requests
// therefore cannot turn a configured fee into zero through integer rounding.
func FeeFor(amount int64, feeBPS int32) (int64, error) {
	fee, err := transferpolicy.FeeFor(amount, feeBPS)
	if errors.Is(err, transferpolicy.ErrNetAmountInvalid) {
		return 0, ErrAmountOutOfRange
	}
	if err != nil {
		return 0, ErrInput
	}
	return fee, nil
}

func canonicalTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}
