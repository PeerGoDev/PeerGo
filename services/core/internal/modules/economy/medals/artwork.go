package medals

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"path"
	"strings"

	_ "golang.org/x/image/webp"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/imaging"
)

const (
	MaxArtworkBytes     = 2 << 20
	artworkDirectory    = "medals/"
	maxArtworkDimension = 2048
)

var (
	ErrArtworkTooLarge    = errors.New("medal artwork exceeds byte limit")
	ErrArtworkUnavailable = errors.New("medal artwork storage is unavailable")
)

type Artwork struct {
	Data        []byte
	ContentType string
	Digest      string
}

// SetArtworkStore wires the durable object store at startup.
func (service *Service) SetArtworkStore(store objectstorage.Store) {
	service.artworkStore = store
}

func (service *Service) UploadArtwork(ctx context.Context, actor authz.StaffActor, updating bool, input io.Reader) (string, error) {
	action := authz.ActionEconomyMedalCreate
	if updating {
		action = authz.ActionEconomyMedalUpdate
	}
	if _, err := authz.AuthorizeStaffAction(ctx, service.authorizer, actor, action, authz.SiteScope(), service.now().UTC(), "economy-medal-artwork"); err != nil {
		return "", err
	}
	if service.artworkStore == nil {
		return "", ErrArtworkUnavailable
	}
	if input == nil {
		return "", ErrInput
	}
	raw, err := io.ReadAll(io.LimitReader(input, MaxArtworkBytes+1))
	if err != nil {
		return "", fmt.Errorf("read medal artwork: %w", err)
	}
	if len(raw) > MaxArtworkBytes {
		return "", ErrArtworkTooLarge
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > maxArtworkDimension || config.Height > maxArtworkDimension {
		return "", ErrInput
	}
	extension := "." + format
	if format == "jpeg" {
		extension = ".jpg"
	}
	if artworkContentType(extension) == "" {
		return "", ErrInput
	}
	frames, err := imaging.ScreenshotFrameCount(raw, format, config.Width, config.Height)
	if err != nil {
		return "", ErrInput
	}
	if frames == 1 {
		if _, _, err := image.Decode(bytes.NewReader(raw)); err != nil {
			return "", ErrInput
		}
	}
	digest := objectstorage.SHA256(sha256.Sum256(raw))
	filename := digest.Hex() + extension
	key := objectstorage.Key(artworkDirectory + filename)
	descriptor := objectstorage.Descriptor{SHA256: digest, ByteLength: int64(len(raw))}
	written, err := service.artworkStore.PutIfAbsent(ctx, key, bytes.NewReader(raw), descriptor)
	if err != nil {
		return "", fmt.Errorf("store medal artwork: %w", err)
	}
	if _, err := service.readArtwork(ctx, key, written.VersionID, digest, artworkContentType(extension)); err != nil {
		return "", err
	}
	return filename, nil
}

func (service *Service) ReadArtwork(ctx context.Context, filename string) (Artwork, error) {
	if service.artworkStore == nil {
		return Artwork{}, ErrArtworkUnavailable
	}
	extension := path.Ext(filename)
	contentType := artworkContentType(extension)
	hash := strings.TrimSuffix(filename, extension)
	var digest objectstorage.SHA256
	if len(hash) != hex.EncodedLen(len(digest)) || contentType == "" || strings.ToLower(hash) != hash {
		return Artwork{}, ErrNotFound
	}
	if _, err := hex.Decode(digest[:], []byte(hash)); err != nil {
		return Artwork{}, ErrNotFound
	}
	return service.readArtwork(ctx, objectstorage.Key(artworkDirectory+filename), "", digest, contentType)
}

func (service *Service) readArtwork(ctx context.Context, key objectstorage.Key, version string, digest objectstorage.SHA256, contentType string) (Artwork, error) {
	object, err := service.artworkStore.Open(ctx, key, version)
	if errors.Is(err, objectstorage.ErrNotFound) {
		return Artwork{}, ErrNotFound
	}
	if err != nil {
		return Artwork{}, fmt.Errorf("open medal artwork: %w", err)
	}
	if object.Body == nil {
		return Artwork{}, objectstorage.ErrObjectConflict
	}
	defer object.Body.Close()
	if object.ByteLength < 1 || object.ByteLength > MaxArtworkBytes {
		return Artwork{}, objectstorage.ErrObjectConflict
	}
	raw, err := objectstorage.ReadAllVerified(object, objectstorage.Descriptor{SHA256: digest, ByteLength: object.ByteLength})
	if err != nil {
		return Artwork{}, fmt.Errorf("verify medal artwork: %w", err)
	}
	return Artwork{Data: raw, ContentType: contentType, Digest: digest.Hex()}, nil
}

func artworkContentType(extension string) string {
	switch extension {
	case ".jpg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}
