package swarmprojection

import (
	"testing"
	"time"
)

func TestCanonicalSwarmTimeMatchesPostgresPrecision(t *testing.T) {
	t.Parallel()
	original := time.Date(2026, 8, 20, 16, 25, 22, 517_019_866, time.FixedZone("UTC+8", 8*60*60))
	want := time.Date(2026, 8, 20, 8, 25, 22, 517_019_000, time.UTC)

	if got := canonicalSwarmTime(original); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("canonicalSwarmTime() = %s; want %s in UTC", got, want)
	}
}
