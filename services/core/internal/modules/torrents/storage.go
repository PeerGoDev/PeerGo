package torrents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
)

const (
	defaultStorageBatchSize     = 20
	defaultStorageLeaseDuration = 2 * time.Minute
	minimumSourceRetention      = 24 * time.Hour
	maximumSourceRetention      = 365 * 24 * time.Hour
)

var (
	ErrStorageInputInvalid       = objectstorage.ErrInputInvalid
	ErrStorageStateConflict      = errors.New("torrent storage state conflict")
	ErrStoredObjectNotFound      = objectstorage.ErrNotFound
	ErrStoredObjectConflict      = objectstorage.ErrObjectConflict
	ErrReadableObjectUnavailable = errors.New("no configured readable object location is available")
	ErrReadableObjectConflict    = errors.New("readable object conflicts with immutable identity")
)

// StorageBackendID is a stable deployment-level name. Credentials, endpoints,
// and host paths stay in runtime configuration; database rows retain only this
// opaque ID so a secret rotation cannot rewrite object history.
type StorageBackendID = objectstorage.BackendID

func ParseStorageBackendID(value string) (StorageBackendID, error) {
	return objectstorage.ParseBackendID(value)
}

// ObjectKey is a provider-neutral relative key. It is deliberately not a host
// path or URL, which lets one immutable object have verified locations on a
// filesystem, MinIO, S3, or another S3-compatible backend.
type ObjectKey = objectstorage.Key

func ParseObjectKey(value string) (ObjectKey, error) {
	return objectstorage.ParseKey(value)
}

// TorrentObjectKey gives the original bytes a deterministic content-addressed
// destination. Retrying a copy therefore targets the same key without relying
// on an editable title, legacy path, or database sequence.
func TorrentObjectKey(digest ObjectSHA256) ObjectKey {
	hexDigest := digest.Hex()
	return ObjectKey("torrents/sha256/" + hexDigest[:2] + "/" + hexDigest + ".torrent")
}

// TorrentScreenshotObjectKey keeps screenshots immutable and independent from
// user-provided filenames. The media type has already been verified from the
// decoded bytes before this key is constructed.
func TorrentScreenshotObjectKey(digest ObjectSHA256, extension string) ObjectKey {
	hexDigest := digest.Hex()
	return ObjectKey("torrent-screenshots/sha256/" + hexDigest[:2] + "/" + hexDigest + extension)
}

type StoredObjectDescriptor = objectstorage.Descriptor
type ObjectWriteResult = objectstorage.WriteResult
type StoredObjectReader = objectstorage.Reader

// ObjectStore is the narrow port required by torrent ingestion and migration.
// PutIfAbsent must never overwrite an existing key and must reject a stream
// whose length or SHA-256 differs from expected. Delete is used only by the
// explicit post-retention cleanup path, never by ordinary torrent handlers.
type ObjectStore = objectstorage.Store

type StoreRegistry = objectstorage.Registry

// ReadableObjectLocation is the shared internal projection used by every
// immutable-object response. Keeping the physical location behind this type
// lets metainfo and screenshots use the same verified local/S3 fallback
// without returning backend identifiers or object keys to Web clients.
type ReadableObjectLocation struct {
	ID         uuid.UUID
	BackendID  StorageBackendID
	ObjectKey  ObjectKey
	Preferred  bool
	VersionID  string
	Descriptor StoredObjectDescriptor
	VerifiedAt time.Time
}

func NewStoreRegistry(stores ...ObjectStore) (*StoreRegistry, error) {
	return objectstorage.NewRegistry(stores...)
}

type StorageLocationState string

const (
	StorageLocationPending  StorageLocationState = "pending"
	StorageLocationVerified StorageLocationState = "verified"
	StorageLocationFailed   StorageLocationState = "failed"
	StorageLocationRetiring StorageLocationState = "retiring"
	StorageLocationDeleted  StorageLocationState = "deleted"
)

type ObjectLocation struct {
	ID         uuid.UUID
	ObjectID   uuid.UUID
	BackendID  StorageBackendID
	ObjectKey  ObjectKey
	State      StorageLocationState
	Preferred  bool
	VersionID  string
	ByteLength int64
	SHA256     ObjectSHA256
	VerifiedAt *time.Time
	RetiringAt *time.Time
	DeletedAt  *time.Time
	Version    int64
}

type StorageMigrationMode string

