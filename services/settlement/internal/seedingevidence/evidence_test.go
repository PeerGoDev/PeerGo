package seedingevidence

import (
	"bytes"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAssembleItemsUnionsOverlappingSessionsAndKeepsSources(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	infoHash := [20]byte{1, 2, 3}
	facts := []intervalFact{
		{EventID: uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222"), UserID: userID, TorrentID: 42, InfoHashV1: infoHash, StartsAt: start.Add(5 * time.Minute), EndsAt: start.Add(20 * time.Minute), RawUploaded: 100, SourceSequence: 2},
		{EventID: uuid.MustParse("0198f20a-6da8-7e51-9c64-333333333333"), UserID: userID, TorrentID: 42, InfoHashV1: infoHash, StartsAt: start.Add(10 * time.Minute), EndsAt: start.Add(30 * time.Minute), RawUploaded: 200, SourceSequence: 3},
		{EventID: uuid.MustParse("0198f20a-6da8-7e51-9c64-444444444444"), UserID: userID, TorrentID: 42, InfoHashV1: infoHash, StartsAt: start.Add(40 * time.Minute), EndsAt: start.Add(50 * time.Minute), RawUploaded: 300, SourceSequence: 4},
	}
	items, err := assembleItems(facts, map[[20]byte]snapshotCounts{infoHash: {Seeders: 3, Leechers: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ActiveSeconds != 35*60 || items[0].RawUploaded != 600 ||
		items[0].Snapshot.Seeders != 3 || items[0].Snapshot.Leechers != 1 || len(items[0].Sources) != 3 ||
		!items[0].FirstActiveAt.Equal(start.Add(5*time.Minute)) || !items[0].LastActiveAt.Equal(start.Add(50*time.Minute)) {
		t.Fatalf("assembled item = %+v", items)
	}
}

func TestAssembleItemsDigestIsStableAcrossInputOrder(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	infoHash := [20]byte{4, 5, 6}
	facts := []intervalFact{
		{EventID: uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222"), UserID: userID, TorrentID: 42, InfoHashV1: infoHash, StartsAt: start, EndsAt: start.Add(10 * time.Minute), SourceSequence: 1},
		{EventID: uuid.MustParse("0198f20a-6da8-7e51-9c64-333333333333"), UserID: userID, TorrentID: 42, InfoHashV1: infoHash, StartsAt: start.Add(20 * time.Minute), EndsAt: start.Add(30 * time.Minute), SourceSequence: 2},
	}
	first, err := assembleItems(facts, nil)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(facts)
	second, err := assembleItems(facts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 || !bytes.Equal(first[0].EvidenceSHA256[:], second[0].EvidenceSHA256[:]) {
		t.Fatalf("digests differ: %x != %x", first[0].EvidenceSHA256, second[0].EvidenceSHA256)
	}
}

func TestAssembleItemsRejectsTorrentInfoHashDrift(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	facts := []intervalFact{
		{EventID: uuid.New(), UserID: userID, TorrentID: 42, InfoHashV1: [20]byte{1}, StartsAt: start, EndsAt: start.Add(time.Minute), SourceSequence: 1},
		{EventID: uuid.New(), UserID: userID, TorrentID: 42, InfoHashV1: [20]byte{2}, StartsAt: start.Add(time.Minute), EndsAt: start.Add(2 * time.Minute), SourceSequence: 2},
	}
	if _, err := assembleItems(facts, nil); err == nil {
		t.Fatal("assembleItems() accepted one torrent ID with two info hashes")
	}
}
