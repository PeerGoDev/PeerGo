package trackercontrol

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type ProjectionRepository interface {
	ClaimNext(context.Context, time.Time, time.Duration) (PendingEvent, bool, error)
	Apply(context.Context, PendingEvent, time.Time) error
	Release(context.Context, PendingEvent, time.Time, string) error
}

type ProjectorConfig struct {
	LeaseDuration time.Duration
	IdleInterval  time.Duration
	RetryBase     time.Duration
}

type Projector struct {
	repository ProjectionRepository
	config     ProjectorConfig
	now        func() time.Time
	logger     *slog.Logger
}

func NewProjector(repository ProjectionRepository, config ProjectorConfig, now func() time.Time, logger *slog.Logger) (*Projector, error) {
	if repository == nil {
		return nil, errors.New("Tracker control projector repository is required")
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = time.Minute
	}
	if config.IdleInterval == 0 {
		config.IdleInterval = time.Second
	}
	if config.RetryBase == 0 {
		config.RetryBase = 2 * time.Second
	}
	if config.LeaseDuration <= 0 || config.LeaseDuration > 5*time.Minute || config.IdleInterval <= 0 ||
		config.IdleInterval > time.Minute || config.RetryBase <= 0 || config.RetryBase > time.Minute {
		return nil, errors.New("Tracker control projector configuration is invalid")
	}
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Projector{repository: repository, config: config, now: now, logger: logger}, nil
}

// RunOnce projects at most one event. Serial global ordering is intentional:
// a later control sequence may not pass a failed earlier event and produce an
// apparently current but incomplete snapshot watermark.
func (projector *Projector) RunOnce(ctx context.Context) (bool, error) {
	now := projector.now().UTC()
	pending, found, err := projector.repository.ClaimNext(ctx, now, projector.config.LeaseDuration)
	if err != nil || !found {
		return found, err
	}
	if err := projector.repository.Apply(ctx, pending, projector.now().UTC()); err != nil {
		retryAt := projector.now().UTC().Add(projector.retryDelay(pending.Attempts))
		if releaseErr := projector.repository.Release(ctx, pending, retryAt, "projection_failed"); releaseErr != nil {
			return true, errors.Join(err, releaseErr)
		}
		projector.logger.WarnContext(ctx, "Tracker control event projection failed", "sequence", pending.Sequence, "attempts", pending.Attempts, "error", err)
		return true, nil
	}
	return true, nil
}

func (projector *Projector) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		processed, err := projector.RunOnce(ctx)
		if err != nil {
			return err
		}
		if processed {
			timer.Reset(0)
		} else {
			timer.Reset(projector.config.IdleInterval)
		}
	}
}

func (projector *Projector) retryDelay(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 6 {
		shift = 6
	}
	delay := projector.config.RetryBase * time.Duration(1<<shift)
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}
