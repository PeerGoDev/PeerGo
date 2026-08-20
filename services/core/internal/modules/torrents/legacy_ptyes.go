package torrents

import (
	"fmt"
	"strconv"
	"strings"
)

// LegacyDiscrepancyCode is written to migration reports. Source rows with any
// discrepancy stay out of the live aggregate until the mismatch is explained;
// import never overwrites the database value or reparses it as a new identity.
type LegacyDiscrepancyCode string

const (
	LegacyInfoHashMismatch  LegacyDiscrepancyCode = "info_hash_mismatch"
	LegacySizeMismatch      LegacyDiscrepancyCode = "size_mismatch"
	LegacyFileCountMismatch LegacyDiscrepancyCode = "file_count_mismatch"
)

type LegacyDiscrepancy struct {
	Code     LegacyDiscrepancyCode
	Expected string
	Actual   string
}

// PtYesIdentity is the minimum source-row projection required to reconcile an
// original .torrent object. SizeBytes follows PtYes's file-length sum and thus
// includes BEP 47 padding entries, matching ParsedMetainfo.TotalSizeBytes.
type PtYesIdentity struct {
	InfoHashV1 InfoHashV1
	SizeBytes  int64
	FileCount  int
}

// ReconcilePtYesIdentity compares independent source-row values with values
// derived from original bytes. A caller may persist all returned discrepancies,
// but must only import into live tables when the result is empty.
func ReconcilePtYesIdentity(source PtYesIdentity, parsed ParsedMetainfo) []LegacyDiscrepancy {
	discrepancies := make([]LegacyDiscrepancy, 0, 3)
	if source.InfoHashV1 != parsed.InfoHashV1 {
		discrepancies = append(discrepancies, LegacyDiscrepancy{
			Code: LegacyInfoHashMismatch, Expected: source.InfoHashV1.Hex(), Actual: parsed.InfoHashV1.Hex(),
		})
	}
	if source.SizeBytes != parsed.TotalSizeBytes {
		discrepancies = append(discrepancies, LegacyDiscrepancy{
			Code:     LegacySizeMismatch,
			Expected: strconv.FormatInt(source.SizeBytes, 10),
			Actual:   strconv.FormatInt(parsed.TotalSizeBytes, 10),
		})
	}
	if source.FileCount != len(parsed.Files) {
		discrepancies = append(discrepancies, LegacyDiscrepancy{
			Code:     LegacyFileCountMismatch,
			Expected: strconv.Itoa(source.FileCount),
			Actual:   strconv.Itoa(len(parsed.Files)),
		})
	}
	return discrepancies
}

// MapPtYesState translates the observed PtYes status vocabulary into the new
// explicit state machine. Empty legacy status was publicly listed by PtYes and
// is therefore treated as published; unknown values fail closed for a manual
// mapping decision. Soft deletion takes precedence over review status.
func MapPtYesState(status string, softDeleted bool) (State, error) {
	if softDeleted {
		return StateDeleted, nil
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "approved":
		return StatePublished, nil
	case "pending", "revision":
		return StatePendingReview, nil
	case "rejected":
		return StateRejected, nil
	default:
		return "", fmt.Errorf("map PtYes torrent state %q: %w", status, ErrTorrentInputInvalid)
	}
}
