package traffic

import (
	"encoding/base64"
	"encoding/binary"
	"math"
	"time"

	"github.com/google/uuid"
)

const (
	hnrCursorVersion = byte(1)
	hnrCursorBytes   = 1 + 8 + 16
)

// EncodeHNRCursor produces a fixed-size opaque keyset cursor. It contains only
// the already-public ordering key and is versioned so a future representation
// cannot be confused with the first contract.
func EncodeHNRCursor(cursor HNRCursor) (string, error) {
	completedAt := cursor.CompletedAt.UTC().Truncate(time.Microsecond)
	micros := completedAt.UnixMicro()
	if cursor.CompletedAt.IsZero() || !cursor.CompletedAt.Equal(completedAt) || micros <= 0 || cursor.ObligationID == uuid.Nil {
		return "", ErrInput
	}
	payload := make([]byte, hnrCursorBytes)
	payload[0] = hnrCursorVersion
	binary.BigEndian.PutUint64(payload[1:9], uint64(micros))
	copy(payload[9:], cursor.ObligationID[:])
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// DecodeHNRCursor rejects non-canonical encodings and unknown versions before
// the repository sees any ordering values.
func DecodeHNRCursor(encoded string) (*HNRCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) != hnrCursorBytes || payload[0] != hnrCursorVersion ||
		base64.RawURLEncoding.EncodeToString(payload) != encoded {
		return nil, ErrInput
	}
	encodedMicros := binary.BigEndian.Uint64(payload[1:9])
	if encodedMicros == 0 || encodedMicros > math.MaxInt64 {
		return nil, ErrInput
	}
	obligationID, err := uuid.FromBytes(payload[9:])
	if err != nil || obligationID == uuid.Nil {
		return nil, ErrInput
	}
	return &HNRCursor{
		CompletedAt:  time.UnixMicro(int64(encodedMicros)).UTC(),
		ObligationID: obligationID,
	}, nil
}
