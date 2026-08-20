// Package policy owns the versioned traffic-accounting rules used by
// Settlement. Tracker records facts; this package turns those facts into
// credited upload and charged download without depending on SQL or network I/O.
package policy

import (
	"errors"
	"fmt"
	"math"
)

const (
	// OneX is one accounting unit per raw byte. Basis points keep all traffic
	// arithmetic deterministic across Go, PostgreSQL, replays, and migrations.
	OneX BasisPoints = 10_000
	// MaxFactor is a deliberate safety bound, not a product setting. Raising it
	// requires reviewing ledger overflow behavior and every policy producer.
	MaxFactor BasisPoints = 100_000
)

var (
	ErrInvalidFactor   = errors.New("traffic factor is outside the supported range")
	ErrTrafficOverflow = errors.New("credited traffic exceeds uint64")
)

// BasisPoints represents a non-negative traffic multiplier: 10_000 is 1x,
// 20_000 is 2x, 5_000 is 50%, and 0 is free or suppressed traffic.
type BasisPoints uint32

// Validate rejects factors that could make a corrupted policy revision create
// unbounded traffic during replay.
func (factor BasisPoints) Validate() error {
	if factor > MaxFactor {
		return fmt.Errorf("%w: %d", ErrInvalidFactor, factor)
	}
	return nil
}

// Apply multiplies raw bytes by a basis-point factor and rounds down exactly as
// the ledger contract specifies. The quotient/remainder form avoids overflowing
// on raw*factor before the final result can be checked.
func (factor BasisPoints) Apply(raw uint64) (uint64, error) {
	if err := factor.Validate(); err != nil {
		return 0, err
	}

	whole := raw / uint64(OneX)
	remainder := raw % uint64(OneX)
	multiplier := uint64(factor)
	if multiplier != 0 && whole > math.MaxUint64/multiplier {
		return 0, ErrTrafficOverflow
	}

	result := whole * multiplier
	fraction := remainder * multiplier / uint64(OneX)
	if result > math.MaxUint64-fraction {
		return 0, ErrTrafficOverflow
	}
	return result + fraction, nil
}

// Factors contains the independent upload-credit and download-charge factors.
// Promotions never change swarm counters, completion, or H&R facts.
type Factors struct {
	Upload   BasisPoints
	Download BasisPoints
}

func (factors Factors) validate() error {
	if err := factors.Upload.Validate(); err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	if err := factors.Download.Validate(); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	return nil
}

func favorable(current, candidate Factors) Factors {
	if candidate.Upload > current.Upload {
		current.Upload = candidate.Upload
	}
	if candidate.Download < current.Download {
		current.Download = candidate.Download
	}
	return current
}

func safeAdd(left, right uint64) (uint64, error) {
	if left > math.MaxUint64-right {
		return 0, ErrTrafficOverflow
	}
	return left + right, nil
}
