package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/audit"
	"github.com/peergo/peergo/services/core/internal/platform/auditsink"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	settings, err := config.LoadAuditWorker()
	if err != nil {
		logger.Error("invalid audit worker configuration", "error", err)
		os.Exit(1)
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()
	pool, err := pgxpool.New(startupCtx, settings.DatabaseURL)
	if err != nil {
		logger.Error("open core database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(startupCtx); err != nil {
		logger.Error("ping core database", "error", err)
		os.Exit(1)
	}
	if err := platformpostgres.RequireCurrentMigration(startupCtx, pool); err != nil {
		logger.Error("core database is not ready", "error", err)
		os.Exit(1)
	}

	sinkClient, err := auditsink.NewClient(settings.AuditSinkURL, settings.ServiceToken, 5*time.Second)
	if err != nil {
		logger.Error("compose audit sink client", "error", err)
		os.Exit(1)
	}
	dispatcher, err := audit.NewDispatcher(
		audit.NewPostgresRepository(pool),
		sinkClient,
		audit.DispatcherConfig{},
		logger,
	)
	if err != nil {
		logger.Error("compose audit dispatcher", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("core audit worker started")
	if err := dispatcher.Run(ctx); err != nil {
		logger.Error("core audit worker stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}
