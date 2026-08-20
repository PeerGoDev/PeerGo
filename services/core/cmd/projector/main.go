package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	drain := flag.Bool("drain", false, "project queued control events and exit when the outbox is empty")
	drainTimeout := flag.Duration("drain-timeout", 30*time.Minute, "maximum time allowed for --drain")
	flag.Parse()
	if *drainTimeout < time.Second || *drainTimeout > 24*time.Hour {
		logger.Error("invalid Tracker control projector drain timeout")
		os.Exit(2)
	}
	settings, err := config.LoadCoreDatabaseProcess()
	if err != nil {
		logger.Error("invalid Tracker control projector configuration", "error", err)
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
	repository, err := trackercontrol.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose Tracker control repository", "error", err)
		os.Exit(1)
	}
	projector, err := trackercontrol.NewProjector(repository, trackercontrol.ProjectorConfig{}, time.Now, logger)
	if err != nil {
		logger.Error("compose Tracker control projector", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *drain {
		if err := drainProjection(ctx, projector, repository, *drainTimeout, logger); err != nil {
			logger.Error("Core Tracker control projector drain failed", "error", err)
			os.Exit(1)
		}
		return
	}
	logger.Info("Core Tracker control projector started")
	if err := projector.Run(ctx); err != nil {
		logger.Error("Core Tracker control projector stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}

// drainProjection is an operator mode used by finite cutovers. An empty claim
// alone is insufficient because a failed event can be leased or delayed for a
// retry; the command exits successfully only when the repository reports zero
// pending events and a continuous projection watermark.
func drainProjection(
	ctx context.Context,
	projector *trackercontrol.Projector,
	repository *trackercontrol.PostgresRepository,
	timeout time.Duration,
	logger *slog.Logger,
) error {
	drainCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var processed int64
	for {
		projected, err := projector.RunOnce(drainCtx)
		if err != nil {
			return err
		}
		if projected {
			processed++
			if processed%250 == 0 {
				logger.Info("Core Tracker control projector drain progress", "processed", processed)
			}
			continue
		}
		status, err := repository.Status(drainCtx)
		if err != nil {
			return err
		}
		if status.PendingEvents == 0 {
			logger.Info(
				"Core Tracker control projector drain completed",
				"processed", processed,
				"last_sequence", status.LastSequence,
			)
			return nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-drainCtx.Done():
			timer.Stop()
			return errors.New("Tracker control projector drain timed out with pending events")
		case <-timer.C:
		}
	}
}
