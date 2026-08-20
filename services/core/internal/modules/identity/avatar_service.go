package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	_ "golang.org/x/image/webp"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/imaging"
)

const (
	MaxAvatarBytes      = 1 << 20
	maxAvatarDimension  = 1024
	minAvatarDimension  = 32
	avatarContentType   = "image/jpeg"
	avatarFileExtension = ".jpg"
)

var (
	ErrAvatarNotFound           = errors.New("public user avatar was not found")
	ErrAvatarTooLarge           = errors.New("avatar upload is too large")
	ErrAvatarStorageUnavailable = errors.New("avatar object storage is unavailable")
	ErrAvatarConflict           = errors.New("avatar object conflicts with immutable metadata")
)

type AvatarRevision struct {
	Revision  string
	UpdatedAt time.Time
}

type PublicAvatar struct {
	Data        []byte
	ContentType string
	Revision    string
	UpdatedAt   time.Time
}

type StoredAvatar struct {
	UserID      uuid.UUID
	ObjectID    uuid.UUID
	Descriptor  objectstorage.Descriptor
	ContentType string
	Extension   string
	Width       int32
	Height      int32
	BackendID   objectstorage.BackendID
	ObjectKey   objectstorage.Key
	VersionID   string
	UpdatedAt   time.Time
}

type AvatarSource struct {
	ObjectID    uuid.UUID
	Descriptor  objectstorage.Descriptor
	ContentType string
	Extension   string
	Width       int32
	Height      int32
	BackendID   objectstorage.BackendID
	ObjectKey   objectstorage.Key
	VersionID   string
	UpdatedAt   time.Time
}

type AvatarRepository interface {
	SaveUserAvatar(context.Context, StoredAvatar) error
	PublicUserAvatar(context.Context, string, time.Time) (AvatarSource, error)
}

type AvatarDerivativeReader interface {
	ReadyForAvatar(context.Context, uuid.UUID, imaging.Variant) (imaging.ReadyDerivative, error)
}

type AvatarServiceConfig struct {
	ActiveBackendID objectstorage.BackendID
	Derivatives     AvatarDerivativeReader
	Now             func() time.Time
	NewUUID         func() uuid.UUID
}

// AvatarService owns the public avatar lifecycle independently from legacy
// profile images. PtYes image paths are intentionally never imported: new
// PeerGo uploads become immutable, verified objects with a current pointer.
type AvatarService struct {
	sessions        WebSessionAuthenticator
	repository      AvatarRepository
	authorizer      authz.Authorizer
	stores          *objectstorage.Registry
	activeBackendID objectstorage.BackendID
	derivatives     AvatarDerivativeReader
	now             func() time.Time
	newUUID         func() uuid.UUID
}

