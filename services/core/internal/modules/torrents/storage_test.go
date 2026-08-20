package torrents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStorageIdentifiersAndContentAddressedKey(t *testing.T) {
	t.Parallel()

	backendID, err := ParseStorageBackendID(" local-primary ")
	if err != nil || backendID != "local-primary" {
		t.Fatalf("ParseStorageBackendID() = %q, %v", backendID, err)
	}
	for _, invalid := range []string{"", "Local", "-local", "local/primary", "local primary"} {
		if _, err := ParseStorageBackendID(invalid); !errors.Is(err, ErrStorageInputInvalid) {
			t.Fatalf("ParseStorageBackendID(%q) error = %v", invalid, err)
		}
	}
	for _, invalid := range []string{"", "/absolute", "../escape", "safe/../escape", "safe\\escape", "safe//object"} {
		if _, err := ParseObjectKey(invalid); !errors.Is(err, ErrStorageInputInvalid) {
			t.Fatalf("ParseObjectKey(%q) error = %v", invalid, err)
		}
	}

	digest := ObjectSHA256(sha256.Sum256([]byte("torrent-object")))
	key := TorrentObjectKey(digest)
	if key != ObjectKey("torrents/sha256/5b/5bbc0a2087bcde4e9342d221a36d42a41cab68f6bcf511ba5be31d9276f7ed14.torrent") {
		t.Fatalf("TorrentObjectKey() = %q", key)
	}
}

func TestStorageMigrationCopiesAndReadbackVerifiesWithoutDeletingSource(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 14, 0, 0, 0, time.UTC)
	raw := []byte("immutable torrent bytes")
	descriptor := StoredObjectDescriptor{SHA256: ObjectSHA256(sha256.Sum256(raw)), ByteLength: int64(len(raw))}
	sourceKey, err := ParseObjectKey("legacy/42.torrent")
	if err != nil {
		t.Fatal(err)
	}
	source := newMemoryObjectStore("local-primary")
	source.objects[sourceKey] = append([]byte(nil), raw...)
	destination := newMemoryObjectStore("s3-primary")
	repository := &memoryStorageMigrationRepository{copyTasks: []StorageCopyTask{{
		MigrationID: uuid.New(), ObjectID: uuid.New(), Descriptor: descriptor,
		SourceBackendID: source.BackendID(), SourceObjectKey: sourceKey,
		DestinationBackendID: destination.BackendID(), LeaseToken: uuid.New(), Attempts: 1,
	}}}
	registry, err := NewStoreRegistry(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewStorageMigrationService(repository, registry, StorageMigrationServiceConfig{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	processed, err := service.RunCopyBatch(context.Background(), repository.copyTasks[0].MigrationID)
	if err != nil {
		t.Fatalf("RunCopyBatch() error = %v", err)
	}
	if processed != 1 || len(repository.verified) != 1 || len(repository.copyReleases) != 0 {
		t.Fatalf("processed=%d verified=%d releases=%d", processed, len(repository.verified), len(repository.copyReleases))
	}
	destinationKey := TorrentObjectKey(descriptor.SHA256)
	if !bytes.Equal(destination.objects[destinationKey], raw) {
		t.Fatalf("destination object = %q", destination.objects[destinationKey])
	}
	if source.deleteCalls != 0 {
		t.Fatalf("copy path deleted source %d times", source.deleteCalls)
	}
	if repository.verified[0].location.VerifiedAt != now || repository.verified[0].location.Descriptor != descriptor {
		t.Fatalf("verified location = %+v", repository.verified[0].location)
	}
}

func TestStorageMigrationRejectsCorruptDestinationReadback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 15, 0, 0, 0, time.UTC)
	raw := []byte("expected bytes")
	descriptor := StoredObjectDescriptor{SHA256: ObjectSHA256(sha256.Sum256(raw)), ByteLength: int64(len(raw))}
	source := newMemoryObjectStore("local-primary")
	sourceKey := ObjectKey("legacy/object.torrent")
	source.objects[sourceKey] = append([]byte(nil), raw...)
	destination := newMemoryObjectStore("s3-primary")
	destination.corruptReadback = true
	task := StorageCopyTask{
		MigrationID: uuid.New(), ObjectID: uuid.New(), Descriptor: descriptor,
		SourceBackendID: source.BackendID(), SourceObjectKey: sourceKey,
		DestinationBackendID: destination.BackendID(), LeaseToken: uuid.New(), Attempts: 2,
	}
	repository := &memoryStorageMigrationRepository{copyTasks: []StorageCopyTask{task}}
	registry, _ := NewStoreRegistry(source, destination)
	service, _ := NewStorageMigrationService(repository, registry, StorageMigrationServiceConfig{Now: func() time.Time { return now }})

	processed, err := service.RunCopyBatch(context.Background(), task.MigrationID)
	if processed != 1 || !errors.Is(err, ErrStoredObjectConflict) {
		t.Fatalf("RunCopyBatch() processed=%d error=%v", processed, err)
	}
	if len(repository.verified) != 0 || len(repository.copyReleases) != 1 {
		t.Fatalf("verified=%d releases=%d", len(repository.verified), len(repository.copyReleases))
	}
	if repository.copyReleases[0].code != "destination_verification_failed" || !repository.copyReleases[0].retryAt.Equal(now.Add(2*time.Second)) {
		t.Fatalf("release = %+v", repository.copyReleases[0])
	}
}

