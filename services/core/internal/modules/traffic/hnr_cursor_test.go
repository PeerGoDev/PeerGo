package traffic

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHNRCursorCanonicalRoundTrip(t *testing.T) {
	t.Parallel()
	want := HNRCursor{
		CompletedAt:  time.Date(2026, time.August, 10, 12, 34, 56, 123456000, time.UTC),
		ObligationID: uuid.MustParse("0198f20a-6da8-4e51-9c64-222222222222"),
	}
	encoded, err := EncodeHNRCursor(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != 34 {
		t.Fatalf("encoded cursor length = %d", len(encoded))
	}
	decoded, err := DecodeHNRCursor(encoded)
	if err != nil || !decoded.CompletedAt.Equal(want.CompletedAt) || decoded.ObligationID != want.ObligationID {
		t.Fatalf("DecodeHNRCursor() = %+v, error %v", decoded, err)
	}
}

func TestHNRCursorRejectsMalformedOrNonCanonicalValue(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "not-a-cursor", strings.Repeat("A", 34)} {
		if _, err := DecodeHNRCursor(value); !errors.Is(err, ErrInput) {
			t.Fatalf("DecodeHNRCursor(%q) error = %v", value, err)
		}
	}
}
