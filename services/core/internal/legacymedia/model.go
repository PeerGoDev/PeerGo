package legacymedia

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	EntityTorrentImage  = "torrent_image"
	EntityTorrentPoster = "torrent_poster"
	imageManifestDomain = "peergo:migration:ptyes-torrent-images:v1\x00"
	imageRowDomain      = "peergo:migration:ptyes-torrent-image-row:v1\x00"
)

type InventoryConfig struct {
	RunID          uuid.UUID
	SnapshotSHA256 [sha256.Size]byte
	MappingVersion string
}

type SourceImage struct {
	EntityKind      string
	LegacyID        int64
	LegacyTorrentID int64
	LegacyPath      string
	Position        int16
	IsCover         bool
	SortOrder       int64
	OriginalSHA256  [sha256.Size]byte
	OriginalBytes   int64
	SourceMetadata  ImageMetadata
	State           string
	ErrorCode       string
}

func (image SourceImage) fingerprint() ([sha256.Size]byte, error) {
	if image.LegacyID < 1 || image.LegacyTorrentID < 1 || image.Position < 0 || image.Position > 5 ||
		(image.EntityKind != EntityTorrentImage && image.EntityKind != EntityTorrentPoster) ||
		!strings.HasPrefix(image.LegacyPath, "/uploads/images/") {
		return [sha256.Size]byte{}, errors.New("legacy image source row is invalid")
	}
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(imageRowDomain))
	writeString(hasher, TransformPolicyVersion)
	writeString(hasher, image.EntityKind)
	writeInt64(hasher, image.LegacyID)
	writeInt64(hasher, image.LegacyTorrentID)
	writeString(hasher, image.LegacyPath)
	writeInt64(hasher, int64(image.Position))
	writeBool(hasher, image.IsCover)
	writeInt64(hasher, image.SortOrder)
	_, _ = hasher.Write(image.OriginalSHA256[:])
	writeInt64(hasher, image.OriginalBytes)
	writeString(hasher, image.SourceMetadata.ContentType)
	writeString(hasher, image.SourceMetadata.Extension)
	writeInt64(hasher, int64(image.SourceMetadata.Width))
	writeInt64(hasher, int64(image.SourceMetadata.Height))
	writeString(hasher, image.State)
	writeString(hasher, image.ErrorCode)
	var result [sha256.Size]byte
	copy(result[:], hasher.Sum(nil))
	return result, nil
}

type ValidationConfig struct {
	Inventory     InventoryConfig
	ImageArchive  string
	ArchiveSHA256 [sha256.Size]byte
	OccurredAt    time.Time
	ProgressEvery int64
}

type ValidationProgress struct {
	Processed int64
	Expected  int64
}

type ValidationResult struct {
	RunID                     uuid.UUID
	ArchiveImages             int64
	ReferencedImages          int64
	ImportableImages          int64
	ExcludedTorrentImages     int64
	MissingPosterPlaceholders int64
	UnreferencedArchiveImages int64
	OriginalBytes             int64
	ManifestSHA256            [sha256.Size]byte
	MissingImageLegacyIDs     []int64
}

type ValidationFailure struct {
	MissingImageRows int64
	LegacyIDs        []int64
}

func (problem *ValidationFailure) Error() string {
	return "PtYes torrent image validation found missing or invalid source objects"
}

type byteWriter interface {
	Write([]byte) (int, error)
}

func writeInt64(writer byteWriter, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = writer.Write(encoded[:])
}

func writeString(writer byteWriter, value string) {
	writeInt64(writer, int64(len(value)))
	_, _ = writer.Write([]byte(value))
}

func writeBool(writer byteWriter, value bool) {
	if value {
		_, _ = writer.Write([]byte{1})
		return
	}
	_, _ = writer.Write([]byte{0})
}
