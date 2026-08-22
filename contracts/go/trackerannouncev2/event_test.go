package trackerannouncev2

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
)

func TestSequencedEventRoundTripsAndPreservesV1Meaning(t *testing.T) {
	t.Parallel()
	legacy := trackerannouncev1.Event{
		SchemaVersion: trackerannouncev1.SchemaVersion,
		EventID:       "0198f20a-6da8-7e51-9c64-111111111111",
		ReceivedAt:    time.Date(2026, 8, 22, 1, 0, 0, 0, time.UTC),
		UserID:        "0198f20a-6da8-7e51-9c64-222222222222",
		TorrentID:     42,
		InfoHashV1:    "0100000000000000000000000000000000000000",
		SessionToken:  "0200000000000000000000000000000000000000000000000000000000000000",
		AddressFamily: 4, Event: "started", Uploaded: 1, Downloaded: 2, Left: 3,
		CredentialVersion: 1, TorrentControlSequence: 4, SubjectControlSequence: 5,
	}
	event, err := FromV1(legacy, "tracker-primary", "0198f20a-6da8-7e51-9c64-333333333333", 7)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || decoded.ProducerSequence != 7 || decoded.ToV1() != legacy {
		t.Fatalf("decoded=%+v error=%v", decoded, err)
	}
	unknown := append(bytes.TrimSuffix(encoded, []byte("}")), []byte(`,"unknown":true}`)...)
	if _, err := Decode(unknown); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown field error=%v", err)
	}
}

func TestSequencedEventRejectsInvalidProducerTuple(t *testing.T) {
	t.Parallel()
	base := trackerannouncev1.Event{
		SchemaVersion: trackerannouncev1.SchemaVersion,
		EventID:       "0198f20a-6da8-7e51-9c64-111111111111",
		ReceivedAt:    time.Date(2026, 8, 22, 1, 0, 0, 0, time.UTC),
		UserID:        "0198f20a-6da8-7e51-9c64-222222222222", TorrentID: 1,
		InfoHashV1:    "0100000000000000000000000000000000000000",
		SessionToken:  "0200000000000000000000000000000000000000000000000000000000000000",
		AddressFamily: 4, CredentialVersion: 1, TorrentControlSequence: 1, SubjectControlSequence: 1,
	}
	for _, candidate := range []struct {
		id    string
		epoch string
		seq   int64
	}{{"Tracker", "0198f20a-6da8-7e51-9c64-333333333333", 1}, {"tracker", "not-an-epoch", 1}, {"tracker", "0198f20a-6da8-7e51-9c64-333333333333", 0}} {
		if _, err := FromV1(base, candidate.id, candidate.epoch, candidate.seq); !errors.Is(err, ErrInvalid) {
			t.Fatalf("candidate=%+v error=%v", candidate, err)
		}
	}
}
