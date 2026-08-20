package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/imaging"
)

type avatarMemoryStore struct {
	backend objectstorage.BackendID
	objects map[objectstorage.Key][]byte
}

func (store *avatarMemoryStore) BackendID() objectstorage.BackendID { return store.backend }

func (store *avatarMemoryStore) PutIfAbsent(_ context.Context, key objectstorage.Key, source io.Reader, expected objectstorage.Descriptor) (objectstorage.WriteResult, error) {
	contents, err := io.ReadAll(source)
	if err != nil || int64(len(contents)) != expected.ByteLength || objectstorage.SHA256(sha256.Sum256(contents)) != expected.SHA256 {
		return objectstorage.WriteResult{}, objectstorage.ErrObjectConflict
	}
	if _, exists := store.objects[key]; exists {
		return objectstorage.WriteResult{Created: false}, nil
	}
	store.objects[key] = contents
	return objectstorage.WriteResult{Created: true}, nil
}

func (store *avatarMemoryStore) Open(_ context.Context, key objectstorage.Key, _ string) (objectstorage.Reader, error) {
	contents, exists := store.objects[key]
	if !exists {
		return objectstorage.Reader{}, objectstorage.ErrNotFound
	}
	return objectstorage.Reader{Body: io.NopCloser(bytes.NewReader(contents)), ByteLength: int64(len(contents))}, nil
}

func (store *avatarMemoryStore) Delete(_ context.Context, key objectstorage.Key, _ string) error {
	delete(store.objects, key)
	return nil
}

type avatarRepositoryStub struct {
	saved  StoredAvatar
	source AvatarSource
}

func (repository *avatarRepositoryStub) SaveUserAvatar(_ context.Context, avatar StoredAvatar) error {
	repository.saved = avatar
	repository.source = AvatarSource{
		ObjectID:   avatar.ObjectID,
		Descriptor: avatar.Descriptor, ContentType: avatar.ContentType, Extension: avatar.Extension,
		Width: avatar.Width, Height: avatar.Height, BackendID: avatar.BackendID, ObjectKey: avatar.ObjectKey,
		VersionID: avatar.VersionID, UpdatedAt: avatar.UpdatedAt,
	}
	return nil
}

type avatarDerivativeFixture struct {
	ready    imaging.ReadyDerivative
	sourceID uuid.UUID
	variant  imaging.Variant
}

func (fixture *avatarDerivativeFixture) ReadyForAvatar(_ context.Context, sourceID uuid.UUID, variant imaging.Variant) (imaging.ReadyDerivative, error) {
	fixture.sourceID, fixture.variant = sourceID, variant
	return fixture.ready, nil
}

func (repository *avatarRepositoryStub) PublicUserAvatar(context.Context, string, time.Time) (AvatarSource, error) {
	return repository.source, nil
}

func TestAvatarServiceStoresVerifiesAndReadsNewPeerGoAvatar(t *testing.T) {
	now := time.Date(2026, time.August, 13, 10, 0, 0, 0, time.UTC)
	userID := uuid.New()
	sessions := &recordingProfileSessions{session: WebSession{User: User{ID: userID}}}
	repository := &avatarRepositoryStub{}
	store := &avatarMemoryStore{backend: "local-primary", objects: make(map[objectstorage.Key][]byte)}
	stores, err := objectstorage.NewRegistry(store)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	service, err := NewAvatarService(sessions, repository, &recordingProfileAuthorizer{now: now}, stores, AvatarServiceConfig{
		ActiveBackendID: store.backend, Now: func() time.Time { return now }, NewUUID: uuid.New,
	})
	if err != nil {
		t.Fatalf("NewAvatarService() error = %v", err)
	}
	contents := squareJPEG(t, 64)
	revision, err := service.UpdateAvatar(context.Background(), "cookie", "csrf", bytes.NewReader(contents))
	if err != nil {
		t.Fatalf("UpdateAvatar() error = %v", err)
	}
	if revision.Revision != repository.saved.Descriptor.SHA256.Hex() || repository.saved.UserID != userID || repository.saved.Width != 64 || repository.saved.Height != 64 {
		t.Fatalf("revision=%+v saved=%+v", revision, repository.saved)
	}
	avatar, err := service.PublicAvatar(context.Background(), "cookie", "member")
	if err != nil {
		t.Fatalf("PublicAvatar() error = %v", err)
	}
	if !bytes.Equal(avatar.Data, contents) || avatar.ContentType != "image/jpeg" || avatar.Revision != revision.Revision {
		t.Fatalf("avatar=%+v", avatar)
	}
}

func TestAvatarServicePrefersVerifiedDisplayDerivative(t *testing.T) {
	now := time.Date(2026, time.August, 18, 1, 30, 0, 0, time.UTC)
	userID := uuid.New()
	sessions := &recordingProfileSessions{session: WebSession{User: User{ID: userID}}}
	repository := &avatarRepositoryStub{}
	store := &avatarMemoryStore{backend: "local-primary", objects: make(map[objectstorage.Key][]byte)}
	stores, err := objectstorage.NewRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	derivativeBytes := []byte("avatar-webp-derivative")
	digest := objectstorage.SHA256(sha256.Sum256(derivativeBytes))
	key, err := objectstorage.ParseKey("image-derivatives/webp-v1/sha256/aa/avatar.webp")
	if err != nil {
		t.Fatal(err)
	}
	store.objects[key] = derivativeBytes
	derivatives := &avatarDerivativeFixture{ready: imaging.ReadyDerivative{
		ObjectID:   uuid.New(),
		Descriptor: objectstorage.Descriptor{SHA256: digest, ByteLength: int64(len(derivativeBytes))},
		Width:      128, Height: 128,
		Locations: []imaging.Location{{BackendID: store.backend, ObjectKey: key, VerifiedAt: now}},
	}}
	service, err := NewAvatarService(sessions, repository, &recordingProfileAuthorizer{now: now}, stores, AvatarServiceConfig{
		ActiveBackendID: store.backend, Derivatives: derivatives,
		Now: func() time.Time { return now }, NewUUID: uuid.New,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateAvatar(context.Background(), "cookie", "csrf", bytes.NewReader(squareJPEG(t, 64))); err != nil {
		t.Fatal(err)
	}
	avatar, err := service.PublicAvatar(context.Background(), "cookie", "member")
	if err != nil || !bytes.Equal(avatar.Data, derivativeBytes) || avatar.ContentType != "image/webp" || avatar.Revision != digest.Hex() {
		t.Fatalf("PublicAvatar() = %+v, %v", avatar, err)
	}
	if derivatives.sourceID != repository.saved.ObjectID || derivatives.variant != imaging.VariantDisplay {
		t.Fatalf("derivative request = %s %s", derivatives.sourceID, derivatives.variant)
	}
}

func squareJPEG(t *testing.T, size int) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			canvas.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, canvas, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("jpeg.Encode() error = %v", err)
	}
	return output.Bytes()
}

var _ objectstorage.Store = (*avatarMemoryStore)(nil)
var _ AvatarRepository = (*avatarRepositoryStub)(nil)
var _ authz.Authorizer = (*recordingProfileAuthorizer)(nil)
