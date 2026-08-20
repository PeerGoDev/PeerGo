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
	"github.com/peergo/peergo/services/settlement/internal/hnroutbox"
	"github.com/peergo/peergo/services/settlement/internal/jetstreamconsumer"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Settlement H&R dispatcher stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadHNRDispatcher()
	if err != nil {
		return fmt.Errorf("load Settlement H&R dispatcher configuration: %w", err)
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
	connection, js, err := jetstreamconsumer.Connect(settings.NATS, "peergo-settlement-hnr-dispatcher", logger)
	if err != nil {
		return fmt.Errorf("connect Settlement H&R JetStream publisher: %w", err)
	}
	defer connection.Close()
	repository, err := hnroutbox.NewPostgresRepository(pool)
	if err != nil {
		return fmt.Errorf("compose Settlement H&R outbox repository: %w", err)
	}
	publisher, err := hnroutbox.NewJetStreamPublisher(js, settings.Stream, settings.Subject)
	if err != nil {
		return fmt.Errorf("compose Settlement H&R JetStream publisher: %w", err)
	}
	dispatcher, err := hnroutbox.NewDispatcher(repository, publisher, hnroutbox.DispatcherConfig{
		LeaseDuration: settings.LeaseDuration, IdleInterval: settings.IdleInterval,
		RetryBase: settings.RetryBase, PublishTimeout: settings.PublishTimeout,
	}, time.Now, logger)
	if err != nil {
		return fmt.Errorf("compose Settlement H&R dispatcher: %w", err)
	}
	logger.Info("Settlement H&R outbox dispatcher started", "stream", settings.Stream, "subject", settings.Subject)
	runtimeErr := dispatcher.Run(rootCtx)
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), settings.ShutdownTimeout)
	defer cancelShutdown()
	if err := connection.FlushWithContext(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		runtimeErr = errors.Join(runtimeErr, fmt.Errorf("flush Settlement H&R NATS connection: %w", err))
	}
	return runtimeErr
}
