package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/progression"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	settings, err := config.LoadCoreDatabaseProcess()
	if err != nil {
		logger.Error("invalid level policy worker configuration", "error", err)
		os.Exit(1)
	}
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()
	pool, err := pgxpool.New(startupCtx, settings.DatabaseURL)
	if err != nil {
		logger.Error("open Core database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(startupCtx); err != nil {
		logger.Error("ping Core database", "error", err)
		os.Exit(1)
	}
	if err := platformpostgres.RequireCurrentMigration(startupCtx, pool); err != nil {
		logger.Error("Core database is not ready", "error", err)
		os.Exit(1)
	}
	repository, err := progression.NewPostgresLevelPolicyRepository(pool)
	if err != nil {
		logger.Error("compose level policy repository", "error", err)
		os.Exit(1)
	}
	worker, err := progression.NewLevelPolicyWorker(repository, progression.LevelPolicyWorkerConfig{}, logger, time.Now)
	if err != nil {
		logger.Error("compose level policy worker", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("Core level policy worker started")
	if err := worker.Run(ctx); err != nil {
		logger.Error("Core level policy worker stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}
