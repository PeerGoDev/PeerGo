package protocol

import (
	"bytes"
	"testing"
)

func TestScrapeParserAcceptsRepeatedBinaryInfoHashes(t *testing.T) {
	parser, err := NewScrapeParser(2)
	if err != nil {
		t.Fatal(err)
	}
	hashes, err := parser.Parse("info_hash=12345678901234567890&info_hash=abcdefghijklmnopqrst")
	if err != nil || len(hashes) != 2 || string(hashes[1][:]) != "abcdefghijklmnopqrst" {
		t.Fatalf("unexpected scrape parse: %v %+v", err, hashes)
	}
}

func TestEncodeScrapeSortsBinaryDictionaryKeys(t *testing.T) {
	var first, second [20]byte
	copy(first[:], "bbbbbbbbbbbbbbbbbbbb")
	copy(second[:], "aaaaaaaaaaaaaaaaaaaa")
	encoded, err := EncodeScrape([]ScrapeStat{
		{InfoHash: first, Complete: 1, Downloaded: 3, Incomplete: 2},
		{InfoHash: second, Complete: 4, Downloaded: 6, Incomplete: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Index(encoded, second[:]) > bytes.Index(encoded, first[:]) {
		t.Fatalf("scrape dictionary keys were not sorted: %q", encoded)
	}
}
