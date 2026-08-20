// Package protocol owns the byte-level HTTP Tracker contract. It parses the
// raw query itself because net/url form decoding turns '+' into a space and is
// therefore unsafe for the 20-byte info_hash and peer_id fields.
package protocol

import (
	"errors"
	"strconv"
	"strings"
)

const (
	MaxRawQueryBytes = 4096
	MaxQueryFields   = 32
	MaxKeyBytes      = 128
)

var ErrInvalidAnnounce = errors.New("announce query is invalid")

type Event uint8

const (
	EventNone Event = iota
	EventStarted
	EventStopped
	EventCompleted
)

type AnnounceRequest struct {
	InfoHash   [20]byte
	PeerID     [20]byte
	Port       uint16
	Uploaded   int64
	Downloaded int64
	Left       int64
	Event      Event
	Compact    bool
	NumWant    int
	Key        string
}

type AnnounceParser struct {
	DefaultNumWant int
	MaxNumWant     int
}

func NewAnnounceParser(defaultNumWant, maxNumWant int) (AnnounceParser, error) {
	if defaultNumWant < 0 || maxNumWant < 1 || defaultNumWant > maxNumWant || maxNumWant > 500 {
		return AnnounceParser{}, ErrInvalidAnnounce
	}
	return AnnounceParser{DefaultNumWant: defaultNumWant, MaxNumWant: maxNumWant}, nil
}

// Parse accepts unknown keys for real-world client compatibility, but known
// singleton keys may appear only once and every recognized value is bounded.
func (parser AnnounceParser) Parse(rawQuery string) (AnnounceRequest, error) {
	if len(rawQuery) == 0 || len(rawQuery) > MaxRawQueryBytes || parser.MaxNumWant < 1 ||
		parser.DefaultNumWant < 0 || parser.DefaultNumWant > parser.MaxNumWant {
		return AnnounceRequest{}, ErrInvalidAnnounce
	}
	segments := strings.Split(rawQuery, "&")
	if len(segments) > MaxQueryFields {
		return AnnounceRequest{}, ErrInvalidAnnounce
	}
	request := AnnounceRequest{Compact: true, NumWant: parser.DefaultNumWant}
	seen := make(map[string]struct{}, 10)
	for _, segment := range segments {
		if segment == "" {
			return AnnounceRequest{}, ErrInvalidAnnounce
		}
		name, rawValue, found := strings.Cut(segment, "=")
		if !found || name == "" {
			return AnnounceRequest{}, ErrInvalidAnnounce
		}
		switch name {
		case "info_hash", "peer_id", "port", "uploaded", "downloaded", "left", "event", "compact", "numwant", "key":
			if _, duplicate := seen[name]; duplicate {
				return AnnounceRequest{}, ErrInvalidAnnounce
			}
			seen[name] = struct{}{}
		default:
			continue
		}
		switch name {
		case "info_hash":
			decoded, err := percentDecode(rawValue, len(request.InfoHash))
			if err != nil || len(decoded) != len(request.InfoHash) {
				return AnnounceRequest{}, ErrInvalidAnnounce
			}
			copy(request.InfoHash[:], decoded)
		case "peer_id":
			decoded, err := percentDecode(rawValue, len(request.PeerID))
			if err != nil || len(decoded) != len(request.PeerID) {
				return AnnounceRequest{}, ErrInvalidAnnounce
			}
			copy(request.PeerID[:], decoded)
		case "port":
			value, err := parseUnsigned(rawValue, 65535)
			if err != nil || value == 0 {
				return AnnounceRequest{}, ErrInvalidAnnounce
			}
			request.Port = uint16(value)
		case "uploaded":
			value, err := parseUnsigned(rawValue, uint64(^uint64(0)>>1))
			if err != nil {
				return AnnounceRequest{}, ErrInvalidAnnounce
			}
			request.Uploaded = int64(value)
		case "downloaded":
			value, err := parseUnsigned(rawValue, uint64(^uint64(0)>>1))
			if err != nil {
				return AnnounceRequest{}, ErrInvalidAnnounce
			}
			request.Downloaded = int64(value)
		case "left":
			value, err := parseUnsigned(rawValue, uint64(^uint64(0)>>1))
			if err != nil {
				return AnnounceRequest{}, ErrInvalidAnnounce
			}
			request.Left = int64(value)
		case "event":
			switch rawValue {
			case "":
				request.Event = EventNone
			case "started":
				request.Event = EventStarted
			case "stopped":
				request.Event = EventStopped
			case "completed":
				request.Event = EventCompleted
			default:
				return AnnounceRequest{}, ErrInvalidAnnounce
			}
		case "compact":
			switch rawValue {
			case "0":
				request.Compact = false
			case "1":
				request.Compact = true
			default:
				return AnnounceRequest{}, ErrInvalidAnnounce
			}
		case "numwant":
			value, err := parseUnsigned(rawValue, uint64(^uint(0)>>1))
			if err != nil {
				return AnnounceRequest{}, ErrInvalidAnnounce
			}
			request.NumWant = min(int(value), parser.MaxNumWant)
		case "key":
			decoded, err := percentDecode(rawValue, MaxKeyBytes)
			if err != nil || len(decoded) > MaxKeyBytes {
				return AnnounceRequest{}, ErrInvalidAnnounce
			}
			request.Key = string(decoded)
		}
	}
	for _, required := range []string{"info_hash", "peer_id", "port", "uploaded", "downloaded", "left"} {
		if _, exists := seen[required]; !exists {
			return AnnounceRequest{}, ErrInvalidAnnounce
		}
	}
	if request.Event == EventStopped {
		request.NumWant = 0
	}
	return request, nil
}

func percentDecode(value string, maximum int) ([]byte, error) {
	if len(value) > maximum*3 || maximum < 0 {
		return nil, ErrInvalidAnnounce
	}
	decoded := make([]byte, 0, min(len(value), maximum))
	for index := 0; index < len(value); index++ {
		if len(decoded) >= maximum {
			return nil, ErrInvalidAnnounce
		}
		if value[index] != '%' {
			// '+' is deliberately copied literally; this is not form decoding.
			decoded = append(decoded, value[index])
			continue
		}
		if index+2 >= len(value) {
			return nil, ErrInvalidAnnounce
		}
		high, okHigh := fromHex(value[index+1])
		low, okLow := fromHex(value[index+2])
		if !okHigh || !okLow {
			return nil, ErrInvalidAnnounce
		}
		decoded = append(decoded, high<<4|low)
		index += 2
	}
	return decoded, nil
}

func fromHex(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

func parseUnsigned(value string, maximum uint64) (uint64, error) {
	if value == "" || len(value) > 20 {
		return 0, ErrInvalidAnnounce
	}
	for index := range value {
		if value[index] < '0' || value[index] > '9' {
			return 0, ErrInvalidAnnounce
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed > maximum {
		return 0, ErrInvalidAnnounce
	}
	return parsed, nil
}