const (
	StorageMigrationReplicate StorageMigrationMode = "replicate"
	StorageMigrationMove      StorageMigrationMode = "move"
)

type PlanStorageMigrationInput struct {
	ID                   uuid.UUID
	Mode                 StorageMigrationMode
	SourceBackendID      StorageBackendID
	DestinationBackendID StorageBackendID
	RequestedBy          uuid.UUID
	OccurredAt           time.Time
}

type StorageMigrationPlan struct {
	ID                   uuid.UUID
	Mode                 StorageMigrationMode
	SourceBackendID      StorageBackendID
	DestinationBackendID StorageBackendID
	ObjectCount          int64
	CreatedAt            time.Time
}

// StorageCopyTask is a leased snapshot row. LeaseToken prevents a stalled
// worker from committing after another process has safely retried the item.
type StorageCopyTask struct {
	MigrationID          uuid.UUID
	ObjectID             uuid.UUID
	Descriptor           StoredObjectDescriptor
	SourceBackendID      StorageBackendID
	SourceObjectKey      ObjectKey
	SourceVersionID      string
	DestinationBackendID StorageBackendID
	LeaseToken           uuid.UUID
	Attempts             int32
}

type StorageCleanupTask struct {
	MigrationID     uuid.UUID
	ObjectID        uuid.UUID
	SourceBackendID StorageBackendID
	SourceObjectKey ObjectKey
	SourceVersionID string
	LeaseToken      uuid.UUID
	Attempts        int32
}

type VerifiedObjectLocation struct {
	BackendID  StorageBackendID
	ObjectKey  ObjectKey
	VersionID  string
	Descriptor StoredObjectDescriptor
	VerifiedAt time.Time
}

// StorageMigrationRepository owns snapshotting, leasing, location state, and
// the atomic read-preference cutover. Object bytes cannot participate in the
// PostgreSQL transaction, so every repository operation is deliberately
// idempotent and guarded by the per-attempt lease token.
type StorageMigrationRepository interface {
	Plan(context.Context, PlanStorageMigrationInput) (StorageMigrationPlan, error)
	ClaimCopyTasks(context.Context, uuid.UUID, time.Time, int32, time.Duration) ([]StorageCopyTask, error)
	MarkCopyVerified(context.Context, StorageCopyTask, VerifiedObjectLocation) error
	ReleaseCopyTask(context.Context, StorageCopyTask, time.Time, string) error
	Cutover(context.Context, uuid.UUID, time.Time, time.Time) error
	ApproveCleanup(context.Context, uuid.UUID, uuid.UUID, time.Time) error
	ClaimCleanupTasks(context.Context, uuid.UUID, time.Time, int32, time.Duration) ([]StorageCleanupTask, error)
	MarkSourceDeleted(context.Context, StorageCleanupTask, time.Time) error
	ReleaseCleanupTask(context.Context, StorageCleanupTask, time.Time, string) error
}

func (service *StorageMigrationService) Plan(ctx context.Context, input PlanStorageMigrationInput) (StorageMigrationPlan, error) {
	if input.ID == uuid.Nil || input.RequestedBy == uuid.Nil || input.OccurredAt.IsZero() ||
		(input.Mode != StorageMigrationReplicate && input.Mode != StorageMigrationMove) ||
		input.SourceBackendID == "" || input.DestinationBackendID == "" || input.SourceBackendID == input.DestinationBackendID {
		return StorageMigrationPlan{}, ErrStorageInputInvalid
	}
	if _, exists := service.stores.Get(input.SourceBackendID); !exists {
		return StorageMigrationPlan{}, ErrStorageInputInvalid
	}
	if _, exists := service.stores.Get(input.DestinationBackendID); !exists {
		return StorageMigrationPlan{}, ErrStorageInputInvalid
	}
	input.OccurredAt = input.OccurredAt.UTC()
	plan, err := service.repository.Plan(ctx, input)
	if err != nil {
		return StorageMigrationPlan{}, fmt.Errorf("plan torrent storage migration: %w", err)
	}
	return plan, nil
}

type StorageMigrationServiceConfig struct {
	BatchSize     int32
	LeaseDuration time.Duration
	Now           func() time.Time
}

