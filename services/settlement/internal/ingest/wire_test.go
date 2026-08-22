package ingest

import (
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackerannouncev2"
)

func TestDecodeAnnounceAcceptsLegacyAndSequencedCanonicalEvents(t *testing.T) {
	t.Parallel()
	legacy := trackerannouncev1.Event{
		SchemaVersion: trackerannouncev1.SchemaVersion,
		EventID:       "0198f20a-6da8-7e51-9c64-111111111111",
		ReceivedAt:    time.Date(2026, 8, 22, 1, 0, 0, 0, time.UTC),
		UserID:        "0198f20a-6da8-7e51-9c64-222222222222", TorrentID: 42,
		InfoHashV1:    "0100000000000000000000000000000000000000",
		SessionToken:  "0200000000000000000000000000000000000000000000000000000000000000",
		AddressFamily: 4, Uploaded: 1, Downloaded: 2, Left: 3,
		CredentialVersion: 1, TorrentControlSequence: 4, SubjectControlSequence: 5,
	}
	legacyPayload, err := trackerannouncev1.Encode(legacy)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeAnnounce(legacyPayload)
	if err != nil || decoded.Event != legacy || decoded.Producer != nil {
		t.Fatalf("legacy decoded=%+v error=%v", decoded, err)
	}
	sequenced, err := trackerannouncev2.FromV1(
		legacy, "tracker-primary", "0198f20a-6da8-7e51-9c64-333333333333", 9,
	)
	if err != nil {
		t.Fatal(err)
	}
	v2Payload, err := trackerannouncev2.Encode(sequenced)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = decodeAnnounce(v2Payload)
	if err != nil || decoded.Event != legacy || decoded.Producer == nil || decoded.Producer.Sequence != 9 {
		t.Fatalf("v2 decoded=%+v error=%v", decoded, err)
	}
}
