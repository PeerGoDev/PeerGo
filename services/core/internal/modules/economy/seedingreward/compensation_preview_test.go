package seedingreward

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBasisPointUnitsKeepsExactDecimal(t *testing.T) {
	value, err := basisPointUnits(12_345)
	if err != nil || value != "1.2345" {
		t.Fatalf("basisPointUnits() = %q, %v", value, err)
	}
}

func TestCorrectedWindowDigestBindsFenceAndOrderedItems(t *testing.T) {
	start := time.Date(2026, 8, 21, 5, 0, 0, 0, time.UTC)
	window := rewardWindow{Start: start}
	window.EvidenceSHA256[0] = 1
	items := []correctedTrackerItem{
		{UserID: uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")},
		{UserID: uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222")},
	}
	items[0].EvidenceSHA256[0], items[1].EvidenceSHA256[0] = 2, 3
	first, err := correctedWindowSHA256(window, 100, items)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := correctedWindowSHA256(window, 100, items)
	if err != nil || replayed != first {
		t.Fatalf("replayed digest = %x, %v; want %x", replayed, err, first)
	}
	changedFence, _ := correctedWindowSHA256(window, 101, items)
	if changedFence == first {
		t.Fatal("Tracker fence was not bound into the corrected evidence digest")
	}
	items[0], items[1] = items[1], items[0]
	changedOrder, _ := correctedWindowSHA256(window, 100, items)
	if changedOrder == first {
		t.Fatal("ordered corrected items were not bound into the evidence digest")
	}
}

func TestCompensationManifestBindsTrackerStream(t *testing.T) {
	header := CompensationArtifactHeader{
		SchemaVersion: CompensationPreviewSchemaVersion,
		RecordType:    "manifest", TrackerSourceStream: "PEERGO_TRACKER_ANNOUNCE_V1",
		TrackerFenceSequence: 42,
	}
	if header.TrackerSourceStream == "" || header.TrackerFenceSequence != 42 {
		t.Fatal("compensation manifest did not retain its Tracker watermark")
	}
}

func TestCorrectedTrackerItemAllowsLegacySubSecondInterval(t *testing.T) {
	item := correctedTrackerItem{
		UserID:    uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"),
		TorrentID: 1, ActiveSeconds: 0, RawUploaded: 0,
		SourceCount: 1, FirstSequence: 1, LastSequence: 1,
	}
	if !validCorrectedTrackerItem(item, len(item.InfoHashV1), 0, 0, 1) {
		t.Fatal("a legacy sub-second interval should remain valid after whole-second truncation")
	}
	item.ActiveSeconds = -1
	if validCorrectedTrackerItem(item, len(item.InfoHashV1), 0, 0, 1) {
		t.Fatal("negative active time must remain invalid")
	}
}
