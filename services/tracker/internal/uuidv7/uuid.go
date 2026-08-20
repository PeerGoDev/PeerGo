package uuidv7

import (
	"encoding/hex"
	"errors"
	"io"
	"time"
)

var ErrInvalid = errors.New("UUIDv7 input is invalid")

// New creates a UUIDv7 whose timestamp comes from the supplied observation.
// Callers inject the random source so deterministic contract tests do not need
// to weaken the production generator.
func New(at time.Time, random io.Reader) (string, error) {
	milliseconds := at.UTC().UnixMilli()
	if random == nil || milliseconds < 0 || uint64(milliseconds) >= 1<<48 {
		return "", ErrInvalid
	}
	var raw [16]byte
	if _, err := io.ReadFull(random, raw[:]); err != nil {
		return "", err
	}
	raw[0] = byte(milliseconds >> 40)
	raw[1] = byte(milliseconds >> 32)
	raw[2] = byte(milliseconds >> 24)
	raw[3] = byte(milliseconds >> 16)
	raw[4] = byte(milliseconds >> 8)
	raw[5] = byte(milliseconds)
	raw[6] = raw[6]&0x0f | 0x70
	raw[8] = raw[8]&0x3f | 0x80
	var compact [32]byte
	hex.Encode(compact[:], raw[:])
	formatted := make([]byte, 36)
	copy(formatted[0:8], compact[0:8])
	formatted[8] = '-'
	copy(formatted[9:13], compact[8:12])
	formatted[13] = '-'
	copy(formatted[14:18], compact[12:16])
	formatted[18] = '-'
	copy(formatted[19:23], compact[16:20])
	formatted[23] = '-'
	copy(formatted[24:36], compact[20:32])
	return string(formatted), nil
}
