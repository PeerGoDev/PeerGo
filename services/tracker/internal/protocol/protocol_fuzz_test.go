package protocol

import (
	"bytes"
	"testing"
)

// FuzzAnnounceParser protects the raw-query boundary used by every supported
// HTTP tracker client. Rejected input is expected; accepted input must remain
// deterministic and satisfy the same hard bounds consumed by the hot path.
func FuzzAnnounceParser(f *testing.F) {
	for _, rawQuery := range []string{
		"info_hash=aaaaaaaaaaaaaaaaaaaa&peer_id=bbbbbbbbbbbbbbbbbbbb&port=6881&uploaded=0&downloaded=0&left=1",
		"info_hash=%00aaaaaaaaaaaaaaaaaa+&peer_id=-qB123-%00aaaaaaaaaaaa&port=6881&uploaded=12&downloaded=34&left=56&event=started&compact=1&numwant=999&key=a%2Bb",
		"info_hash=aaaaaaaaaaaaaaaaaaaa&peer_id=bbbbbbbbbbbbbbbbbbbb&port=51413&uploaded=1&downloaded=2&left=0&event=completed&compact=0",
		"",
		"info_hash=%zz",
	} {
		f.Add(rawQuery)
	}

	parser, err := NewAnnounceParser(50, 100)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, rawQuery string) {
		request, err := parser.Parse(rawQuery)
		if err != nil {
			return
		}
		repeated, err := parser.Parse(rawQuery)
		if err != nil || repeated != request {
			t.Fatalf("accepted announce query was not deterministic: first=%+v repeated=%+v err=%v", request, repeated, err)
		}
		if request.Port == 0 || request.Uploaded < 0 || request.Downloaded < 0 || request.Left < 0 ||
			request.NumWant < 0 || request.NumWant > parser.MaxNumWant || len(request.Key) > MaxKeyBytes {
			t.Fatalf("accepted announce query escaped parser bounds: %+v", request)
		}
		if request.Event == EventStopped && request.NumWant != 0 {
			t.Fatalf("stopped announce requested peers: %+v", request)
		}
	})
}

// FuzzScrapeParser covers BEP 48 repeated binary info_hash parameters without
// ever permitting anonymous full scrape, duplicates, or response amplification
// past the configured budget.
func FuzzScrapeParser(f *testing.F) {
	for _, rawQuery := range []string{
		"info_hash=12345678901234567890",
		"info_hash=12345678901234567890&info_hash=abcdefghijklmnopqrst",
		"info_hash=%00aaaaaaaaaaaaaaaaaa+",
		"",
		"info_hash=short",
	} {
		f.Add(rawQuery)
	}

	parser, err := NewScrapeParser(50)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, rawQuery string) {
		hashes, err := parser.Parse(rawQuery)
		if err != nil {
			return
		}
		if len(hashes) < 1 || len(hashes) > parser.MaxInfoHashes {
			t.Fatalf("accepted scrape query escaped hash budget: %d", len(hashes))
		}
		seen := make(map[[20]byte]struct{}, len(hashes))
		stats := make([]ScrapeStat, 0, len(hashes))
		for _, hash := range hashes {
			if _, duplicate := seen[hash]; duplicate {
				t.Fatalf("accepted scrape query contained a duplicate hash: %x", hash)
			}
			seen[hash] = struct{}{}
			stats = append(stats, ScrapeStat{InfoHash: hash})
		}
		repeated, err := parser.Parse(rawQuery)
		if err != nil || len(repeated) != len(hashes) {
			t.Fatalf("accepted scrape query was not repeatable: hashes=%d repeated=%d err=%v", len(hashes), len(repeated), err)
		}
		for index := range hashes {
			if !bytes.Equal(hashes[index][:], repeated[index][:]) {
				t.Fatalf("accepted scrape hash changed at index %d", index)
			}
		}
		if _, err := EncodeScrape(stats); err != nil {
			t.Fatalf("accepted scrape query could not be encoded: %v", err)
		}
	})
}