func TestStorageMigrationCutoverRequiresRetentionAndCleanupIsSeparate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 8, 16, 0, 0, 0, time.UTC)
	migrationID := uuid.New()
	objectID := uuid.New()
	source := newMemoryObjectStore("local-primary")
	sourceKey := ObjectKey("legacy/object.torrent")
	source.objects[sourceKey] = []byte("object")
	destination := newMemoryObjectStore("s3-primary")
	repository := &memoryStorageMigrationRepository{cleanupTasks: []StorageCleanupTask{{
		MigrationID: migrationID, ObjectID: objectID, SourceBackendID: source.BackendID(),
		SourceObjectKey: sourceKey, LeaseToken: uuid.New(), Attempts: 1,
	}}}
	registry, _ := NewStoreRegistry(source, destination)
	service, _ := NewStorageMigrationService(repository, registry, StorageMigrationServiceConfig{Now: func() time.Time { return now }})

	if err := service.Cutover(context.Background(), migrationID, time.Hour); !errors.Is(err, ErrStorageInputInvalid) {
		t.Fatalf("Cutover(short retention) error = %v", err)
	}
	if err := service.Cutover(context.Background(), migrationID, 7*24*time.Hour); err != nil {
		t.Fatalf("Cutover() error = %v", err)
	}
	if repository.cutover == nil || !repository.cutover.retentionUntil.Equal(now.Add(7*24*time.Hour)) {
		t.Fatalf("cutover = %+v", repository.cutover)
	}
	if source.deleteCalls != 0 {
		t.Fatal("Cutover() deleted source before cleanup")
	}

	processed, err := service.RunCleanupBatch(context.Background(), migrationID)
	if err != nil || processed != 1 {
		t.Fatalf("RunCleanupBatch() processed=%d error=%v", processed, err)
	}
	if source.deleteCalls != 1 || len(repository.deleted) != 1 {
		t.Fatalf("delete calls=%d deleted records=%d", source.deleteCalls, len(repository.deleted))
	}
	if _, exists := source.objects[sourceKey]; exists {
		t.Fatal("source object still exists after explicit cleanup")
	}
}

type memoryObjectStore struct {
	backendID       StorageBackendID
	objects         map[ObjectKey][]byte
	corruptReadback bool
	deleteCalls     int
	deleteErr       error
}

func newMemoryObjectStore(backendID StorageBackendID) *memoryObjectStore {
	return &memoryObjectStore{backendID: backendID, objects: make(map[ObjectKey][]byte)}
}

func (store *memoryObjectStore) BackendID() StorageBackendID { return store.backendID }

func (store *memoryObjectStore) PutIfAbsent(_ context.Context, key ObjectKey, source io.Reader, expected StoredObjectDescriptor) (ObjectWriteResult, error) {
	if _, exists := store.objects[key]; exists {
		return ObjectWriteResult{Created: false}, nil
	}
	contents, err := io.ReadAll(io.LimitReader(source, expected.ByteLength+1))
	if err != nil {
		return ObjectWriteResult{}, err
	}
	if int64(len(contents)) != expected.ByteLength || ObjectSHA256(sha256.Sum256(contents)) != expected.SHA256 {
		return ObjectWriteResult{}, ErrStoredObjectConflict
	}
	store.objects[key] = append([]byte(nil), contents...)
	return ObjectWriteResult{Created: true}, nil
}

