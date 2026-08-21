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
	"github.com/peergo/peergo/services/settlement/internal/settler"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Settlement policy worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadPolicyWorker()
	if err != nil {
		return fmt.Errorf("load Settlement policy worker configuration: %w", err)
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
	repository, err := settler.NewPostgresRepository(pool)
	if err != nil {
		return fmt.Errorf("compose Settlement policy repository: %w", err)
	}
	worker, err := settler.NewWorker(repository, settler.WorkerConfig{
		LeaseDuration: settings.LeaseDuration, IdleInterval: settings.IdleInterval, RetryBase: settings.RetryBase,
		Concurrency: settings.Concurrency,
	}, time.Now, logger)
	if err != nil {
		return fmt.Errorf("compose Settlement policy worker: %w", err)
	}
	logger.Info("Settlement immutable policy worker started", "concurrency", settings.Concurrency)
	return worker.Run(rootCtx)
}
