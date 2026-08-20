package ratiowatch

import (
	"errors"
	"math"
	"testing"
)

func TestNormalizePolicyKeepsFamiliarPtYesRuleInExactUnits(t *testing.T) {
	t.Parallel()
	input := PolicyInput{
		Enabled:                     true,
		DownloadThresholdBytes:      50 << 30,
		MinimumRatioBasisPoints:     4000,
		WatchPeriodSeconds:          14 * 24 * 60 * 60,
		RestrictionRatioBasisPoints: 3000,
	}
	got, err := normalizePolicy(input)
	if err != nil || got != input {
		t.Fatalf("normalizePolicy() = %+v, %v", got, err)
	}
}

func TestRecoveryUploadBytesUsesCeilingAndCapsOverflow(t *testing.T) {
	t.Parallel()
	if got := recoveryUploadBytes(0, 3, 3333); got != 1 {
		t.Fatalf("recoveryUploadBytes(0, 3, 3333) = %d, want 1", got)
	}
	if got := recoveryUploadBytes(2, 3, 5000); got != 0 {
		t.Fatalf("recoveryUploadBytes(2, 3, 5000) = %d, want 0", got)
	}
	if got := recoveryUploadBytes(math.MaxInt64, math.MaxInt64, MaximumRatioBPS); got != math.MaxInt64 {
		t.Fatalf("recoveryUploadBytes(extreme) = %d, want MaxInt64", got)
	}
}

func TestRatioAtLeastUsesExactIntegerComparison(t *testing.T) {
	t.Parallel()
	if ratioAtLeast(1, 3, 3334) {
		t.Fatal("ratioAtLeast(1, 3, 3334) = true")
	}
	if !ratioAtLeast(1, 3, 3333) {
		t.Fatal("ratioAtLeast(1, 3, 3333) = false")
	}
}

func TestNormalizePolicyRejectsRestrictionAboveRecoveryTarget(t *testing.T) {
	t.Parallel()
	_, err := normalizePolicy(PolicyInput{
		Enabled:                     true,
		DownloadThresholdBytes:      50 << 30,
		MinimumRatioBasisPoints:     4000,
		WatchPeriodSeconds:          14 * 24 * 60 * 60,
		RestrictionRatioBasisPoints: 5000,
	})
	if !errors.Is(err, ErrInput) {
		t.Fatalf("normalizePolicy() error = %v", err)
	}
}

func TestRatioBasisPointsUsesIntegerFloorAndCapsExtremeRatios(t *testing.T) {
	t.Parallel()
	if got := ratioBasisPoints(1, 3); got != 3333 {
		t.Fatalf("ratioBasisPoints(1, 3) = %d", got)
	}
	if got := ratioBasisPoints(9_000_000_000_000_000_000, 1); got != MaximumRatioBPS {
		t.Fatalf("ratioBasisPoints(extreme) = %d", got)
	}
}
