package seedingreward

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	defaultSettlementBatchSize       int32 = 20
	defaultSettlementLeaseDuration         = 30 * time.Second
	defaultSettlementPollInterval          = time.Second
	defaultSettlementMaximumAttempts       = int32(10)
)

type WorkerConfig struct {
	BatchSize       int32
	LeaseDuration   time.Duration
	PollInterval    time.Duration
	MaximumAttempts int32
	Now             func() time.Time
}

type Worker struct {
	repository SettlementRepository
	config     WorkerConfig
	logger     *slog.Logger
}

func NewWorker(repository SettlementRepository, config WorkerConfig, logger *slog.Logger) (*Worker, error) {
	if repository == nil {
		return nil, ErrInput
	}
	if config.BatchSize == 0 {
		config.BatchSize = defaultSettlementBatchSize
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = defaultSettlementLeaseDuration
	}
	if config.PollInterval == 0 {
		config.PollInterval = defaultSettlementPollInterval
	}
	if config.MaximumAttempts == 0 {
		config.MaximumAttempts = defaultSettlementMaximumAttempts
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.BatchSize < 1 || config.BatchSize > 100 || config.LeaseDuration <= 0 ||
		config.LeaseDuration > 5*time.Minute || config.PollInterval <= 0 ||
		config.MaximumAttempts < 1 || config.MaximumAttempts > 1000 {
		return nil, ErrInput
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{repository: repository, config: config, logger: logger}, nil
}

func (worker *Worker) RunOnce(ctx context.Context) (int, error) {
	now := worker.config.Now().UTC()
	pending, err := worker.repository.Claim(ctx, now, worker.config.BatchSize, worker.config.LeaseDuration)
	if err != nil {
		return 0, err
	}
	var failures []error
	for _, item := range pending {
		result, settleErr := worker.repository.Settle(ctx, item, worker.config.Now().UTC())
		if settleErr == nil {
			worker.logger.InfoContext(ctx, "seeding reward settled",
				"window_start", result.WindowStart, "user_id", result.UserID,
				"policy_revision", result.PolicyRevision, "reward", result.Reward,
				"experience", result.ExperienceAmount)
			continue
		}
		errorCode, indefinitelyRetryable := rewardErrorCode(settleErr)
		terminal := !indefinitelyRetryable && item.Attempts >= worker.config.MaximumAttempts
		next := worker.config.Now().UTC().Add(rewardRetryDelay(item.Attempts, indefinitelyRetryable))
		if releaseErr := worker.repository.Release(ctx, item, next, errorCode, terminal); releaseErr != nil {
			failures = append(failures, fmt.Errorf("settle reward %s/%s: %w; release: %v",
				item.WindowStart.Format(time.RFC3339), item.UserID, settleErr, releaseErr))
			continue
		}
		failures = append(failures, fmt.Errorf("settle reward %s/%s: %w",
			item.WindowStart.Format(time.RFC3339), item.UserID, settleErr))
	}
	return len(pending), errors.Join(failures...)
}

func (worker *Worker) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		processed, err := worker.RunOnce(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			worker.logger.ErrorContext(ctx, "seeding reward batch failed", "error", err)
		}
		delay := worker.config.PollInterval
		if processed == int(worker.config.BatchSize) {
			delay = 0
		}
		timer.Reset(delay)
	}
}

func rewardErrorCode(err error) (string, bool) {
	switch {
	case errors.Is(err, ErrPolicyNotFound):
		return "policy_not_found", true
	case errors.Is(err, ErrBenefitNotFound):
		return "benefit_not_found", true
	case errors.Is(err, ErrWorkLease):
		return "lease_lost", false
	case errors.Is(err, ErrEvidenceConflict):
		return "evidence_conflict", false
	case errors.Is(err, ErrInput):
		return "invalid_input", false
	case errors.Is(err, ErrInvariant):
		return "invariant_failed", false
	default:
		return "temporary_failure", false
	}
}

func rewardRetryDelay(attempts int32, blocked bool) time.Duration {
	if blocked {
		return time.Minute
	}
	if attempts < 1 {
		attempts = 1
	}
	shift := attempts - 1
	if shift > 6 {
		shift = 6
	}
	return time.Second * time.Duration(1<<shift)
}
