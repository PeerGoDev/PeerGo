package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	"github.com/peergo/peergo/services/core/internal/modules/objectmigration"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	"github.com/peergo/peergo/services/core/internal/platform/objectstore"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	action := flag.String("action", "", "plan, copy, retry, cutover, approve-cleanup, or cleanup")
	migrationID := flag.String("migration-id", "", "existing migration UUID; plan generates one when omitted")
	actorID := flag.String("actor-id", "", "staff user UUID for plan or cleanup approval")
	mode := flag.String("mode", string(objectmigration.ModeMove), "plan mode: move or replicate")
	kinds := flag.String("kinds", "all", "all or comma-separated: torrent,torrent_screenshot,avatar,image_derivative")
	retention := flag.Duration("retention", 7*24*time.Hour, "source retention after move cutover; minimum 24h")
	batchSize := flag.Int("batch-size", objectmigration.DefaultBatchSize, "bounded copy or cleanup batch size")
	flag.Parse()
	if *action == "" || *batchSize < 1 || *batchSize > 100 {
		logger.Error("invalid object storage command flags")
		os.Exit(2)
	}

	settings, err := config.LoadObjectMigrationTool()
	if err != nil {
		logger.Error("invalid object storage configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
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
	registry, err := objectstorage.NewRegistry(source, destination)
	if err != nil {
		logger.Error("compose object store registry", "error", err)
		os.Exit(1)
	}
	repository, err := objectmigration.NewPostgresRepository(pool)
	if err != nil {
		logger.Error("compose object migration repository", "error", err)
		os.Exit(1)
	}
	service, err := objectmigration.NewService(repository, registry, objectmigration.ServiceConfig{BatchSize: int32(*batchSize)})
	if err != nil {
		logger.Error("compose object migration service", "error", err)
		os.Exit(1)
	}
	if err := execute(ctx, service, commandInput{
		action: *action, migrationID: *migrationID, actorID: *actorID,
		mode: *mode, kinds: *kinds, retention: *retention,
		sourceBackendID: string(source.BackendID()), destinationBackendID: string(destination.BackendID()),
	}, logger); err != nil {
		logger.Error("object storage command failed", "action", *action, "error", err)
		os.Exit(1)
	}
}

type commandInput struct {
	action, migrationID, actorID, mode, kinds string
	retention                                 time.Duration
	sourceBackendID, destinationBackendID     string
}

func execute(ctx context.Context, service *objectmigration.Service, input commandInput, logger *slog.Logger) error {
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
		kinds, err := parseKinds(input.kinds)
		if err != nil {
			return err
		}
		plan, err := service.Plan(ctx, objectmigration.PlanInput{
			ID: migrationID, Mode: objectmigration.Mode(input.mode), Kinds: kinds,
			SourceBackendID:      objectstorage.BackendID(input.sourceBackendID),
			DestinationBackendID: objectstorage.BackendID(input.destinationBackendID),
			RequestedBy:          actorID, OccurredAt: time.Now(),
		})
		if err != nil {
			return err
		}
		logger.Info("unified object storage migration planned",
			"migration_id", plan.ID, "mode", plan.Mode, "kinds", plan.Kinds,
			"object_count", plan.ObjectCount, "source_backend_id", plan.SourceBackendID,
			"destination_backend_id", plan.DestinationBackendID)
		return nil
	case "copy", "cleanup":
		migrationID, err := requiredMigrationID(input.migrationID)
		if err != nil {
			return err
		}
		if input.action == "copy" {
			return runBatches(ctx, logger, "copy", func() (int, error) { return service.RunCopyBatch(ctx, migrationID) })
		}
		return runBatches(ctx, logger, "cleanup", func() (int, error) { return service.RunCleanupBatch(ctx, migrationID) })
	case "retry":
		migrationID, err := requiredMigrationID(input.migrationID)
		if err != nil {
			return err
		}
		count, err := service.RetryFailures(ctx, migrationID)
		if err == nil {
			logger.Info("object storage failures scheduled for retry", "migration_id", migrationID, "item_count", count)
		}
		return err
	case "cutover":
		migrationID, err := requiredMigrationID(input.migrationID)
		if err != nil {
			return err
		}
		if err := service.Cutover(ctx, migrationID, input.retention); err != nil {
			return err
		}
		logger.Info("object storage read preference cut over", "migration_id", migrationID, "source_retention", input.retention)
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
		logger.Info("object storage source cleanup approved", "migration_id", migrationID)
		return nil
	default:
		return errors.New("action must be plan, copy, retry, cutover, approve-cleanup, or cleanup")
	}
}

func parseKinds(value string) ([]objectmigration.Kind, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "all" {
		return append([]objectmigration.Kind(nil), objectmigration.AllKinds...), nil
	}
	var result []objectmigration.Kind
	seen := make(map[objectmigration.Kind]struct{})
	for _, raw := range strings.Split(value, ",") {
		kind := objectmigration.Kind(strings.TrimSpace(raw))
		if !kind.Valid() {
			return nil, errors.New("kinds contains an unsupported object kind")
		}
		if _, exists := seen[kind]; !exists {
			seen[kind] = struct{}{}
			result = append(result, kind)
		}
	}
	return result, nil
}

func requiredMigrationID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, errors.New("migration-id must be a UUID")
	}
	return id, nil
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
			logger.Info("object storage batches complete", "action", action, "processed", total)
			return nil
		}
	}
}
