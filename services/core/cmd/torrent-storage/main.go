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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	"github.com/peergo/peergo/services/core/internal/platform/objectstore"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	action := flag.String("action", "", "plan, copy, cutover, approve-cleanup, or cleanup")
	migrationIDValue := flag.String("migration-id", "", "existing migration UUID; plan generates one when omitted")
	actorIDValue := flag.String("actor-id", "", "staff user UUID for plan or cleanup approval")
	modeValue := flag.String("mode", string(torrents.StorageMigrationMove), "plan mode: move or replicate")
	retention := flag.Duration("retention", 7*24*time.Hour, "source retention after move cutover")
	batchSize := flag.Int("batch-size", 20, "bounded copy or cleanup batch size")
	flag.Parse()

	if *action == "" || *batchSize < 1 || *batchSize > 100 {
		logger.Error("invalid torrent storage command flags")
		os.Exit(2)
	}
	settings, err := config.LoadTorrentStorageTool()
	if err != nil {
		logger.Error("invalid torrent storage configuration", "error", err)
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

	source, err := objectstore.NewConfigured(startupCtx, settings.Source)
	if err != nil {
		logger.Error("compose source object store", "error", err)
		os.Exit(1)
	}
	destination, err := objectstore.NewConfigured(startupCtx, settings.Destination)
	if err != nil {
		logger.Error("compose destination object store", "error", err)
		os.Exit(1)
	}
	registry, err := torrents.NewStoreRegistry(source, destination)
	if err != nil {
		logger.Error("compose object store registry", "error", err)
		os.Exit(1)
	}
	repository, err := torrents.NewPostgresStorageMigrationRepository(pool)
	if err != nil {
		logger.Error("compose storage migration repository", "error", err)
		os.Exit(1)
	}
	service, err := torrents.NewStorageMigrationService(repository, registry, torrents.StorageMigrationServiceConfig{
		BatchSize: int32(*batchSize),
	})
	if err != nil {
		logger.Error("compose storage migration service", "error", err)
		os.Exit(1)
	}

	if err := execute(ctx, service, commandInput{
		action: *action, migrationID: *migrationIDValue, actorID: *actorIDValue,
		mode: *modeValue, retention: *retention,
		sourceBackendID: source.BackendID(), destinationBackendID: destination.BackendID(),
	}, logger); err != nil {
		logger.Error("torrent storage command failed", "action", *action, "error", err)
		os.Exit(1)
	}
}

type commandInput struct {
	action               string
	migrationID          string
	actorID              string
	mode                 string
	retention            time.Duration
	sourceBackendID      torrents.StorageBackendID
	destinationBackendID torrents.StorageBackendID
}

func execute(ctx context.Context, service *torrents.StorageMigrationService, input commandInput, logger *slog.Logger) error {
	switch input.action {
	case "plan":
		migrationID := uuid.New()
		if input.migrationID != "" {
			parsed, err := uuid.Parse(input.migrationID)
			if err != nil {
				return errors.New("migration-id must be a UUID")
			}
			migrationID = parsed
		}
		actorID, err := uuid.Parse(input.actorID)
		if err != nil {
			return errors.New("actor-id must be a staff user UUID")
		}
		plan, err := service.Plan(ctx, torrents.PlanStorageMigrationInput{
			ID: migrationID, Mode: torrents.StorageMigrationMode(input.mode),
			SourceBackendID: input.sourceBackendID, DestinationBackendID: input.destinationBackendID,
			RequestedBy: actorID, OccurredAt: time.Now(),
		})
		if err != nil {
			return err
		}
		logger.Info("torrent storage migration planned",
			"migration_id", plan.ID, "mode", plan.Mode, "object_count", plan.ObjectCount,
			"source_backend_id", plan.SourceBackendID, "destination_backend_id", plan.DestinationBackendID,
		)
		return nil
	case "copy":
		migrationID, err := requiredMigrationID(input.migrationID)
		if err != nil {
			return err
		}
		return runBatches(ctx, logger, "copy", func() (int, error) {
			return service.RunCopyBatch(ctx, migrationID)
		})
	case "cutover":
		migrationID, err := requiredMigrationID(input.migrationID)
		if err != nil {
			return err
		}
		if err := service.Cutover(ctx, migrationID, input.retention); err != nil {
			return err
		}
		logger.Info("torrent storage read preference cut over",
			"migration_id", migrationID, "source_retention", input.retention,
		)
		return nil
	case "approve-cleanup":
		migrationID, err := requiredMigrationID(input.migrationID)
		if err != nil {
			return err
		}
		actorID, err := uuid.Parse(input.actorID)
		if err != nil {
			return errors.New("actor-id must be a staff user UUID")
		}
		if err := service.ApproveCleanup(ctx, migrationID, actorID); err != nil {
			return err
		}
		logger.Info("torrent source cleanup approved", "migration_id", migrationID)
		return nil
	case "cleanup":
		migrationID, err := requiredMigrationID(input.migrationID)
		if err != nil {
			return err
		}
		return runBatches(ctx, logger, "cleanup", func() (int, error) {
			return service.RunCleanupBatch(ctx, migrationID)
		})
	default:
		return errors.New("action must be plan, copy, cutover, approve-cleanup, or cleanup")
	}
}

func requiredMigrationID(value string) (uuid.UUID, error) {
	migrationID, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, errors.New("migration-id must be a UUID")
	}
	return migrationID, nil
}

func runBatches(ctx context.Context, logger *slog.Logger, action string, run func() (int, error)) error {
	total := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		processed, err := run()
		total += processed
		if err != nil {
			return err
		}
		if processed == 0 {
			logger.Info("torrent storage batches complete", "action", action, "processed", total)
			return nil
		}
	}
}
