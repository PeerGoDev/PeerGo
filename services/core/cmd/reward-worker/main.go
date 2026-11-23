package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
	"github.com/peergo/peergo/services/core/internal/modules/progression"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil && !errors.Is(err, ctx.Err()) {
		logger.Error("Core reward worker failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	settings, err := config.LoadCoreDatabaseProcess()
	if err != nil {
		return fmt.Errorf("load Core reward worker configuration: %w", err)
	}
	startupCtx, cancelStartup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelStartup()
	pool, err := pgxpool.New(startupCtx, settings.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open Core database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(startupCtx); err != nil {
		return fmt.Errorf("ping Core database: %w", err)
	}
	if err := platformpostgres.RequireCurrentMigration(startupCtx, pool); err != nil {
		return fmt.Errorf("check Core database migrations: %w", err)
	}

	seedingLogger := logger.With("worker", "seeding_reward")
	seedingRepository, err := seedingreward.NewPostgresSettlementRepository(pool)
	if err != nil {
		return fmt.Errorf("compose seeding reward repository: %w", err)
	}
	seedingWorker, err := seedingreward.NewWorker(seedingRepository, seedingreward.WorkerConfig{}, seedingLogger)
	if err != nil {
		return fmt.Errorf("compose seeding reward worker: %w", err)
	}
	contributionLogger := logger.With("worker", "contribution_experience")
	contributionRepository, err := progression.NewPostgresContributionSettlementRepository(pool)
	if err != nil {
		return fmt.Errorf("compose contribution experience repository: %w", err)
	}
	contributionWorker, err := progression.NewContributionSettlementWorker(contributionRepository, progression.ContributionSettlementWorkerConfig{}, contributionLogger)
	if err != nil {
		return fmt.Errorf("compose contribution experience worker: %w", err)
	}
	cancelStartup()
	seedingLogger.InfoContext(ctx, "Core seeding reward worker started")
	contributionLogger.InfoContext(ctx, "Core contribution experience worker started")
	return runWorkers(ctx, seedingWorker.Run, contributionWorker.Run)
}

func runWorkers(ctx context.Context, seedingRewards, contributionExperience func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	start := func(name string, run func(context.Context) error) {
		go func() {
			err := run(ctx)
			if err != nil {
				err = fmt.Errorf("%s: %w", name, err)
			} else if ctx.Err() == nil {
				err = fmt.Errorf("%s stopped unexpectedly", name)
			}
			results <- err
		}()
	}
	start("seeding reward worker", seedingRewards)
	start("contribution experience worker", contributionExperience)
	first := <-results
	cancel()
	return errors.Join(first, <-results)
}
