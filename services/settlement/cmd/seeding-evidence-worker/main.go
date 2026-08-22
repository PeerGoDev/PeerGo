package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/settlement/internal/config"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
	"github.com/peergo/peergo/services/settlement/internal/seedingevidence"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Settlement seeding evidence worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadSeedingEvidenceWorker()
	if err != nil {
		return fmt.Errorf("load seeding evidence worker configuration: %w", err)
	}
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancelStartup := context.WithTimeout(rootCtx, settings.StartupTimeout)
	defer cancelStartup()
	pool, err := pgxpool.New(startupCtx, settings.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open Tracker Ledger database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(startupCtx); err != nil {
		return fmt.Errorf("ping Tracker Ledger database: %w", err)
	}
	if err := platformpostgres.RequireCurrentMigration(startupCtx, pool); err != nil {
		return err
	}
	repository, err := seedingevidence.NewPostgresRepository(pool, seedingevidence.PostgresRepositoryConfig{
		AnnounceStream: settings.AnnounceStream, SnapshotStream: settings.SnapshotStream,
		SnapshotSubject: settings.SnapshotSubject, MaxFutureSkew: settings.MaxFutureSkew,
		MaximumSnapshotClosureDelay: settings.SnapshotMaxDelay, ClosureDelay: settings.ClosureDelay,
		MaxIntervalCredit: settings.MaxIntervalCredit,
	}, time.Now)
	if err != nil {
		return fmt.Errorf("compose seeding evidence repository: %w", err)
	}
	worker, err := seedingevidence.NewWorker(repository, seedingevidence.WorkerConfig{
		InitialWindowStart: settings.InitialWindow, ClosureDelay: settings.ClosureDelay,
		IdleInterval: settings.IdleInterval,
	}, time.Now, logger)
	if err != nil {
		return fmt.Errorf("compose seeding evidence worker: %w", err)
	}
	logger.Info("Settlement seeding evidence worker started",
		"initial_window_start", settings.InitialWindow, "closure_delay", settings.ClosureDelay,
		"max_interval_credit", settings.MaxIntervalCredit)
	return worker.Run(rootCtx)
}
