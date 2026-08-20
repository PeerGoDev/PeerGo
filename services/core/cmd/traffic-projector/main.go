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
	"github.com/peergo/peergo/services/core/internal/modules/traffic"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
	"github.com/peergo/peergo/services/core/internal/trafficconsumer"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Core traffic projector stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadTrafficProjector()
	if err != nil {
		return fmt.Errorf("load Core traffic projector configuration: %w", err)
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
	connection, js, err := trafficconsumer.Connect(settings.NATS, "peergo-core-traffic-projector", logger)
	if err != nil {
		return fmt.Errorf("connect Core traffic JetStream consumer: %w", err)
	}
	defer connection.Close()
	source, err := trafficconsumer.OpenSource(startupCtx, js, trafficconsumer.BindingConfig{
		Stream: settings.Stream, Subject: settings.Subject, Durable: settings.Durable,
		FetchWait: settings.FetchWait, MaximumProcessingTime: settings.ProcessTimeout, MaximumAckTime: settings.AckTimeout,
	})
	if err != nil {
		return fmt.Errorf("open Core traffic JetStream source: %w", err)
	}
	projector, err := traffic.NewPostgresRepository(pool, time.Now)
	if err != nil {
		return fmt.Errorf("compose Core traffic repository: %w", err)
	}
	runner, err := trafficconsumer.NewRunner(source, projector, trafficconsumer.RunnerConfig{
		Stream: settings.Stream, Subject: settings.Subject, Durable: settings.Durable,
		ProcessTimeout: settings.ProcessTimeout, AckTimeout: settings.AckTimeout, RetryDelay: settings.RetryDelay,
	}, time.Now, logger)
	if err != nil {
		return fmt.Errorf("compose Core traffic projector: %w", err)
	}
	logger.Info("Core traffic projector started", "stream", settings.Stream, "subject", settings.Subject, "durable", settings.Durable)
	runtimeErr := runner.Run(rootCtx)
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), settings.ShutdownTimeout)
	defer cancelShutdown()
	if err := connection.FlushWithContext(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		runtimeErr = errors.Join(runtimeErr, fmt.Errorf("flush Core traffic NATS connection: %w", err))
	}
	return runtimeErr
}
