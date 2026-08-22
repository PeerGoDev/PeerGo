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
	"github.com/peergo/peergo/services/settlement/internal/storagecleanup"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Settlement storage maintenance stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadStorageCleanup()
	if err != nil {
		return fmt.Errorf("load Settlement storage maintenance configuration: %w", err)
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
	repository, err := storagecleanup.NewPostgresRepository(pool)
	if err != nil {
		return fmt.Errorf("compose Settlement storage repository: %w", err)
	}
	worker, err := storagecleanup.NewWorker(repository, storagecleanup.WorkerConfig{
		RunInterval: settings.RunInterval, TerminalRetention: settings.TerminalRetention,
		SessionRetention: settings.SessionRetention, DetailRetention: settings.DetailRetention,
		AnomalyRetention: settings.AnomalyRetention, BatchSize: settings.BatchSize,
	}, time.Now, logger)
	if err != nil {
		return fmt.Errorf("compose Settlement storage worker: %w", err)
	}
	logger.Info("Settlement bounded storage maintenance started",
		"interval", settings.RunInterval, "batch_size", settings.BatchSize,
		"terminal_retention", settings.TerminalRetention, "session_retention", settings.SessionRetention,
		"detail_retention", settings.DetailRetention, "anomaly_retention", settings.AnomalyRetention)
	return worker.Run(rootCtx)
}
