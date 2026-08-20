package announceevent

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/peergo/peergo/services/tracker/internal/protocol"
)

func TestFactoryBuildsCanonicalPrivacyMinimizedEvent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 8, 23, 30, 0, 123, time.UTC)
	event, err := NewFactory(bytes.NewReader(bytes.Repeat([]byte{0x81}, 16))).New(Input{
		ReceivedAt: now, UserID: "0198f20a-6da8-7e51-9c64-111111111111", TorrentID: 42,
		InfoHash: [20]byte{1}, PeerID: [20]byte{2}, Key: "client-key", AddressFamily: 4,
		Event: protocol.EventCompleted, Uploaded: 100, Downloaded: 200, Left: 0,
		CredentialVersion: 1, TorrentControlSequence: 8, SubjectControlSequence: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || decoded.EventID != event.EventID || event.EventID[14] != '7' || event.Event != "completed" {
		t.Fatalf("decoded=%+v error=%v", decoded, err)
	}
	for _, forbidden := range []string{"passkey", "peer_ip", "port", "client-key"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("event leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestFactoryDerivesStableCompletionIdentityAcrossRequestRetries(t *testing.T) {
	t.Parallel()
	factory := NewFactory(bytes.NewReader(bytes.Repeat([]byte{0x72}, 64)))
	input := Input{
		ReceivedAt: time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC),
		UserID:     "0198f20a-6da8-7e51-9c64-111111111111", TorrentID: 42,
		InfoHash: [20]byte{1}, PeerID: [20]byte{2}, Key: "client-key", AddressFamily: 4,
		Event: protocol.EventCompleted, Downloaded: 200, Left: 0, CompletionToken: [32]byte{9},
		CredentialVersion: 1, TorrentControlSequence: 8, SubjectControlSequence: 3,
	}
	first, err := factory.New(input)
	if err != nil {
		t.Fatal(err)
	}
	input.ReceivedAt = input.ReceivedAt.Add(time.Second)
	second, err := factory.New(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.EventID == second.EventID || first.CompletionID == "" || first.CompletionID != second.CompletionID {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	input.CompletionToken = [32]byte{10}
	third, err := factory.New(input)
	if err != nil {
		t.Fatal(err)
	}
	if third.CompletionID == first.CompletionID {
		t.Fatal("distinct completion transition tokens produced the same identity")
	}
}
