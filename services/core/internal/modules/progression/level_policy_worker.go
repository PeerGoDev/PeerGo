package progression

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type LevelPolicyWorkerConfig struct {
	PollInterval time.Duration
	MaximumBatch int
}

type LevelPolicyWorker struct {
	repository LevelPolicyRepository
	config     LevelPolicyWorkerConfig
	logger     *slog.Logger
	now        func() time.Time
}

func NewLevelPolicyWorker(repository LevelPolicyRepository, config LevelPolicyWorkerConfig, logger *slog.Logger, now func() time.Time) (*LevelPolicyWorker, error) {
	if repository == nil {
		return nil, errors.New("level policy worker repository is required")
	}
	if config.PollInterval == 0 {
		config.PollInterval = 30 * time.Second
	}
	if config.MaximumBatch == 0 {
		config.MaximumBatch = 4
	}
	if config.PollInterval < time.Second || config.PollInterval > time.Hour || config.MaximumBatch < 1 || config.MaximumBatch > 32 {
		return nil, errors.New("level policy worker configuration is invalid")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	return &LevelPolicyWorker{repository: repository, config: config, logger: logger, now: now}, nil
}

func (worker *LevelPolicyWorker) RunOnce(ctx context.Context) error {
	for index := 0; index < worker.config.MaximumBatch; index++ {
		activation, found, err := worker.repository.ActivateDueLevelPolicy(ctx, worker.now())
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		worker.logger.InfoContext(ctx, "level policy activated",
			"policy_version", activation.PolicyVersion,
			"affected_users", activation.AffectedUsers,
			"changed_levels", activation.ChangedLevels,
			"applied_at", activation.AppliedAt,
		)
	}
	return nil
}

func (worker *LevelPolicyWorker) Run(ctx context.Context) error {
	if err := worker.RunOnce(ctx); err != nil {
		worker.logger.ErrorContext(ctx, "initial level policy activation failed", "error", err)
	}
	ticker := time.NewTicker(worker.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := worker.RunOnce(ctx); err != nil {
				worker.logger.ErrorContext(ctx, "level policy activation failed", "error", err)
			}
		}
	}
}