func NewAvatarService(sessions WebSessionAuthenticator, repository AvatarRepository, authorizer authz.Authorizer, stores *objectstorage.Registry, config AvatarServiceConfig) (*AvatarService, error) {
	if sessions == nil || repository == nil || authorizer == nil || stores == nil || config.ActiveBackendID == "" {
		return nil, errors.New("avatar service dependencies are required")
	}
	if _, ok := stores.Get(config.ActiveBackendID); !ok {
		return nil, errors.New("active avatar storage backend is not configured")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewUUID == nil {
		config.NewUUID = uuid.New
	}
	return &AvatarService{
		sessions: sessions, repository: repository, authorizer: authorizer, stores: stores,
		activeBackendID: config.ActiveBackendID, now: config.Now, newUUID: config.NewUUID,
		derivatives: config.Derivatives,
	}, nil
}

// Update authenticates before retaining bytes, validates decoded image
// dimensions, writes a deterministic immutable key, then performs a complete
// storage read-back before the database pointer can change.
func (service *AvatarService) UpdateAvatar(ctx context.Context, cookieToken, csrfToken string, source io.Reader) (AvatarRevision, error) {
	if source == nil {
		return AvatarRevision{}, ErrInvalidInput
	}
	session, err := service.sessions.AuthenticateWrite(ctx, cookieToken, csrfToken)
	if err != nil {
		return AvatarRevision{}, err
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebSelfAction(ctx, service.authorizer, session.User.ID, authz.ActionAccountProfileUpdateSelf, now); err != nil {
		return AvatarRevision{}, err
	}
	contents, err := io.ReadAll(io.LimitReader(source, MaxAvatarBytes+1))
	if err != nil {
		return AvatarRevision{}, fmt.Errorf("read avatar upload: %w", err)
	}
	if len(contents) > MaxAvatarBytes {
		return AvatarRevision{}, ErrAvatarTooLarge
	}
	if len(contents) == 0 {
		return AvatarRevision{}, ErrInvalidInput
	}
	decoded, format, err := image.Decode(bytes.NewReader(contents))
	bounds := image.Rectangle{}
	if decoded != nil {
		bounds = decoded.Bounds()
	}
	width, height := bounds.Dx(), bounds.Dy()
	if err != nil || format != "jpeg" || width != height || width < minAvatarDimension || width > maxAvatarDimension {
		return AvatarRevision{}, ErrInvalidInput
	}
	digest := objectstorage.SHA256(sha256.Sum256(contents))
	descriptor := objectstorage.Descriptor{SHA256: digest, ByteLength: int64(len(contents))}
	key, err := objectstorage.ParseKey("avatars/sha256/" + digest.Hex()[:2] + "/" + digest.Hex() + avatarFileExtension)
	if err != nil {
		return AvatarRevision{}, fmt.Errorf("build avatar object key: %w", err)
	}
	store, _ := service.stores.Get(service.activeBackendID)
	write, err := store.PutIfAbsent(ctx, key, bytes.NewReader(contents), descriptor)
	if err != nil {
		return AvatarRevision{}, fmt.Errorf("%w: %v", ErrAvatarStorageUnavailable, err)
	}
	opened, err := store.Open(ctx, key, write.VersionID)
	if err != nil {
		return AvatarRevision{}, fmt.Errorf("%w: %v", ErrAvatarStorageUnavailable, err)
	}
	verifiedContents, verifyErr := objectstorage.ReadAllVerified(opened, descriptor)
	closeErr := opened.Body.Close()
	if verifyErr != nil || closeErr != nil || !bytes.Equal(verifiedContents, contents) {
		if write.Created {
			_ = store.Delete(ctx, key, write.VersionID)
		}
		return AvatarRevision{}, ErrAvatarConflict
	}
	versionID := opened.VersionID
	if versionID == "" {
		versionID = write.VersionID
	}
	record := StoredAvatar{
		UserID: session.User.ID, ObjectID: service.newUUID(), Descriptor: descriptor,
		ContentType: avatarContentType, Extension: avatarFileExtension,
		Width: int32(width), Height: int32(height),
		BackendID: service.activeBackendID, ObjectKey: key, VersionID: versionID, UpdatedAt: now,
	}
	if err := service.repository.SaveUserAvatar(ctx, record); err != nil {
		return AvatarRevision{}, fmt.Errorf("save current user avatar: %w", err)
	}
	return AvatarRevision{Revision: digest.Hex(), UpdatedAt: now}, nil
}

// Public authenticates the member read and re-verifies the selected physical
// location. Database metadata alone is never treated as proof of stored bytes.
func (service *AvatarService) PublicAvatar(ctx context.Context, cookieToken, username string) (PublicAvatar, error) {
	session, err := service.sessions.CurrentSession(ctx, cookieToken)
	if err != nil {
		return PublicAvatar{}, err
	}
	username = strings.TrimSpace(username)
	if username == "" || utf8.RuneCountInString(username) > 64 {
		return PublicAvatar{}, ErrInvalidInput
	}
	now := service.now().UTC()
	if _, err := authz.AuthorizeWebMemberAction(ctx, service.authorizer, session.User.ID, authz.ActionUserProfileReadMember, now); err != nil {
		return PublicAvatar{}, err
	}
	source, err := service.repository.PublicUserAvatar(ctx, username, now)
	if err != nil {
		return PublicAvatar{}, err
	}
	if service.derivatives != nil && source.ObjectID != uuid.Nil {
		derivative, derivativeErr := service.derivatives.ReadyForAvatar(ctx, source.ObjectID, imaging.VariantDisplay)
		if derivativeErr == nil {
			if contents, readErr := imaging.ReadReady(ctx, service.stores, derivative); readErr == nil {
				return PublicAvatar{
					Data: contents, ContentType: "image/webp",
					Revision: derivative.Descriptor.SHA256.Hex(), UpdatedAt: source.UpdatedAt,
				}, nil
			}
		}
	}
	store, ok := service.stores.Get(source.BackendID)
	if !ok {
		return PublicAvatar{}, ErrAvatarStorageUnavailable
	}
	opened, err := store.Open(ctx, source.ObjectKey, source.VersionID)
	if err != nil {
		return PublicAvatar{}, fmt.Errorf("%w: %v", ErrAvatarStorageUnavailable, err)
	}
	contents, verifyErr := objectstorage.ReadAllVerified(opened, source.Descriptor)
	closeErr := opened.Body.Close()
	if verifyErr != nil || closeErr != nil {
		return PublicAvatar{}, ErrAvatarConflict
	}
	return PublicAvatar{
		Data: contents, ContentType: source.ContentType, Revision: source.Descriptor.SHA256.Hex(), UpdatedAt: source.UpdatedAt,
	}, nil
}
