package progression

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

const (
	defaultContributionSettlementBatchSize    = 100
	defaultContributionSettlementPollInterval = time.Second
)

type ContributionSettlementKind string

const (
	ContributionSettlementUpload     ContributionSettlementKind = "upload"
	ContributionSettlementPublish    ContributionSettlementKind = "torrent_publish"
	ContributionSettlementAccountDay ContributionSettlementKind = "account_day"
)

// ContributionSettlementResult is intentionally small: immutable calculation
// inputs live in the progression ledger and source tables, while the worker
// log only needs enough data to identify one completed unit of work.
type ContributionSettlementResult struct {
	Kind              ContributionSettlementKind
	UserID            uuid.UUID
	SourceReference   string
	PolicyRevision    string
	ExperienceAmount  string
	ExperienceEntryID uuid.UUID
}

type ContributionSettlementRepository interface {
	SettleNextUpload(context.Context, time.Time) (ContributionSettlementResult, bool, error)
	SettleNextTorrentPublish(context.Context, time.Time) (ContributionSettlementResult, bool, error)
	SettleNextAccountDay(context.Context, time.Time) (ContributionSettlementResult, bool, error)
}

type ContributionSettlementWorkerConfig struct {
	BatchSize    int
	PollInterval time.Duration
	Now          func() time.Time
}

type ContributionSettlementWorker struct {
	repository ContributionSettlementRepository
	config     ContributionSettlementWorkerConfig
	logger     *slog.Logger
}

func NewContributionSettlementWorker(repository ContributionSettlementRepository, config ContributionSettlementWorkerConfig, logger *slog.Logger) (*ContributionSettlementWorker, error) {
	if repository == nil {
		return nil, ErrInput
	}
	if config.BatchSize == 0 {
		config.BatchSize = defaultContributionSettlementBatchSize
	}
	if config.PollInterval == 0 {
		config.PollInterval = defaultContributionSettlementPollInterval
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.BatchSize < 1 || config.BatchSize > 1000 || config.PollInterval <= 0 || config.PollInterval > time.Minute {
		return nil, ErrInput
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ContributionSettlementWorker{repository: repository, config: config, logger: logger}, nil
}

func (worker *ContributionSettlementWorker) RunOnce(ctx context.Context) (int, error) {
	settlers := []func(context.Context, time.Time) (ContributionSettlementResult, bool, error){
		worker.repository.SettleNextUpload,
		worker.repository.SettleNextTorrentPublish,
		worker.repository.SettleNextAccountDay,
	}
	processed := 0
	var failures []error
	for processed < worker.config.BatchSize {
		cycleProcessed := false
		for _, settle := range settlers {
			if processed >= worker.config.BatchSize {
				break
			}
			result, found, err := settle(ctx, worker.config.Now().UTC())
			if err != nil {
				failures = append(failures, err)
				continue
			}
			if !found {
				continue
			}
			cycleProcessed = true
			processed++
			worker.logger.InfoContext(ctx, "contribution experience settled",
				"kind", result.Kind,
				"user_id", result.UserID,
				"source_reference", result.SourceReference,
				"policy_revision", result.PolicyRevision,
				"experience", result.ExperienceAmount,
			)
		}
		if !cycleProcessed || len(failures) > 0 {
			break
		}
	}
	return processed, errors.Join(failures...)
}

func (worker *ContributionSettlementWorker) Run(ctx context.Context) error {
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
			worker.logger.ErrorContext(ctx, "contribution experience batch failed", "error", err)
		}
		delay := worker.config.PollInterval
		if processed == worker.config.BatchSize {
			delay = 0
		}
		timer.Reset(delay)
	}
}
