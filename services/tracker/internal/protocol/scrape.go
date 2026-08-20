package protocol

import (
	"bytes"
	"errors"
	"sort"
	"strings"
)

const MaxScrapeRawQueryBytes = 8192

var ErrInvalidScrape = errors.New("scrape query is invalid")

type ScrapeParser struct{ MaxInfoHashes int }

func NewScrapeParser(maxInfoHashes int) (ScrapeParser, error) {
	if maxInfoHashes < 1 || maxInfoHashes > 100 {
		return ScrapeParser{}, ErrInvalidScrape
	}
	return ScrapeParser{MaxInfoHashes: maxInfoHashes}, nil
}

// Parse preserves raw binary info hashes and permits the one scrape key to be
// repeated, as required by BEP 48. Duplicate hashes are rejected so one client
// cannot spend the response budget on identical work.
func (parser ScrapeParser) Parse(rawQuery string) ([][20]byte, error) {
	if rawQuery == "" || len(rawQuery) > MaxScrapeRawQueryBytes || parser.MaxInfoHashes < 1 || parser.MaxInfoHashes > 100 {
		return nil, ErrInvalidScrape
	}
	segments := strings.Split(rawQuery, "&")
	if len(segments) > parser.MaxInfoHashes {
		return nil, ErrInvalidScrape
	}
	result := make([][20]byte, 0, len(segments))
	seen := make(map[[20]byte]struct{}, len(segments))
	for _, segment := range segments {
		name, rawValue, found := strings.Cut(segment, "=")
		if !found || name != "info_hash" {
			return nil, ErrInvalidScrape
		}
		decoded, err := percentDecode(rawValue, 20)
		if err != nil || len(decoded) != 20 {
			return nil, ErrInvalidScrape
		}
		var hash [20]byte
		copy(hash[:], decoded)
		if _, duplicate := seen[hash]; duplicate {
			return nil, ErrInvalidScrape
		}
		seen[hash] = struct{}{}
		result = append(result, hash)
	}
	if len(result) == 0 {
		return nil, ErrInvalidScrape
	}
	return result, nil
}

type ScrapeStat struct {
	InfoHash   [20]byte
	Complete   int
	Incomplete int
	Downloaded int64
}

func EncodeScrape(stats []ScrapeStat) ([]byte, error) {
	if len(stats) > 100 {
		return nil, ErrInvalidScrape
	}
	ordered := append([]ScrapeStat(nil), stats...)
	sort.Slice(ordered, func(i, j int) bool { return bytes.Compare(ordered[i].InfoHash[:], ordered[j].InfoHash[:]) < 0 })
	encoded := []byte("d5:filesd")
	var previous [20]byte
	for index, stat := range ordered {
		if stat.Complete < 0 || stat.Incomplete < 0 || stat.Downloaded < 0 ||
			(index > 0 && bytes.Equal(previous[:], stat.InfoHash[:])) {
			return nil, ErrInvalidScrape
		}
		encoded = appendBytes(encoded, stat.InfoHash[:])
		encoded = append(encoded, "d8:complete"...)
		encoded = appendInteger(encoded, stat.Complete)
		encoded = append(encoded, "10:downloaded"...)
		encoded = appendInteger64(encoded, stat.Downloaded)
		encoded = append(encoded, "10:incomplete"...)
		encoded = appendInteger(encoded, stat.Incomplete)
		encoded = append(encoded, 'e')
		previous = stat.InfoHash
	}
	return append(encoded, 'e', 'e'), nil
}
