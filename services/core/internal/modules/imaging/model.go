// Package imaging owns replaceable WebP derivatives of immutable image
// sources. Torrent and identity domains continue to own their original bytes;
// this package only records reproducible display objects and processing state.
package imaging

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
)

const (
	PolicyVersion  = "webp-v1"
	MaxAttempts    = 5
	MaxOutputBytes = 16 << 20
)

var (
	ErrInput             = errors.New("image derivative input is invalid")
	ErrNotFound          = errors.New("image derivative was not found")
	ErrLeaseConflict     = errors.New("image derivative processing lease changed")
	ErrSourceUnavailable = errors.New("image derivative source is unavailable")
	ErrSourceConflict    = errors.New("image derivative source failed integrity verification")
	ErrOutputConflict    = errors.New("image derivative output conflicts with immutable metadata")
)

type SourceKind string

const (
	SourceTorrentScreenshot SourceKind = "torrent_screenshot"
	SourceAvatar            SourceKind = "avatar"
)

func (kind SourceKind) Valid() bool {
	return kind == SourceTorrentScreenshot || kind == SourceAvatar
}

type Variant string

const (
	VariantThumbnail Variant = "thumbnail"
	VariantDisplay   Variant = "display"
	VariantLarge     Variant = "large"
)

func (variant Variant) Valid() bool {
	return variant == VariantThumbnail || variant == VariantDisplay || variant == VariantLarge
}

type Location struct {
	BackendID  objectstorage.BackendID
	ObjectKey  objectstorage.Key
	VersionID  string
	VerifiedAt time.Time
}

type Source struct {
	Kind        SourceKind
	ObjectID    uuid.UUID
	Descriptor  objectstorage.Descriptor
	ContentType string
	Extension   string
	Width       int
	Height      int
	Locations   []Location
}

type Job struct {
	ID           uuid.UUID
	SourceKind   SourceKind
	SourceObject uuid.UUID
	Variant      Variant
	AttemptCount int
	LeaseToken   uuid.UUID
	LeaseUntil   time.Time
}

type Output struct {
	Descriptor objectstorage.Descriptor
	Width      int
	Height     int
	Bytes      []byte
}

type ReadyDerivative struct {
	ObjectID   uuid.UUID
	Descriptor objectstorage.Descriptor
	Width      int
	Height     int
	Locations  []Location
}

type QueueOverview struct {
	PolicyVersion   string
	Pending         int64
	Processing      int64
	Retrying        int64
	Ready           int64
	Dead            int64
	SourceObjects   int64
	OutputObjects   int64
	OutputBytes     int64
	OldestPendingAt *time.Time
	LastErrorCode   string
	LastErrorAt     *time.Time
}

type TransformProfile struct {
	MaxWidth  int
	MaxHeight int
	Quality   int
}

func Profile(kind SourceKind, variant Variant) (TransformProfile, error) {
	if !kind.Valid() || !variant.Valid() {
		return TransformProfile{}, ErrInput
	}
	if kind == SourceAvatar {
		size := map[Variant]int{
			VariantThumbnail: 64,
			VariantDisplay:   128,
			VariantLarge:     256,
		}[variant]
		return TransformProfile{MaxWidth: size, MaxHeight: size, Quality: 82}, nil
	}
	switch variant {
	case VariantThumbnail:
		return TransformProfile{MaxWidth: 320, MaxHeight: 480, Quality: 76}, nil
	case VariantDisplay:
		return TransformProfile{MaxWidth: 1280, MaxHeight: 1280, Quality: 82}, nil
	case VariantLarge:
		return TransformProfile{MaxWidth: 2560, MaxHeight: 2560, Quality: 85}, nil
	default:
		return TransformProfile{}, ErrInput
	}
}
