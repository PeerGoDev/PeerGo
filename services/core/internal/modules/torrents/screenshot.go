package torrents

import (
	"bytes"
	"crypto/sha256"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"

	"github.com/google/uuid"
	_ "golang.org/x/image/webp"
)

const (
	MaxTorrentScreenshots = 6
	// New source images are retained, so live uploads use a stricter 2 MiB
	// admission limit. The wider stored limit below exists only for finite
	// legacy migration and older already-accepted objects.
	MaxTorrentScreenshotBytes = 2 << 20
	// MaxStoredTorrentScreenshotBytes is wider than live admission only for
	// verified legacy objects. New uploads remain capped by the constant above.
	MaxStoredTorrentScreenshotBytes = 16 << 20
	maxScreenshotDimension          = 32768
	maxScreenshotPixels             = 25_000_000
)

type TorrentScreenshotInput struct {
	Raw []byte
}

// TorrentScreenshot is validated media evidence attached in an explicit
// order. The first row is the cover; user filenames and browser MIME claims
// deliberately never enter the domain model.
type TorrentScreenshot struct {
	ID            uuid.UUID
	ContentSHA256 ObjectSHA256
	ByteLength    int64
	ContentType   string
	Extension     string
	Width         int
	Height        int
	Position      int
}

type preparedTorrentScreenshot struct {
	TorrentScreenshot
	Raw []byte
}

type storedTorrentScreenshot struct {
	TorrentScreenshot
	BackendID        StorageBackendID
	ObjectKey        ObjectKey
	StorageVersionID string
}

func prepareTorrentScreenshots(inputs []TorrentScreenshotInput, newUUID func() uuid.UUID) ([]preparedTorrentScreenshot, error) {
	if len(inputs) > MaxTorrentScreenshots || newUUID == nil {
		return nil, ErrTorrentInputInvalid
	}
	result := make([]preparedTorrentScreenshot, 0, len(inputs))
	seen := make(map[ObjectSHA256]struct{}, len(inputs))
	for position, input := range inputs {
		raw := input.Raw
		if len(raw) < 1 || len(raw) > MaxTorrentScreenshotBytes {
			return nil, validationFailure(CodeObjectTooLarge, "screenshots", position, "screenshot exceeds the upload byte policy")
		}
		contentType := http.DetectContentType(raw)
		extension, expectedFormat, ok := supportedScreenshotType(contentType)
		if !ok {
			return nil, validationFailure(CodeInvalidScreenshot, "screenshots", position, "unsupported screenshot content type")
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil || format != expectedFormat || config.Width < 1 || config.Height < 1 ||
			config.Width > maxScreenshotDimension || config.Height > maxScreenshotDimension ||
			int64(config.Width)*int64(config.Height) > maxScreenshotPixels {
			return nil, validationFailure(CodeInvalidScreenshot, "screenshots", position, "invalid screenshot dimensions or encoding")
		}
		digest := ObjectSHA256(sha256.Sum256(raw))
		if _, duplicate := seen[digest]; duplicate {
			return nil, validationFailure(CodeInvalidScreenshot, "screenshots", position, "duplicate screenshot")
		}
		seen[digest] = struct{}{}
		result = append(result, preparedTorrentScreenshot{
			TorrentScreenshot: TorrentScreenshot{
				ID: newUUID(), ContentSHA256: digest, ByteLength: int64(len(raw)),
				ContentType: contentType, Extension: extension,
				Width: config.Width, Height: config.Height, Position: position,
			},
			Raw: append([]byte(nil), raw...),
		})
	}
	return result, nil
}

func supportedScreenshotType(contentType string) (extension, format string, ok bool) {
	switch contentType {
	case "image/jpeg":
		return ".jpg", "jpeg", true
	case "image/png":
		return ".png", "png", true
	case "image/webp":
		return ".webp", "webp", true
	default:
		return "", "", false
	}
}

func supportedStoredScreenshotType(contentType string) bool {
	if contentType == "image/gif" {
		return true
	}
	_, _, ok := supportedScreenshotType(contentType)
	return ok
}

func screenshotMetadata(prepared []preparedTorrentScreenshot) []TorrentScreenshot {
	result := make([]TorrentScreenshot, 0, len(prepared))
	for _, screenshot := range prepared {
		result = append(result, screenshot.TorrentScreenshot)
	}
	return result
}

func validTorrentScreenshots(screenshots []TorrentScreenshot) bool {
	if len(screenshots) > MaxTorrentScreenshots {
		return false
	}
	seen := make(map[ObjectSHA256]struct{}, len(screenshots))
	for position, screenshot := range screenshots {
		if screenshot.ID == uuid.Nil || screenshot.ContentSHA256 == (ObjectSHA256{}) ||
			screenshot.ByteLength < 1 || screenshot.ByteLength > MaxTorrentScreenshotBytes ||
			screenshot.Position != position || screenshot.Width < 1 || screenshot.Height < 1 ||
			screenshot.Width > maxScreenshotDimension || screenshot.Height > maxScreenshotDimension ||
			int64(screenshot.Width)*int64(screenshot.Height) > maxScreenshotPixels {
			return false
		}
		extension, _, ok := supportedScreenshotType(screenshot.ContentType)
		if !ok || extension != screenshot.Extension {
			return false
		}
		if _, duplicate := seen[screenshot.ContentSHA256]; duplicate {
			return false
		}
		seen[screenshot.ContentSHA256] = struct{}{}
	}
	return true
}
