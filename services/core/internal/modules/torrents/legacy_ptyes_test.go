package torrents

import (
	"errors"
	"testing"
)

func TestReconcilePtYesIdentityRequiresIndependentValuesToMatch(t *testing.T) {
	t.Parallel()

	parsed := mustParseV1(t, validSingleFixture("legacy.bin", 42, 16*1024), ValidationProfileStrictUpload)
	matched := ReconcilePtYesIdentity(PtYesIdentity{
		InfoHashV1: parsed.InfoHashV1,
		SizeBytes:  parsed.TotalSizeBytes,
		FileCount:  len(parsed.Files),
	}, parsed)
	if len(matched) != 0 {
		t.Fatalf("matched discrepancies = %+v", matched)
	}

	var wrongHash InfoHashV1
	discrepancies := ReconcilePtYesIdentity(PtYesIdentity{
		InfoHashV1: wrongHash,
		SizeBytes:  parsed.TotalSizeBytes + 1,
		FileCount:  len(parsed.Files) + 1,
	}, parsed)
	if len(discrepancies) != 3 || discrepancies[0].Code != LegacyInfoHashMismatch ||
		discrepancies[1].Code != LegacySizeMismatch || discrepancies[2].Code != LegacyFileCountMismatch {
		t.Fatalf("discrepancies = %+v", discrepancies)
	}
}

func TestMapPtYesStatePreservesObservedVisibilitySemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status      string
		softDeleted bool
		want        State
	}{
		{status: "approved", want: StatePublished},
		{status: "", want: StatePublished},
		{status: "pending", want: StatePendingReview},
		{status: "revision", want: StatePendingReview},
		{status: "rejected", want: StateRejected},
		{status: "approved", softDeleted: true, want: StateDeleted},
		{status: "unknown", softDeleted: true, want: StateDeleted},
	}
	for _, test := range tests {
		got, err := MapPtYesState(test.status, test.softDeleted)
		if err != nil {
			t.Fatalf("MapPtYesState(%q, %t) error = %v", test.status, test.softDeleted, err)
		}
		if got != test.want {
			t.Fatalf("MapPtYesState(%q, %t) = %q, want %q", test.status, test.softDeleted, got, test.want)
		}
	}

	if _, err := MapPtYesState("mystery", false); !errors.Is(err, ErrTorrentInputInvalid) {
		t.Fatalf("MapPtYesState(unknown) error = %v, want ErrTorrentInputInvalid", err)
	}
}
