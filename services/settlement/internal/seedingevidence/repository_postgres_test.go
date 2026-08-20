package seedingevidence

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestSameTimestampAcceptsPostgresPrecisionRoundTrip(t *testing.T) {
	t.Parallel()
	original := time.Date(2026, 8, 20, 16, 25, 22, 534_392_462, time.UTC)
	stored := original.Truncate(time.Microsecond)

	if !sameTimestamp(pgtype.Timestamptz{Time: stored, Valid: true}, original) {
		t.Fatal("sub-microsecond Tracker timestamp did not survive the PostgreSQL precision boundary")
	}
	if sameTimestamp(pgtype.Timestamptz{Time: stored.Add(time.Microsecond), Valid: true}, original) {
		t.Fatal("distinct microsecond timestamps were treated as equal")
	}
}
