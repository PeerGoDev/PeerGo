package objectmigration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
)

const defaultLeaseDuration = 2 * time.Minute

type ServiceConfig struct {
	BatchSize     int32
	LeaseDuration time.Duration
	Now           func() time.Time
}

type Service struct {
	repository    Repository
	stores        *objectstorage.Registry
	batchSize     int32
	leaseDuration time.Duration
	now           func() time.Time
}

func NewService(repository Repository, stores *objectstorage.Registry, config ServiceConfig) (*Service, error) {
	if repository == nil || stores == nil {
		return nil, errors.New("object migration repository and store registry are required")
	}
	if config.BatchSize == 0 {
		config.BatchSize = DefaultBatchSize
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = defaultLeaseDuration
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.BatchSize < 1 || config.BatchSize > 100 || config.LeaseDuration <= 0 || config.LeaseDuration > 10*time.Minute {
		return nil, errors.New("object migration service configuration is invalid")
	}
	return &Service{
		repository: repository, stores: stores, batchSize: config.BatchSize,
		leaseDuration: config.LeaseDuration, now: config.Now,
	}, nil
}

func (service *Service) Plan(ctx context.Context, input PlanInput) (Plan, error) {
	input.Kinds = normalizeKinds(input.Kinds)
	if input.ID == uuid.Nil || input.RequestedBy == uuid.Nil || input.OccurredAt.IsZero() ||
		(input.Mode != ModeMove && input.Mode != ModeReplicate) || len(input.Kinds) == 0 ||
		input.SourceBackendID == "" || input.DestinationBackendID == "" || input.SourceBackendID == input.DestinationBackendID {
		return Plan{}, ErrInput
	}
	if _, ok := service.stores.Get(input.SourceBackendID); !ok {
		return Plan{}, ErrInput
	}
	if _, ok := service.stores.Get(input.DestinationBackendID); !ok {
		return Plan{}, ErrInput
	}
	input.OccurredAt = input.OccurredAt.UTC()
	plan, err := service.repository.Plan(ctx, input)
	if err != nil {
		return Plan{}, fmt.Errorf("plan unified object storage migration: %w", err)
	}
	return plan, nil
}

func normalizeKinds(kinds []Kind) []Kind {
	if len(kinds) == 0 {
		kinds = AllKinds
	}
	seen := make(map[Kind]struct{}, len(kinds))
	result := make([]Kind, 0, len(kinds))
	for _, allowed := range AllKinds {
		for _, kind := range kinds {
			if kind == allowed {
				if _, exists := seen[kind]; !exists {
					seen[kind] = struct{}{}
					result = append(result, kind)
				}
			}
		}
	}
	return result
}

func (service *Service) RunCopyBatch(ctx context.Context, migrationID uuid.UUID) (int, error) {
	if migrationID == uuid.Nil {
		return 0, ErrInput
	}
	tasks, err := service.repository.ClaimCopyTasks(ctx, migrationID, service.now().UTC(), service.batchSize, service.leaseDuration)
	if err != nil {
		return 0, fmt.Errorf("claim object copy tasks: %w", err)
	}
	var failures []error
	for _, task := range tasks {
		if err := service.copyAndVerify(ctx, task); err != nil {
			failures = append(failures, err)
		}
	}
	return len(tasks), errors.Join(failures...)
}

func (service *Service) copyAndVerify(ctx context.Context, task CopyTask) error {
	if task.ItemID == uuid.Nil || task.MigrationID == uuid.Nil || task.ObjectID == uuid.Nil ||
		task.LeaseToken == uuid.Nil || !task.Kind.Valid() || !task.Descriptor.Valid() ||
		task.DestinationObjectKey == "" {
		return service.releaseCopyFailure(ctx, task, "invalid_copy_task", ErrInput)
	}
	source, sourceOK := service.stores.Get(task.SourceBackendID)
	destination, destinationOK := service.stores.Get(task.DestinationBackendID)
	if !sourceOK || !destinationOK || source.BackendID() == destination.BackendID() {
		return service.releaseCopyFailure(ctx, task, "storage_backend_unavailable", ErrInput)
	}

	opened, err := source.Open(ctx, task.SourceObjectKey, task.SourceVersionID)
	if err != nil {
		return service.releaseCopyFailure(ctx, task, "source_open_failed", err)
	}
	if opened.Body == nil || opened.ByteLength != task.Descriptor.ByteLength {
		if opened.Body != nil {
			_ = opened.Body.Close()
		}
		return service.releaseCopyFailure(ctx, task, "source_length_mismatch", objectstorage.ErrObjectConflict)
	}
	write, err := destination.PutIfAbsent(ctx, task.DestinationObjectKey, opened.Body, task.Descriptor)
	closeErr := opened.Body.Close()
	if err != nil {
		return service.releaseCopyFailure(ctx, task, "destination_write_failed", err)
	}
	if closeErr != nil {
		return service.releaseCopyFailure(ctx, task, "source_close_failed", closeErr)
	}

	readback, err := destination.Open(ctx, task.DestinationObjectKey, write.VersionID)
	if err != nil {
		return service.releaseCopyFailure(ctx, task, "destination_readback_failed", err)
	}
	verified, verifyErr := objectstorage.Verify(readback, task.Descriptor)
	closeErr = readback.Body.Close()
	if verifyErr != nil {
		return service.releaseCopyFailure(ctx, task, "destination_verification_failed", verifyErr)
	}
	if closeErr != nil {
		return service.releaseCopyFailure(ctx, task, "destination_readback_close_failed", closeErr)
	}
	versionID := readback.VersionID
	if versionID == "" {
		versionID = write.VersionID
	}
	if err := service.repository.MarkCopyVerified(ctx, task, VerifiedLocation{
		BackendID: task.DestinationBackendID, ObjectKey: task.DestinationObjectKey,
		VersionID: versionID, Descriptor: verified, VerifiedAt: service.now().UTC(),
	}); err != nil {
		return fmt.Errorf("record verified %s object %s: %w", task.Kind, task.ObjectID, err)
	}
	return nil
}

func (service *Service) releaseCopyFailure(ctx context.Context, task CopyTask, code string, cause error) error {
	retryAt := service.now().UTC().Add(retryDelay(task.Attempts))
	if err := service.repository.ReleaseCopyTask(ctx, task, retryAt, code); err != nil {
		return fmt.Errorf("copy %s object %s: %w; release task: %v", task.Kind, task.ObjectID, cause, err)
	}
	return fmt.Errorf("copy %s object %s: %w", task.Kind, task.ObjectID, cause)
}

func (service *Service) RetryFailures(ctx context.Context, migrationID uuid.UUID) (int64, error) {
	if migrationID == uuid.Nil {
		return 0, ErrInput
	}
	count, err := service.repository.RetryFailures(ctx, migrationID, service.now().UTC())
	if err != nil {
		return 0, fmt.Errorf("retry failed storage items: %w", err)
	}
	return count, nil
}

func (service *Service) Cutover(ctx context.Context, migrationID uuid.UUID, retention time.Duration) error {
	if migrationID == uuid.Nil || retention < MinimumRetention || retention > MaximumRetention {
		return ErrInput
	}
	now := service.now().UTC()
	if err := service.repository.Cutover(ctx, migrationID, now, now.Add(retention)); err != nil {
		return fmt.Errorf("cut over unified object storage: %w", err)
	}
	return nil
}

func (service *Service) ApproveCleanup(ctx context.Context, migrationID, approvedBy uuid.UUID) error {
	if migrationID == uuid.Nil || approvedBy == uuid.Nil {
		return ErrInput
	}
	if err := service.repository.ApproveCleanup(ctx, migrationID, approvedBy, service.now().UTC()); err != nil {
		return fmt.Errorf("approve source cleanup: %w", err)
	}
	return nil
}

func (service *Service) RunCleanupBatch(ctx context.Context, migrationID uuid.UUID) (int, error) {
	if migrationID == uuid.Nil {
		return 0, ErrInput
	}
	tasks, err := service.repository.ClaimCleanupTasks(ctx, migrationID, service.now().UTC(), service.batchSize, service.leaseDuration)
	if err != nil {
		return 0, fmt.Errorf("claim object cleanup tasks: %w", err)
	}
	var failures []error
	for _, task := range tasks {
		source, ok := service.stores.Get(task.SourceBackendID)
		if !ok {
			failures = append(failures, service.releaseCleanupFailure(ctx, task, "storage_backend_unavailable", ErrInput))
			continue
		}
		if err := source.Delete(ctx, task.SourceObjectKey, task.SourceVersionID); err != nil {
			failures = append(failures, service.releaseCleanupFailure(ctx, task, "source_delete_failed", err))
			continue
		}
		if err := service.repository.MarkSourceDeleted(ctx, task, service.now().UTC()); err != nil {
			failures = append(failures, fmt.Errorf("record deleted %s source %s: %w", task.Kind, task.ObjectID, err))
		}
	}
	return len(tasks), errors.Join(failures...)
}

func (service *Service) releaseCleanupFailure(ctx context.Context, task CleanupTask, code string, cause error) error {
	retryAt := service.now().UTC().Add(retryDelay(task.Attempts))
	if err := service.repository.ReleaseCleanupTask(ctx, task, retryAt, code); err != nil {
		return fmt.Errorf("clean %s source %s: %w; release task: %v", task.Kind, task.ObjectID, cause, err)
	}
	return fmt.Errorf("clean %s source %s: %w", task.Kind, task.ObjectID, cause)
}

func retryDelay(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 8 {
		shift = 8
	}
	return time.Second * time.Duration(1<<shift)
}
