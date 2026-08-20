package imaging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
)

type ProcessorConfig struct {
	ActiveBackendID objectstorage.BackendID
	LeaseDuration   time.Duration
	Now             func() time.Time
	NewUUID         func() uuid.UUID
}

type Processor struct {
	repository      Repository
	stores          *objectstorage.Registry
	transformer     Transformer
	activeBackendID objectstorage.BackendID
	leaseDuration   time.Duration
	now             func() time.Time
	newUUID         func() uuid.UUID
}

func NewProcessor(repository Repository, stores *objectstorage.Registry, transformer Transformer, config ProcessorConfig) (*Processor, error) {
	if repository == nil || stores == nil || transformer == nil || config.ActiveBackendID == "" {
		return nil, errors.New("image derivative processor dependencies are required")
	}
	if _, ok := stores.Get(config.ActiveBackendID); !ok {
		return nil, errors.New("active image derivative storage backend is unavailable")
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = 5 * time.Minute
	}
	if config.LeaseDuration < time.Second || config.LeaseDuration > 10*time.Minute {
		return nil, errors.New("image derivative lease duration is invalid")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewUUID == nil {
		config.NewUUID = uuid.New
	}
	return &Processor{
		repository: repository, stores: stores, transformer: transformer,
		activeBackendID: config.ActiveBackendID, leaseDuration: config.LeaseDuration,
		now: config.Now, newUUID: config.NewUUID,
	}, nil
}

// ProcessNext claims at most one job. Failures are reduced to stable error
// codes before being persisted so database rows never retain object paths,
// native command lines or attacker-controlled decoder diagnostics.
func (processor *Processor) ProcessNext(ctx context.Context) (bool, error) {
	now := processor.now().UTC().Round(0)
	job, err := processor.repository.Claim(ctx, now, processor.leaseDuration, processor.newUUID())
	if err != nil || job == nil {
		return job != nil, err
	}
	if err := processor.process(ctx, *job, now); err != nil {
		code := processingErrorCode(err)
		if failErr := processor.repository.Fail(ctx, *job, code, processor.now().UTC().Round(0)); failErr != nil {
			return true, fmt.Errorf("image derivative failed (%s) and state update failed: %w", code, failErr)
		}
		return true, nil
	}
	return true, nil
}

func (processor *Processor) process(ctx context.Context, job Job, now time.Time) error {
	source, err := processor.repository.Source(ctx, job)
	if err != nil {
		return err
	}
	raw, err := processor.readSource(ctx, source)
	if err != nil {
		return err
	}
	output, err := processor.transformer.Transform(ctx, source.Kind, job.Variant, raw, source.Extension)
	if err != nil {
		return err
	}
	key, err := objectstorage.ParseKey(
		"image-derivatives/" + PolicyVersion + "/sha256/" + output.Descriptor.SHA256.Hex()[:2] + "/" + output.Descriptor.SHA256.Hex() + ".webp",
	)
	if err != nil {
		return ErrOutputConflict
	}
	store, _ := processor.stores.Get(processor.activeBackendID)
	write, err := store.PutIfAbsent(ctx, key, bytes.NewReader(output.Bytes), output.Descriptor)
	if err != nil {
		return fmt.Errorf("store image derivative: %w", err)
	}
	opened, err := store.Open(ctx, key, write.VersionID)
	if err != nil {
		return fmt.Errorf("open image derivative read-back: %w", err)
	}
	verified, verifyErr := objectstorage.ReadAllVerified(opened, output.Descriptor)
	closeErr := opened.Body.Close()
	if verifyErr != nil || closeErr != nil || !bytes.Equal(verified, output.Bytes) {
		if write.Created {
			_ = store.Delete(ctx, key, write.VersionID)
		}
		return ErrOutputConflict
	}
	versionID := opened.VersionID
	if versionID == "" {
		versionID = write.VersionID
	}
	return processor.repository.Complete(ctx, job, output, processor.activeBackendID, key, versionID, now)
}

func (processor *Processor) readSource(ctx context.Context, source Source) ([]byte, error) {
	configuredLocation := false
	for _, location := range source.Locations {
		store, ok := processor.stores.Get(location.BackendID)
		if !ok {
			continue
		}
		configuredLocation = true
		opened, err := store.Open(ctx, location.ObjectKey, location.VersionID)
		if err != nil {
			continue
		}
		contents, verifyErr := objectstorage.ReadAllVerified(opened, source.Descriptor)
		closeErr := opened.Body.Close()
		if verifyErr == nil && closeErr == nil {
			return contents, nil
		}
	}
	if configuredLocation {
		return nil, ErrSourceConflict
	}
	return nil, ErrSourceUnavailable
}

func processingErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrNotFound):
		return "source_not_found"
	case errors.Is(err, ErrSourceUnavailable):
		return "source_backend_unavailable"
	case errors.Is(err, ErrSourceConflict):
		return "source_integrity_conflict"
	case errors.Is(err, ErrOutputConflict):
		return "output_integrity_conflict"
	default:
		return "transform_failed"
	}
}

func ReadReady(ctx context.Context, stores *objectstorage.Registry, derivative ReadyDerivative) ([]byte, error) {
	if ctx == nil || stores == nil || !derivative.Descriptor.Valid() || len(derivative.Locations) == 0 {
		return nil, ErrInput
	}
	configured := false
	for _, location := range derivative.Locations {
		store, ok := stores.Get(location.BackendID)
		if !ok {
			continue
		}
		configured = true
		opened, err := store.Open(ctx, location.ObjectKey, location.VersionID)
		if err != nil {
			continue
		}
		contents, verifyErr := objectstorage.ReadAllVerified(opened, derivative.Descriptor)
		closeErr := opened.Body.Close()
		if verifyErr == nil && closeErr == nil {
			return contents, nil
		}
	}
	if configured {
		return nil, ErrOutputConflict
	}
	return nil, ErrSourceUnavailable
}
