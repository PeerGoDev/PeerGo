package trackerannouncev1

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestEventCodecRequiresCanonicalStrictJSON(t *testing.T) {
	t.Parallel()
	event := contractTestEvent()
	encoded, err := Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || decoded != event {
		t.Fatalf("Decode() = %+v, %v", decoded, err)
	}
	withUnknown := append(bytes.TrimSuffix(encoded, []byte("}")), []byte(`,"unknown":true}`)...)
	if _, err := Decode(withUnknown); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown field error = %v", err)
	}
	nonCanonical := append([]byte(" "), encoded...)
	if _, err := Decode(nonCanonical); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-canonical error = %v", err)
	}
}

func TestBinaryIdentitiesAndMessagingNames(t *testing.T) {
	t.Parallel()
	event := contractTestEvent()
	if hash, err := DecodeInfoHashV1(event.InfoHashV1); err != nil || hash[0] != 1 {
		t.Fatalf("DecodeInfoHashV1() = %x, %v", hash, err)
	}
	if token, err := DecodeSessionToken(event.SessionToken); err != nil || token[0] != 2 {
		t.Fatalf("DecodeSessionToken() = %x, %v", token, err)
	}
	if !ValidStreamName(DefaultStream) || ValidStreamName("bad.name") || ValidStreamName("bad/name") {
		t.Fatal("stream name boundary is incorrect")
	}
	if !ValidLiteralSubject(DefaultSubject) || ValidLiteralSubject("peergo.tracker.*") || ValidLiteralSubject("peergo..announce") {
		t.Fatal("subject boundary is incorrect")
	}
}

func TestCompletionIdentityIsOptionalAndBoundToCompletedEvent(t *testing.T) {
	t.Parallel()
	event := contractTestEvent()
	event.Event = "completed"
	event.Left = 0
	event.CompletionID = "0300000000000000000000000000000000000000000000000000000000000000"
	if _, err := Encode(event); err != nil {
		t.Fatal(err)
	}
	event.Event = "started"
	if _, err := Encode(event); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid completion binding error=%v", err)
	}
}

func contractTestEvent() Event {
	return Event{
		SchemaVersion: SchemaVersion, EventID: "0198f20a-6da8-7e51-9c64-111111111111",
		ReceivedAt: time.Date(2026, 8, 8, 20, 0, 0, 0, time.UTC),
		UserID:     "0198f20a-6da8-7e51-9c64-222222222222", TorrentID: 42,
		InfoHashV1:    "0100000000000000000000000000000000000000",
		SessionToken:  "0200000000000000000000000000000000000000000000000000000000000000",
		AddressFamily: 4, Event: "started", Uploaded: 1, Downloaded: 2, Left: 3,
		CredentialVersion: 1, TorrentControlSequence: 4, SubjectControlSequence: 5,
	}
}
