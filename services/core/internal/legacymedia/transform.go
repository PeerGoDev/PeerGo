package legacymedia

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"

	_ "golang.org/x/image/webp"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
)

const (
	// Changing source-retention semantics changes every snapshot-bound image
	// fingerprint, so an older normalized-image run cannot silently resume as a
	// source-preserving run.
	TransformPolicyVersion = "ptyes-source-v2"
	maxStoredPixels        = 100_000_000
)

type ImageMetadata struct {
	ContentType string
	Extension   string
	Width       int
	Height      int
}

type StoredImage struct {
	Bytes      []byte
	Metadata   ImageMetadata
	Descriptor objectstorage.Descriptor
}

func ValidateSourceImage(raw []byte, extension string) (ImageMetadata, error) {
	return inspectImage(raw, extension, maxStoredPixels)
}

func inspectImage(raw []byte, extension string, maxPixels int64) (ImageMetadata, error) {
	if len(raw) < 1 || len(raw) > maxSourceImageBytes {
		return ImageMetadata{}, errors.New("legacy image byte length is invalid")
	}
	contentType := http.DetectContentType(raw)
	expectedType, expectedFormat, canonicalExtension, ok := supportedLegacyImageExtension(extension)
	if !ok || contentType != expectedType {
		return ImageMetadata{}, errors.New("legacy image extension does not match decoded content")
	}
	configuration, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || format != expectedFormat || configuration.Width < 1 || configuration.Height < 1 ||
		configuration.Width > 32768 || configuration.Height > 32768 ||
		int64(configuration.Width)*int64(configuration.Height) > maxPixels {
		return ImageMetadata{}, errors.New("legacy image dimensions or encoding are invalid")
	}
	return ImageMetadata{
		ContentType: contentType, Extension: canonicalExtension,
		Width: configuration.Width, Height: configuration.Height,
	}, nil
}

func supportedLegacyImageExtension(extension string) (string, string, string, bool) {
	switch strings.ToLower(extension) {
	case ".jpg", ".jpeg":
		return "image/jpeg", "jpeg", ".jpg", true
	case ".png":
		return "image/png", "png", ".png", true
	case ".webp":
		return "image/webp", "webp", ".webp", true
	case ".gif":
		return "image/gif", "gif", ".gif", true
	default:
		return "", "", "", false
	}
}

func buildStoredImage(raw []byte, metadata ImageMetadata) (StoredImage, error) {
	if len(raw) < 1 || len(raw) > 16<<20 {
		return StoredImage{}, errors.New("legacy source image exceeds the stored byte policy")
	}
	digest := objectstorage.SHA256(sha256.Sum256(raw))
	return StoredImage{
		Bytes: append([]byte(nil), raw...), Metadata: metadata,
		Descriptor: objectstorage.Descriptor{SHA256: digest, ByteLength: int64(len(raw))},
	}, nil
}
