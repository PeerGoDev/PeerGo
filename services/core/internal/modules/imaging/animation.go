package imaging

import (
	"bytes"
	"encoding/binary"
)

const (
	maxAnimationFrames = 120
	maxAnimationPixels = 80_000_000
)

// IsAnimatedWebP reads the animation flag in the first RIFF chunk.
func IsAnimatedWebP(raw []byte) bool {
	return len(raw) >= 30 && string(raw[:4]) == "RIFF" &&
		string(raw[8:16]) == "WEBPVP8X" && binary.LittleEndian.Uint32(raw[16:20]) == 10 &&
		raw[20]&0x02 != 0
}

// ScreenshotFrameCount bounds native decoding using the number of frames and
// the canvas size. It skips compressed frame data without allocating pixels.
func ScreenshotFrameCount(raw []byte, format string, width, height int) (int, error) {
	frames := 1
	switch {
	case format == "gif":
		frames = gifFrames(raw)
	case format == "webp" && IsAnimatedWebP(raw):
		frames = webpFrames(raw)
	}
	if frames < 1 || frames > maxAnimationFrames ||
		(frames > 1 && int64(width)*int64(height)*int64(frames) > maxAnimationPixels) {
		return 0, ErrInput
	}
	return frames, nil
}

func gifFrames(raw []byte) int {
	if len(raw) < 13 {
		return 0
	}
	offset := 13 + gifColorTableSize(raw[10])
	frames := 0
	for offset < len(raw) {
		block := raw[offset]
		offset++
		switch block {
		case 0x3b: // Trailer.
			return frames
		case 0x21: // Extension label followed by sub-blocks.
			offset++
		case 0x2c: // Image descriptor, optional palette, and LZW code size.
			if len(raw)-offset < 10 {
				return 0
			}
			offset += 10 + gifColorTableSize(raw[offset+8])
			frames++
			if frames > maxAnimationFrames {
				return frames
			}
		default:
			return 0
		}
		for {
			if offset >= len(raw) {
				return 0
			}
			size := int(raw[offset])
			offset += 1 + size
			if size == 0 {
				break
			}
		}
	}
	return 0
}

func gifColorTableSize(flags byte) int {
	if flags&0x80 == 0 {
		return 0
	}
	return 3 * (2 << (flags & 7))
}

func webpFrames(raw []byte) int {
	if uint64(binary.LittleEndian.Uint32(raw[4:8]))+8 != uint64(len(raw)) {
		return 0
	}
	frames := 0
	for offset := 12; offset < len(raw); {
		if len(raw)-offset < 8 {
			return 0
		}
		size := int64(binary.LittleEndian.Uint32(raw[offset+4 : offset+8]))
		paddedSize := size + size%2
		if paddedSize > int64(len(raw)-offset-8) {
			return 0
		}
		if bytes.Equal(raw[offset:offset+4], []byte("ANMF")) {
			if size < 16 {
				return 0
			}
			frames++
			if frames > maxAnimationFrames {
				return frames
			}
		}
		offset += 8 + int(paddedSize)
	}
	return frames
}
