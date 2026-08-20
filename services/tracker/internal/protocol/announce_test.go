package protocol

import (
	"bytes"
	"net/netip"
	"strings"
	"testing"
)

func TestAnnounceParserPreservesBinaryPlusAndBoundsValues(t *testing.T) {
	t.Parallel()
	parser, err := NewAnnounceParser(50, 100)
	if err != nil {
		t.Fatal(err)
	}
	raw := "info_hash=%00aaaaaaaaaaaaaaaaaa+&peer_id=-qB123-%00aaaaaaaaaaaa&port=6881&uploaded=12&downloaded=34&left=56&event=started&compact=1&numwant=999&key=a%2Bb&ip=198.51.100.2"
	request, err := parser.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if request.InfoHash[0] != 0 || request.InfoHash[19] != '+' || request.PeerID[7] != 0 ||
		request.Port != 6881 || request.Uploaded != 12 || request.Downloaded != 34 || request.Left != 56 ||
		request.Event != EventStarted || !request.Compact || request.NumWant != 100 || request.Key != "a+b" {
		t.Fatalf("request = %+v", request)
	}
}

func TestAnnounceParserRejectsMalformedOrAmbiguousQueries(t *testing.T) {
	t.Parallel()
	parser, _ := NewAnnounceParser(50, 100)
	valid := "info_hash=aaaaaaaaaaaaaaaaaaaa&peer_id=bbbbbbbbbbbbbbbbbbbb&port=6881&uploaded=0&downloaded=0&left=1"
	for name, raw := range map[string]string{
		"duplicate":     valid + "&port=6882",
		"short_hash":    strings.Replace(valid, "aaaaaaaaaaaaaaaaaaaa", "short", 1),
		"bad_escape":    strings.Replace(valid, "aaaaaaaaaaaaaaaaaaaa", "%zz", 1),
		"negative":      strings.Replace(valid, "uploaded=0", "uploaded=-1", 1),
		"overflow":      strings.Replace(valid, "left=1", "left=9223372036854775808", 1),
		"event":         valid + "&event=paused",
		"empty_segment": valid + "&",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parser.Parse(raw); err == nil {
				t.Fatalf("Parse(%q) unexpectedly succeeded", raw)
			}
		})
	}
}

func TestEncodeAnnounceCompactAndNonCompact(t *testing.T) {
	t.Parallel()
	peers := []Peer{
		{ID: [20]byte{1}, Endpoint: netip.MustParseAddrPort("192.0.2.1:6881")},
		{ID: [20]byte{2}, Endpoint: netip.MustParseAddrPort("[2001:db8::1]:51413")},
	}
	compact, err := EncodeAnnounce(AnnounceResponse{
		Interval: 1800, MinInterval: 900, Complete: 2, Incomplete: 3, Peers: peers,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(compact, []byte("6:peers618:")) || !bytes.Contains(compact, []byte{192, 0, 2, 1, 0x1a, 0xe1}) ||
		!bytes.HasSuffix(compact, []byte("e")) {
		t.Fatalf("compact response = %q", compact)
	}
	nonCompact, err := EncodeAnnounce(AnnounceResponse{
		Interval: 1800, MinInterval: 900, Complete: 2, Incomplete: 3, Peers: peers,
	}, false)
	if err != nil || !bytes.Contains(nonCompact, []byte("9:192.0.2.1")) || !bytes.Contains(nonCompact, []byte("11:2001:db8::1")) {
		t.Fatalf("non-compact response = %q, %v", nonCompact, err)
	}
	if failure := string(EncodeFailure("access denied")); failure != "d14:failure reason13:access deniede" {
		t.Fatalf("failure = %q", failure)
	}
}