func (store *memoryObjectStore) Open(_ context.Context, key ObjectKey, _ string) (StoredObjectReader, error) {
	contents, exists := store.objects[key]
	if !exists {
		return StoredObjectReader{}, ErrStoredObjectNotFound
	}
	contents = append([]byte(nil), contents...)
	if store.corruptReadback && len(contents) > 0 {
		contents[0] ^= 0xff
	}
	return StoredObjectReader{Body: io.NopCloser(bytes.NewReader(contents)), ByteLength: int64(len(contents))}, nil
}

func (store *memoryObjectStore) Delete(_ context.Context, key ObjectKey, _ string) error {
	store.deleteCalls++
	if store.deleteErr != nil {
		return store.deleteErr
	}
	delete(store.objects, key)
	return nil
}

type verifiedStorageRecord struct {
	task     StorageCopyTask
	location VerifiedObjectLocation
}

type copyReleaseRecord struct {
	task    StorageCopyTask
	retryAt time.Time
	code    string
}

type cleanupReleaseRecord struct {
	task    StorageCleanupTask
	retryAt time.Time
	code    string
}

type cutoverRecord struct {
	migrationID    uuid.UUID
	cutoverAt      time.Time
	retentionUntil time.Time
}

type memoryStorageMigrationRepository struct {
	copyTasks       []StorageCopyTask
	cleanupTasks    []StorageCleanupTask
	verified        []verifiedStorageRecord
	copyReleases    []copyReleaseRecord
	deleted         []StorageCleanupTask
	cleanupReleases []cleanupReleaseRecord
	cutover         *cutoverRecord
}

func (repository *memoryStorageMigrationRepository) Plan(_ context.Context, input PlanStorageMigrationInput) (StorageMigrationPlan, error) {
	return StorageMigrationPlan{
		ID: input.ID, Mode: input.Mode, SourceBackendID: input.SourceBackendID,
		DestinationBackendID: input.DestinationBackendID, ObjectCount: int64(len(repository.copyTasks)),
		CreatedAt: input.OccurredAt,
	}, nil
}

func (repository *memoryStorageMigrationRepository) ClaimCopyTasks(_ context.Context, _ uuid.UUID, _ time.Time, _ int32, _ time.Duration) ([]StorageCopyTask, error) {
	return append([]StorageCopyTask(nil), repository.copyTasks...), nil
}

func (repository *memoryStorageMigrationRepository) MarkCopyVerified(_ context.Context, task StorageCopyTask, location VerifiedObjectLocation) error {
	repository.verified = append(repository.verified, verifiedStorageRecord{task: task, location: location})
	return nil
}

func (repository *memoryStorageMigrationRepository) ReleaseCopyTask(_ context.Context, task StorageCopyTask, retryAt time.Time, code string) error {
	repository.copyReleases = append(repository.copyReleases, copyReleaseRecord{task: task, retryAt: retryAt, code: code})
	return nil
}

func (repository *memoryStorageMigrationRepository) Cutover(_ context.Context, migrationID uuid.UUID, cutoverAt, retentionUntil time.Time) error {
	repository.cutover = &cutoverRecord{migrationID: migrationID, cutoverAt: cutoverAt, retentionUntil: retentionUntil}
	return nil
}

func (repository *memoryStorageMigrationRepository) ApproveCleanup(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ time.Time) error {
	return nil
}

func (repository *memoryStorageMigrationRepository) ClaimCleanupTasks(_ context.Context, _ uuid.UUID, _ time.Time, _ int32, _ time.Duration) ([]StorageCleanupTask, error) {
	return append([]StorageCleanupTask(nil), repository.cleanupTasks...), nil
}

func (repository *memoryStorageMigrationRepository) MarkSourceDeleted(_ context.Context, task StorageCleanupTask, _ time.Time) error {
	repository.deleted = append(repository.deleted, task)
	return nil
}

func (repository *memoryStorageMigrationRepository) ReleaseCleanupTask(_ context.Context, task StorageCleanupTask, retryAt time.Time, code string) error {
	repository.cleanupReleases = append(repository.cleanupReleases, cleanupReleaseRecord{task: task, retryAt: retryAt, code: code})
	return nil
}

var _ ObjectStore = (*memoryObjectStore)(nil)
var _ StorageMigrationRepository = (*memoryStorageMigrationRepository)(nil)
