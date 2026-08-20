package objectmigration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	"github.com/peergo/peergo/services/core/internal/platform/objectstore"
)

type memoryRepository struct {
	copyTasks       []CopyTask
	cleanupTasks    []CleanupTask
	verified        map[uuid.UUID]VerifiedLocation
	deleted         map[uuid.UUID]time.Time
	cutoverAt       time.Time
	retentionUntil  time.Time
	cleanupApproved bool
}

func (repository *memoryRepository) Plan(context.Context, PlanInput) (Plan, error) {
	return Plan{}, nil
}

func (repository *memoryRepository) ClaimCopyTasks(_ context.Context, migrationID uuid.UUID, _ time.Time, _ int32, _ time.Duration) ([]CopyTask, error) {
	var result []CopyTask
	for _, task := range repository.copyTasks {
		if task.MigrationID == migrationID {
			result = append(result, task)
		}
	}
	repository.copyTasks = nil
	return result, nil
}

func (repository *memoryRepository) MarkCopyVerified(_ context.Context, task CopyTask, location VerifiedLocation) error {
	if repository.verified == nil {
		repository.verified = make(map[uuid.UUID]VerifiedLocation)
	}
	repository.verified[task.ItemID] = location
	repository.cleanupTasks = append(repository.cleanupTasks, CleanupTask{
		ItemID: task.ItemID, MigrationID: task.MigrationID, Kind: task.Kind,
		ObjectID: task.ObjectID, SourceBackendID: task.SourceBackendID,
		SourceObjectKey: task.SourceObjectKey, SourceVersionID: task.SourceVersionID,
		LeaseToken: uuid.New(), Attempts: 1,
	})
	return nil
}

func (*memoryRepository) ReleaseCopyTask(context.Context, CopyTask, time.Time, string) error {
	return errors.New("unexpected copy failure")
}

func (*memoryRepository) RetryFailures(context.Context, uuid.UUID, time.Time) (int64, error) {
	return 0, nil
}

func (repository *memoryRepository) Cutover(_ context.Context, _ uuid.UUID, cutoverAt, retentionUntil time.Time) error {
	repository.cutoverAt, repository.retentionUntil = cutoverAt, retentionUntil
	return nil
}

func (repository *memoryRepository) ApproveCleanup(context.Context, uuid.UUID, uuid.UUID, time.Time) error {
	repository.cleanupApproved = true
	return nil
}

func (repository *memoryRepository) ClaimCleanupTasks(_ context.Context, migrationID uuid.UUID, _ time.Time, _ int32, _ time.Duration) ([]CleanupTask, error) {
	if !repository.cleanupApproved {
		return nil, nil
	}
	var result []CleanupTask
	for _, task := range repository.cleanupTasks {
		if task.MigrationID == migrationID {
			result = append(result, task)
		}
	}
	repository.cleanupTasks = nil
	return result, nil
}

func (repository *memoryRepository) MarkSourceDeleted(_ context.Context, task CleanupTask, deletedAt time.Time) error {
	if repository.deleted == nil {
		repository.deleted = make(map[uuid.UUID]time.Time)
	}
	repository.deleted[task.ItemID] = deletedAt
	return nil
}

func (*memoryRepository) ReleaseCleanupTask(context.Context, CleanupTask, time.Time, string) error {
	return errors.New("unexpected cleanup failure")
}

func TestFilesystemRoundTripCoversEveryObjectKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	source, err := objectstore.NewFilesystem("local-source", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	destination, err := objectstore.NewFilesystem("local-destination", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := objectstorage.NewRegistry(source, destination)
	if err != nil {
		t.Fatal(err)
	}

	migrationID := uuid.New()
	now := time.Date(2026, 8, 18, 1, 0, 0, 0, time.UTC)
	kinds := []Kind{KindTorrent, KindTorrentScreenshot, KindAvatar, KindImageDerivative}
	keys := []objectstorage.Key{
		"torrents/sha256/01/fixture.torrent",
		"torrent-screenshots/sha256/02/fixture.jpg",
		"avatars/sha256/03/fixture.jpg",
		"image-derivatives/webp-v1/sha256/04/fixture.webp",
	}
	forward := &memoryRepository{}
	descriptors := make([]objectstorage.Descriptor, len(kinds))
	for index, kind := range kinds {
		contents := []byte("peergo-round-trip-" + string(kind))
		digest := objectstorage.SHA256(sha256.Sum256(contents))
		descriptor := objectstorage.Descriptor{SHA256: digest, ByteLength: int64(len(contents))}
		descriptors[index] = descriptor
		if _, err := source.PutIfAbsent(ctx, keys[index], bytes.NewReader(contents), descriptor); err != nil {
			t.Fatal(err)
		}
		forward.copyTasks = append(forward.copyTasks, CopyTask{
			ItemID: uuid.New(), MigrationID: migrationID, Kind: kind, ObjectID: uuid.New(),
			Descriptor: descriptor, SourceBackendID: source.BackendID(), SourceObjectKey: keys[index],
			DestinationBackendID: destination.BackendID(), DestinationObjectKey: keys[index],
			LeaseToken: uuid.New(), Attempts: 1,
		})
	}
	service, err := NewService(forward, registry, ServiceConfig{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	processed, err := service.RunCopyBatch(ctx, migrationID)
	if err != nil || processed != len(kinds) {
		t.Fatalf("forward copy processed=%d err=%v", processed, err)
	}
	for index, key := range keys {
		assertStoredDescriptor(t, ctx, destination, key, descriptors[index])
	}
	if err := service.Cutover(ctx, migrationID, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if !forward.retentionUntil.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("retention=%s", forward.retentionUntil)
	}
	if err := service.ApproveCleanup(ctx, migrationID, uuid.New()); err != nil {
		t.Fatal(err)
	}
	processed, err = service.RunCleanupBatch(ctx, migrationID)
	if err != nil || processed != len(kinds) {
		t.Fatalf("cleanup processed=%d err=%v", processed, err)
	}
	for _, key := range keys {
		if _, err := source.Open(ctx, key, ""); !errors.Is(err, objectstorage.ErrNotFound) {
			t.Fatalf("source %q still readable: %v", key, err)
		}
	}

	reverseID := uuid.New()
	reverse := &memoryRepository{}
	for index, kind := range kinds {
		reverse.copyTasks = append(reverse.copyTasks, CopyTask{
			ItemID: uuid.New(), MigrationID: reverseID, Kind: kind, ObjectID: uuid.New(),
			Descriptor: descriptors[index], SourceBackendID: destination.BackendID(), SourceObjectKey: keys[index],
			DestinationBackendID: source.BackendID(), DestinationObjectKey: keys[index],
			LeaseToken: uuid.New(), Attempts: 1,
		})
	}
	reverseService, err := NewService(reverse, registry, ServiceConfig{Now: func() time.Time { return now.Add(25 * time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	processed, err = reverseService.RunCopyBatch(ctx, reverseID)
	if err != nil || processed != len(kinds) {
		t.Fatalf("reverse copy processed=%d err=%v", processed, err)
	}
	for index, key := range keys {
		assertStoredDescriptor(t, ctx, source, key, descriptors[index])
	}
}

func assertStoredDescriptor(t *testing.T, ctx context.Context, store objectstorage.Store, key objectstorage.Key, expected objectstorage.Descriptor) {
	t.Helper()
	opened, err := store.Open(ctx, key, "")
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Body.Close()
	if _, err := objectstorage.Verify(opened, expected); err != nil {
		t.Fatalf("verify %q: %v", key, err)
	}
}

var _ Repository = (*memoryRepository)(nil)