// StorageMigrationService copies first, performs a complete destination
// read-back, and records verification before any read preference can change.
// Source deletion is a separate method and is repository-gated by cutover plus
// retention, so a copy worker can never delete the only durable object.
type StorageMigrationService struct {
	repository    StorageMigrationRepository
	stores        *StoreRegistry
	batchSize     int32
	leaseDuration time.Duration
	now           func() time.Time
}

func NewStorageMigrationService(repository StorageMigrationRepository, stores *StoreRegistry, config StorageMigrationServiceConfig) (*StorageMigrationService, error) {
	if repository == nil || stores == nil {
		return nil, errors.New("storage migration repository and store registry are required")
	}
	if config.BatchSize == 0 {
		config.BatchSize = defaultStorageBatchSize
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = defaultStorageLeaseDuration
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.BatchSize < 1 || config.BatchSize > 100 || config.LeaseDuration <= 0 || config.LeaseDuration > 10*time.Minute {
		return nil, errors.New("storage migration service configuration is invalid")
	}
	return &StorageMigrationService{
		repository: repository, stores: stores, batchSize: config.BatchSize,
		leaseDuration: config.LeaseDuration, now: config.Now,
	}, nil
}

func (service *StorageMigrationService) RunCopyBatch(ctx context.Context, migrationID uuid.UUID) (int, error) {
	if migrationID == uuid.Nil {
		return 0, ErrStorageInputInvalid
	}
	tasks, err := service.repository.ClaimCopyTasks(ctx, migrationID, service.now().UTC(), service.batchSize, service.leaseDuration)
	if err != nil {
		return 0, fmt.Errorf("claim torrent object copy tasks: %w", err)
	}

	var failures []error
	for _, task := range tasks {
		if err := service.copyAndVerify(ctx, task); err != nil {
			failures = append(failures, err)
		}
	}
	return len(tasks), errors.Join(failures...)
}

func (service *StorageMigrationService) copyAndVerify(ctx context.Context, task StorageCopyTask) error {
	if task.MigrationID == uuid.Nil || task.ObjectID == uuid.Nil || task.LeaseToken == uuid.Nil || !task.Descriptor.Valid() {
		return service.releaseCopyFailure(ctx, task, "invalid_copy_task", ErrStorageInputInvalid)
	}
	source, sourceExists := service.stores.Get(task.SourceBackendID)
	destination, destinationExists := service.stores.Get(task.DestinationBackendID)
	if !sourceExists || !destinationExists || source.BackendID() == destination.BackendID() {
		return service.releaseCopyFailure(ctx, task, "storage_backend_unavailable", ErrStorageInputInvalid)
	}

	sourceObject, err := source.Open(ctx, task.SourceObjectKey, task.SourceVersionID)
	if err != nil {
		return service.releaseCopyFailure(ctx, task, "source_open_failed", err)
	}
	if sourceObject.Body == nil {
		return service.releaseCopyFailure(ctx, task, "source_open_failed", ErrStoredObjectConflict)
	}
	defer sourceObject.Body.Close()
	if sourceObject.ByteLength != task.Descriptor.ByteLength {
		return service.releaseCopyFailure(ctx, task, "source_length_mismatch", ErrStoredObjectConflict)
	}

	destinationKey := TorrentObjectKey(task.Descriptor.SHA256)
	writeResult, err := destination.PutIfAbsent(ctx, destinationKey, sourceObject.Body, task.Descriptor)
	if err != nil {
		return service.releaseCopyFailure(ctx, task, "destination_write_failed", err)
	}

	destinationObject, err := destination.Open(ctx, destinationKey, writeResult.VersionID)
	if err != nil {
		return service.releaseCopyFailure(ctx, task, "destination_readback_failed", err)
	}
	if destinationObject.Body == nil {
		return service.releaseCopyFailure(ctx, task, "destination_readback_failed", ErrStoredObjectConflict)
	}
	verifiedDescriptor, verificationErr := VerifyStoredObject(destinationObject, task.Descriptor)
	closeErr := destinationObject.Body.Close()
	if verificationErr != nil {
		return service.releaseCopyFailure(ctx, task, "destination_verification_failed", verificationErr)
	}
	if closeErr != nil {
		return service.releaseCopyFailure(ctx, task, "destination_readback_close_failed", closeErr)
	}
	versionID := destinationObject.VersionID
	if versionID == "" {
		versionID = writeResult.VersionID
	}
	verifiedAt := service.now().UTC()
	if err := service.repository.MarkCopyVerified(ctx, task, VerifiedObjectLocation{
		BackendID: task.DestinationBackendID, ObjectKey: destinationKey,
		VersionID: versionID, Descriptor: verifiedDescriptor, VerifiedAt: verifiedAt,
	}); err != nil {
		return fmt.Errorf("record verified torrent object %s: %w", task.ObjectID, err)
	}
	return nil
}

// VerifyStoredObject performs the shared full-object read-back used after both
// a new upload and a storage migration. Provider metadata and ETags are not a
// substitute for verifying the exact immutable SHA-256 and byte length.
func VerifyStoredObject(object StoredObjectReader, expected StoredObjectDescriptor) (StoredObjectDescriptor, error) {
	return objectstorage.Verify(object, expected)
}

func (service *StorageMigrationService) releaseCopyFailure(ctx context.Context, task StorageCopyTask, code string, cause error) error {
	retryAt := service.now().UTC().Add(storageRetryDelay(task.Attempts))
	if releaseErr := service.repository.ReleaseCopyTask(ctx, task, retryAt, code); releaseErr != nil {
		return fmt.Errorf("copy torrent object %s: %w; release task: %v", task.ObjectID, cause, releaseErr)
	}
	return fmt.Errorf("copy torrent object %s: %w", task.ObjectID, cause)
}

func (service *StorageMigrationService) Cutover(ctx context.Context, migrationID uuid.UUID, retention time.Duration) error {
	if migrationID == uuid.Nil || retention < minimumSourceRetention || retention > maximumSourceRetention {
		return ErrStorageInputInvalid
	}
	now := service.now().UTC()
	if err := service.repository.Cutover(ctx, migrationID, now, now.Add(retention)); err != nil {
		return fmt.Errorf("cut over torrent object storage: %w", err)
	}
	return nil
}

// ApproveCleanup is deliberately distinct from Cutover. It is expected to be
// called by a freshly authorized staff command only after the retention window;
// an unattended copy worker therefore cannot turn verification into deletion.
func (service *StorageMigrationService) ApproveCleanup(ctx context.Context, migrationID, approvedBy uuid.UUID) error {
	if migrationID == uuid.Nil || approvedBy == uuid.Nil {
		return ErrStorageInputInvalid
	}
	if err := service.repository.ApproveCleanup(ctx, migrationID, approvedBy, service.now().UTC()); err != nil {
		return fmt.Errorf("approve torrent source cleanup: %w", err)
	}
	return nil
}

func (service *StorageMigrationService) RunCleanupBatch(ctx context.Context, migrationID uuid.UUID) (int, error) {
	if migrationID == uuid.Nil {
		return 0, ErrStorageInputInvalid
	}
	tasks, err := service.repository.ClaimCleanupTasks(ctx, migrationID, service.now().UTC(), service.batchSize, service.leaseDuration)
	if err != nil {
		return 0, fmt.Errorf("claim torrent object cleanup tasks: %w", err)
	}

	var failures []error
	for _, task := range tasks {
		source, exists := service.stores.Get(task.SourceBackendID)
		if !exists {
			failures = append(failures, service.releaseCleanupFailure(ctx, task, "storage_backend_unavailable", ErrStorageInputInvalid))
			continue
		}
		if err := source.Delete(ctx, task.SourceObjectKey, task.SourceVersionID); err != nil {
			failures = append(failures, service.releaseCleanupFailure(ctx, task, "source_delete_failed", err))
			continue
		}
		if err := service.repository.MarkSourceDeleted(ctx, task, service.now().UTC()); err != nil {
			failures = append(failures, fmt.Errorf("record deleted torrent source %s: %w", task.ObjectID, err))
		}
	}
	return len(tasks), errors.Join(failures...)
}

func (service *StorageMigrationService) releaseCleanupFailure(ctx context.Context, task StorageCleanupTask, code string, cause error) error {
	retryAt := service.now().UTC().Add(storageRetryDelay(task.Attempts))
	if releaseErr := service.repository.ReleaseCleanupTask(ctx, task, retryAt, code); releaseErr != nil {
		return fmt.Errorf("clean torrent source %s: %w; release task: %v", task.ObjectID, cause, releaseErr)
	}
	return fmt.Errorf("clean torrent source %s: %w", task.ObjectID, cause)
}

func storageRetryDelay(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 8 {
		shift = 8
	}
	return time.Second * time.Duration(1<<shift)
}
