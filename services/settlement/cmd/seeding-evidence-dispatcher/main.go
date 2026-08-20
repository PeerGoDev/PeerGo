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
	"github.com/peergo/peergo/services/settlement/internal/seedingoutbox"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Settlement seeding evidence dispatcher stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadSeedingEvidenceDispatcher()
	if err != nil {
		return fmt.Errorf("load Settlement seeding evidence dispatcher configuration: %w", err)
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
	connection, js, err := jetstreamconsumer.Connect(settings.NATS, "peergo-settlement-seeding-evidence-dispatcher", logger)
	if err != nil {
		return fmt.Errorf("connect Settlement seeding evidence JetStream publisher: %w", err)
	}
	defer connection.Close()
	repository, err := seedingoutbox.NewPostgresRepository(pool)
	if err != nil {
		return fmt.Errorf("compose Settlement seeding evidence outbox repository: %w", err)
	}
	publisher, err := seedingoutbox.NewJetStreamPublisher(js, settings.Stream, settings.Subject)
	if err != nil {
		return fmt.Errorf("compose Settlement seeding evidence JetStream publisher: %w", err)
	}
	dispatcher, err := seedingoutbox.NewDispatcher(repository, publisher, seedingoutbox.DispatcherConfig{
		LeaseDuration: settings.LeaseDuration, IdleInterval: settings.IdleInterval,
		RetryBase: settings.RetryBase, PublishTimeout: settings.PublishTimeout,
	}, time.Now, logger)
	if err != nil {
		return fmt.Errorf("compose Settlement seeding evidence dispatcher: %w", err)
	}
	logger.Info("Settlement seeding evidence outbox dispatcher started", "stream", settings.Stream, "subject", settings.Subject)
	runtimeErr := dispatcher.Run(rootCtx)
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), settings.ShutdownTimeout)
	defer cancelShutdown()
	if err := connection.FlushWithContext(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		runtimeErr = errors.Join(runtimeErr, fmt.Errorf("flush Settlement seeding evidence NATS connection: %w", err))
	}
	return runtimeErr
}
