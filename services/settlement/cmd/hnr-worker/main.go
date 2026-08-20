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
	"github.com/peergo/peergo/services/settlement/internal/hnr"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Settlement H&R worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadHNRWorker()
	if err != nil {
		return fmt.Errorf("load Settlement H&R worker configuration: %w", err)
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
	repository, err := hnr.NewPostgresRepository(pool)
	if err != nil {
		return fmt.Errorf("compose Settlement H&R repository: %w", err)
	}
	worker, err := hnr.NewWorker(repository, hnr.WorkerConfig{
		LeaseDuration: settings.LeaseDuration, IdleInterval: settings.IdleInterval, RetryBase: settings.RetryBase,
	}, time.Now, logger)
	if err != nil {
		return fmt.Errorf("compose Settlement H&R worker: %w", err)
	}
	logger.Info("Settlement immutable H&R worker started")
	return worker.Run(rootCtx)
}
