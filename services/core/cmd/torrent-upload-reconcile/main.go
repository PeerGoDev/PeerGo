package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	"github.com/peergo/peergo/services/core/internal/platform/objectstore"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	retention := flag.Duration("retention", 24*time.Hour, "minimum age before an incomplete upload may be abandoned")
	batchSize := flag.Int("batch-size", 20, "bounded cleanup batch size")
	flag.Parse()
	if *retention < 24*time.Hour || *retention > 30*24*time.Hour || *batchSize < 1 || *batchSize > 100 {
		logger.Error("invalid torrent upload reconciliation flags")
		os.Exit(2)
	}
	settings, err := config.LoadTorrentUploadStorageTool()
	if err != nil {
		logger.Error("invalid torrent upload reconciliation configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancelStartup := context.WithTimeout(ctx, 15*time.Second)
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
	store, err := objectstore.NewConfigured(startupCtx, settings.Store)
	if err != nil {
		logger.Error("compose torrent object store", "error", err)
		os.Exit(1)
	}
	registry, err := torrents.NewStoreRegistry(store)
	if err != nil {
		logger.Error("compose torrent object store registry", "error", err)
		os.Exit(1)
	}
	repository, err := torrents.NewPostgresTorrentUploadRepository(pool)
	if err != nil {
		logger.Error("compose torrent upload repository", "error", err)
		os.Exit(1)
	}
	service, err := torrents.NewTorrentUploadOrphanService(repository, registry, torrents.TorrentUploadOrphanServiceConfig{
		Retention: *retention, BatchSize: int32(*batchSize),
	})
	if err != nil {
		logger.Error("compose torrent upload orphan service", "error", err)
		os.Exit(1)
	}

	total := 0
	for {
		processed, runErr := service.RunBatch(ctx, store.BackendID())
		total += processed
		if runErr != nil {
			logger.Error("torrent upload orphan cleanup failed", "processed", total, "error", runErr)
			os.Exit(1)
		}
		if processed == 0 {
			logger.Info("torrent upload orphan cleanup complete", "processed", total, "backend_id", store.BackendID())
			return
		}
	}
}
