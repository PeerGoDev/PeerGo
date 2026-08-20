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
	"github.com/peergo/peergo/services/core/internal/modules/swarmprojection"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
	"github.com/peergo/peergo/services/core/internal/swarmconsumer"
	"github.com/peergo/peergo/services/core/internal/trafficconsumer"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Core swarm projector stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadSwarmProjector()
	if err != nil {
		return fmt.Errorf("load Core swarm projector configuration: %w", err)
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
	connection, js, err := trafficconsumer.Connect(settings.NATS, "peergo-core-swarm-projector", logger)
	if err != nil {
		return fmt.Errorf("connect Core swarm JetStream consumer: %w", err)
	}
	defer connection.Close()
	snapshotSource, err := trafficconsumer.OpenSource(startupCtx, js, settings.Snapshot)
	if err != nil {
		return fmt.Errorf("open Core swarm snapshot source: %w", err)
	}
	completionSource, err := trafficconsumer.OpenSource(startupCtx, js, settings.Completion)
	if err != nil {
		return fmt.Errorf("open Core swarm completion source: %w", err)
	}
	projector, err := swarmprojection.NewPostgresRepository(pool, time.Now, settings.MaxFutureSkew)
	if err != nil {
		return fmt.Errorf("compose Core swarm repository: %w", err)
	}
	snapshotRunner, err := swarmconsumer.NewSnapshotRunner(snapshotSource, projector, settings.Snapshot, settings.RetryDelay, time.Now, logger)
	if err != nil {
		return fmt.Errorf("compose Core swarm snapshot runner: %w", err)
	}
	completionRunner, err := swarmconsumer.NewCompletionRunner(completionSource, projector, settings.Completion, settings.RetryDelay, time.Now, logger)
	if err != nil {
		return fmt.Errorf("compose Core swarm completion runner: %w", err)
	}
	logger.Info("Core swarm projector started",
		"snapshot_stream", settings.Snapshot.Stream, "snapshot_durable", settings.Snapshot.Durable,
		"completion_stream", settings.Completion.Stream, "completion_durable", settings.Completion.Durable)
	runtimeErr := runProjectors(rootCtx, snapshotRunner, completionRunner)
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), settings.ShutdownTimeout)
	defer cancelShutdown()
	if err := connection.FlushWithContext(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		runtimeErr = errors.Join(runtimeErr, fmt.Errorf("flush Core swarm NATS connection: %w", err))
	}
	return runtimeErr
}

func runProjectors(ctx context.Context, snapshot, completion *swarmconsumer.Runner) error {
	runtimeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- snapshot.Run(runtimeCtx) }()
	go func() { results <- completion.Run(runtimeCtx) }()

	var runtimeErr error
	remaining := 2
	ctxDone := ctx.Done()
	for remaining > 0 {
		select {
		case err := <-results:
			remaining--
			if err == nil && ctx.Err() == nil {
				err = errors.New("Core swarm projection runner stopped unexpectedly")
			}
			if err != nil {
				runtimeErr = errors.Join(runtimeErr, err)
				cancel()
			}
		case <-ctxDone:
			cancel()
			ctxDone = nil
		}
	}
	return runtimeErr
}
