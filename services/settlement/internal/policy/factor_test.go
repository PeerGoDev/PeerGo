package policy

import (
	"errors"
	"math"
	"testing"
)

func TestBasisPointsApplyUsesExactIntegerRounding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		factor BasisPoints
		raw    uint64
		want   uint64
	}{
		{name: "free", factor: 0, raw: 99, want: 0},
		{name: "thirty percent rounds down", factor: 3_000, raw: 9, want: 2},
		{name: "half", factor: 5_000, raw: 101, want: 50},
		{name: "double", factor: 20_000, raw: 101, want: 202},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := test.factor.Apply(test.raw)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Apply() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestBasisPointsApplyRejectsOverflow(t *testing.T) {
	t.Parallel()

	_, err := MaxFactor.Apply(math.MaxUint64)
	if !errors.Is(err, ErrTrafficOverflow) {
		t.Fatalf("Apply() error = %v, want ErrTrafficOverflow", err)
	}
}

func TestBasisPointsRejectsUnboundedFactor(t *testing.T) {
	t.Parallel()

	_, err := BasisPoints(MaxFactor + 1).Apply(1)
	if !errors.Is(err, ErrInvalidFactor) {
		t.Fatalf("Apply() error = %v, want ErrInvalidFactor", err)
	}
}
