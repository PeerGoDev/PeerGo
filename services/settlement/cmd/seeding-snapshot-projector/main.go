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
	"github.com/peergo/peergo/services/settlement/internal/jetstreamconsumer"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
	"github.com/peergo/peergo/services/settlement/internal/seedingevidence"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Settlement seeding snapshot projector stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadSeedingSnapshotRuntime()
	if err != nil {
		return fmt.Errorf("load seeding snapshot projector configuration: %w", err)
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
	connection, js, err := jetstreamconsumer.Connect(settings.NATS, "peergo-settlement-seeding-snapshot-projector", logger)
	if err != nil {
		return err
	}
	defer connection.Close()
	binding := jetstreamconsumer.BindingConfig{
		Stream: settings.Stream, Subject: settings.Subject, Durable: settings.Durable,
		FetchWait: settings.FetchWait, MaximumProcessingTime: settings.ProcessTimeout,
		MaximumAckTime: settings.AckTimeout,
	}
	source, err := jetstreamconsumer.OpenSource(startupCtx, js, binding)
	if err != nil {
		return err
	}
	repository, err := seedingevidence.NewPostgresRepository(pool, seedingevidence.PostgresRepositoryConfig{
		AnnounceStream: settings.AnnounceStream, SnapshotStream: settings.Stream,
		SnapshotSubject: settings.Subject, MaxFutureSkew: settings.MaxFutureSkew,
		MaximumSnapshotClosureDelay: settings.ClosureDelay,
	}, time.Now)
	if err != nil {
		return fmt.Errorf("compose seeding snapshot repository: %w", err)
	}
	runner, err := seedingevidence.NewSnapshotRunner(source, repository, seedingevidence.SnapshotRunnerConfig{
		Stream: settings.Stream, Subject: settings.Subject, Durable: settings.Durable,
		ProcessTimeout: settings.ProcessTimeout, AckTimeout: settings.AckTimeout, RetryDelay: settings.RetryDelay,
	}, logger)
	if err != nil {
		return fmt.Errorf("compose seeding snapshot runner: %w", err)
	}
	logger.Info("Settlement seeding snapshot projector started",
		"stream", settings.Stream, "subject", settings.Subject, "consumer", settings.Durable)
	runtimeErr := runner.Run(rootCtx)
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), settings.ShutdownTimeout)
	defer cancelShutdown()
	if err := connection.FlushWithContext(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		runtimeErr = errors.Join(runtimeErr, fmt.Errorf("flush seeding snapshot NATS connection: %w", err))
	}
	return runtimeErr
}
