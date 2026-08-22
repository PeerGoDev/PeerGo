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

	"github.com/peergo/peergo/services/settlement/internal/config"
	"github.com/peergo/peergo/services/settlement/internal/ingest"
	"github.com/peergo/peergo/services/settlement/internal/jetstreamconsumer"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Settlement stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadRuntime()
	if err != nil {
		return fmt.Errorf("load Settlement configuration: %w", err)
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

	connection, js, err := jetstreamconsumer.Connect(settings.NATS, "peergo-settlement-runtime", logger)
	if err != nil {
		return fmt.Errorf("connect Settlement JetStream runtime: %w", err)
	}
	defer connection.Close()
	source, err := jetstreamconsumer.OpenSource(startupCtx, js, jetstreamconsumer.BindingConfig{
		Stream: settings.Stream, Subject: settings.Subject, Durable: settings.Durable,
		FetchWait: settings.FetchWait, MaximumProcessingTime: settings.ProcessTimeout,
		MaximumAckTime: settings.AckTimeout, BatchSize: settings.BatchSize,
	})
	if err != nil {
		return err
	}
	repository, err := ingest.NewPostgresRepository(pool, settings.Stream, settings.Subject, time.Now)
	if err != nil {
		return fmt.Errorf("compose Settlement Tracker Ledger repository: %w", err)
	}
	runner, err := jetstreamconsumer.NewRunner(source, repository, jetstreamconsumer.RunnerConfig{
		Stream: settings.Stream, Subject: settings.Subject, Durable: settings.Durable,
		ProcessTimeout: settings.ProcessTimeout, AckTimeout: settings.AckTimeout,
		RetryDelay: settings.RetryDelay, BatchSize: settings.BatchSize,
	}, logger)
	if err != nil {
		return fmt.Errorf("compose Settlement consumer: %w", err)
	}

	logger.Info("Settlement raw Tracker ledger consumer started",
		"stream", settings.Stream, "subject", settings.Subject,
		"consumer", settings.Durable, "batch_size", settings.BatchSize)
	runtimeErr := runner.Run(rootCtx)
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), settings.ShutdownTimeout)
	defer cancelShutdown()
	if err := connection.FlushWithContext(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		runtimeErr = errors.Join(runtimeErr, fmt.Errorf("flush Settlement NATS connection: %w", err))
	}
	return runtimeErr
}
