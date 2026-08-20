package torrents

import (
	"bytes"
	"errors"
	"net/url"
	"sort"
	"strconv"
)

const (
	maxAnnounceTiers       = 4
	maxAnnounceURLsPerTier = 4
	maxAnnounceURLBytes    = 2048
	maxDownloadCopyGrowth  = 32 << 10
)

type downloadMetainfoEntry struct {
	key      []byte
	rawValue []byte
}

// RewriteTrackerAnnounce rebuilds only the outer metainfo dictionary. Every
// untouched value is copied byte-for-byte from the verified object, especially
// the complete info value whose exact bencoded bytes define the v1 swarm hash.
// Existing announce-list values are removed so a fresh download cannot retain
// an old domain or another user's embedded passkey.
func RewriteTrackerAnnounce(raw []byte, expectedInfoOffset, expectedInfoLength int64, tiers [][]string) ([]byte, error) {
	if len(raw) == 0 || len(raw) > MaxMetainfoBytes || expectedInfoOffset < 0 || expectedInfoLength <= 0 ||
		expectedInfoOffset > int64(len(raw))-expectedInfoLength {
		return nil, ErrTorrentInputInvalid
	}
	if err := validateAnnounceTiers(tiers); err != nil {
		return nil, err
	}
	root, _, err := decodeBencode(raw, ValidationProfileLegacyImport)
	if err != nil {
		return nil, err
	}
	if root.kind != bencodeDictionary {
		return nil, ErrTorrentInputInvalid
	}
	info, exists := root.get("info")
	if !exists || info.kind != bencodeDictionary || int64(info.start) != expectedInfoOffset || int64(info.end-info.start) != expectedInfoLength {
		return nil, errors.New("metainfo info span conflicts with immutable manifest")
	}

	entries := make([]downloadMetainfoEntry, 0, len(root.entries)+1)
	for _, entry := range root.entries {
		if bytes.Equal(entry.key, []byte("announce")) || bytes.Equal(entry.key, []byte("announce-list")) {
			continue
		}
		entries = append(entries, downloadMetainfoEntry{
			key:      append([]byte(nil), entry.key...),
			rawValue: raw[entry.value.start:entry.value.end],
		})
	}
	entries = append(entries, downloadMetainfoEntry{
		key:      []byte("announce"),
		rawValue: appendBencodedBytes(nil, []byte(tiers[0][0])),
	})
	if len(tiers) > 1 || len(tiers[0]) > 1 {
		entries = append(entries, downloadMetainfoEntry{
			key:      []byte("announce-list"),
			rawValue: appendBencodedAnnounceTiers(nil, tiers),
		})
	}
	sort.Slice(entries, func(left, right int) bool {
		return bytes.Compare(entries[left].key, entries[right].key) < 0
	})

	result := make([]byte, 0, len(raw)+1024)
	result = append(result, 'd')
	for _, entry := range entries {
		result = appendBencodedBytes(result, entry.key)
		result = append(result, entry.rawValue...)
	}
	result = append(result, 'e')
	if len(result) > MaxMetainfoBytes+maxDownloadCopyGrowth {
		return nil, ErrTorrentInputInvalid
	}
	return result, nil
}

func validateAnnounceTiers(tiers [][]string) error {
	if len(tiers) < 1 || len(tiers) > maxAnnounceTiers {
		return ErrTorrentInputInvalid
	}
	seen := make(map[string]struct{})
	for _, tier := range tiers {
		if len(tier) < 1 || len(tier) > maxAnnounceURLsPerTier {
			return ErrTorrentInputInvalid
		}
		for _, rawURL := range tier {
			if len(rawURL) < 1 || len(rawURL) > maxAnnounceURLBytes {
				return ErrTorrentInputInvalid
			}
			parsed, err := url.Parse(rawURL)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil ||
				parsed.RawQuery != "" || parsed.Fragment != "" ||
				(parsed.Scheme != "http" && parsed.Scheme != "https") {
				return ErrTorrentInputInvalid
			}
			if _, duplicate := seen[parsed.String()]; duplicate {
				return ErrTorrentInputInvalid
			}
			seen[parsed.String()] = struct{}{}
		}
	}
	return nil
}

func appendBencodedAnnounceTiers(destination []byte, tiers [][]string) []byte {
	destination = append(destination, 'l')
	for _, tier := range tiers {
		destination = append(destination, 'l')
		for _, announceURL := range tier {
			destination = appendBencodedBytes(destination, []byte(announceURL))
		}
		destination = append(destination, 'e')
	}
	return append(destination, 'e')
}

func appendBencodedBytes(destination, value []byte) []byte {
	destination = strconv.AppendInt(destination, int64(len(value)), 10)
	destination = append(destination, ':')
	return append(destination, value...)
}
