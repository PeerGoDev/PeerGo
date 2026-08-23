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
	"github.com/peergo/peergo/services/core/internal/platform/config"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
	"github.com/peergo/peergo/services/core/internal/trafficcleanup"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Core storage maintenance stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadCoreStorageCleanup()
	if err != nil {
		return fmt.Errorf("load Core storage maintenance configuration: %w", err)
	}
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancelStartup := context.WithTimeout(rootCtx, settings.StartupTimeout)
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
		return err
	}
	repository, err := trafficcleanup.NewPostgresRepository(pool)
	if err != nil {
		return fmt.Errorf("compose Core traffic cleanup repository: %w", err)
	}
	worker, err := trafficcleanup.NewWorker(repository, trafficcleanup.WorkerConfig{
		RunInterval: settings.RunInterval, DetailRetention: settings.DetailRetention,
		HistoryRetention: settings.HistoryRetention, BatchSize: settings.BatchSize,
	}, time.Now, logger)
	if err != nil {
		return fmt.Errorf("compose Core traffic cleanup worker: %w", err)
	}
	logger.Info("Core bounded traffic storage maintenance started",
		"interval", settings.RunInterval, "detail_retention", settings.DetailRetention,
		"history_retention", settings.HistoryRetention, "batch_size", settings.BatchSize)
	return worker.Run(rootCtx)
}
