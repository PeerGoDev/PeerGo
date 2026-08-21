package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
	"github.com/peergo/peergo/services/core/internal/platform/trackersnapshot"
)

const publicationTimeout = 30 * time.Second

type snapshotBuilders struct {
	control *trackercontrol.SnapshotBuilder
	subject *trackercontrol.SubjectSnapshotBuilder
	policy  *trackercontrol.RuntimePolicySnapshotBuilder
	logger  *slog.Logger
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	settings, err := config.LoadTrackerSnapshotBuilder()
	if err != nil {
		logger.Error("invalid Tracker snapshot builder configuration", "error", err)
		os.Exit(1)
	}
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancelStartup := context.WithTimeout(rootCtx, publicationTimeout)
	pool, err := pgxpool.New(startupCtx, settings.DatabaseURL)
	if err == nil {
		err = pool.Ping(startupCtx)
	}
	if err == nil {
		err = platformpostgres.RequireCurrentMigration(startupCtx, pool)
	}
	cancelStartup()
	if err != nil {
		logger.Error("Core database is not ready", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	builders, err := composeSnapshotBuilders(pool, settings, logger)
	if err != nil {
		logger.Error("compose Tracker snapshot publishers", "error", err)
		os.Exit(1)
	}
	if settings.PublishInterval == 0 {
		ctx, cancel := context.WithTimeout(rootCtx, publicationTimeout)
		err := builders.publish(ctx)
		cancel()
		if err != nil {
			logger.Error("publish Tracker snapshots", "error", err)
			os.Exit(1)
		}
		return
	}

	// A recurring publisher keeps the short-lived subject view fresh and also
	// activates newly issued runtime policies without restarting Tracker. Each
	// cycle remains bounded; a transient failure is logged and retried instead
	// of terminating the only local/production publisher.
	logger.Info("Tracker snapshot publisher started", "interval", settings.PublishInterval)
	publishOnce := func() {
		ctx, cancel := context.WithTimeout(rootCtx, publicationTimeout)
		defer cancel()
		if err := builders.publish(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("publish Tracker snapshots", "error", err)
		}
	}
	publishOnce()
	ticker := time.NewTicker(settings.PublishInterval)
	defer ticker.Stop()
	for {
		select {
		case <-rootCtx.Done():
			logger.Info("Tracker snapshot publisher stopped")
			return
		case <-ticker.C:
			publishOnce()
		}
	}
}

func composeSnapshotBuilders(pool *pgxpool.Pool, settings config.TrackerSnapshotBuilderProcessConfig, logger *slog.Logger) (snapshotBuilders, error) {
	repository, err := trackercontrol.NewPostgresRepository(pool)
	if err != nil {
		return snapshotBuilders{}, err
	}
	controlPublisher, err := trackersnapshot.NewFilesystemPublisher(settings.SnapshotPath)
	if err != nil {
		return snapshotBuilders{}, err
	}
	controlBuilder, err := trackercontrol.NewSnapshotBuilder(repository, controlPublisher, settings.KeyID, settings.SigningKey, time.Now)
	if err != nil {
		return snapshotBuilders{}, err
	}
	subjectPublisher, err := trackersnapshot.NewSubjectFilesystemPublisher(settings.SubjectSnapshotPath)
	if err != nil {
		return snapshotBuilders{}, err
	}
	subjectBuilder, err := trackercontrol.NewSubjectSnapshotBuilder(repository, subjectPublisher, settings.KeyID, settings.SigningKey, time.Now)
	if err != nil {
		return snapshotBuilders{}, err
	}
	policyRepository, err := trackercontrol.NewPostgresRuntimePolicyRepository(pool)
	if err != nil {
		return snapshotBuilders{}, err
	}
	policyPublisher, err := trackersnapshot.NewRuntimePolicyFilesystemPublisher(settings.RuntimePolicyPath)
	if err != nil {
		return snapshotBuilders{}, err
	}
	policyBuilder, err := trackercontrol.NewRuntimePolicySnapshotBuilder(policyRepository, policyPublisher, settings.KeyID, settings.SigningKey, time.Now)
	if err != nil {
		return snapshotBuilders{}, err
	}
	return snapshotBuilders{control: controlBuilder, subject: subjectBuilder, policy: policyBuilder, logger: logger}, nil
}

func (builders snapshotBuilders) publish(ctx context.Context) error {
	controlResult, err := builders.control.BuildAndPublish(ctx)
	if err != nil {
		return fmt.Errorf("build Tracker control snapshot: %w", err)
	}
	builders.logger.Info("Tracker control snapshot ready",
		"control_sequence", controlResult.ControlSequence, "completion_sequence", controlResult.CompletionSequence,
		"torrent_count", controlResult.TorrentCount,
		"generated_at", controlResult.GeneratedAt, "state_sha256", controlResult.StateSHA256,
		"artifact_sha256", hex.EncodeToString(controlResult.ArtifactSHA256[:]), "published", controlResult.Published,
	)
	subjectResult, err := builders.subject.BuildAndPublish(ctx)
	if err != nil {
		return fmt.Errorf("build Tracker subject control snapshot: %w", err)
	}
	builders.logger.Info("Tracker subject control snapshot ready",
		"control_sequence", subjectResult.ControlSequence, "subject_count", subjectResult.SubjectCount,
		"generated_at", subjectResult.GeneratedAt, "state_sha256", subjectResult.StateSHA256,
		"artifact_sha256", hex.EncodeToString(subjectResult.ArtifactSHA256[:]), "published", subjectResult.Published,
	)
	policyResult, err := builders.policy.BuildAndPublish(ctx)
	if err != nil {
		return fmt.Errorf("build Tracker runtime policy snapshot: %w", err)
	}
	builders.logger.Info("Tracker runtime policy snapshot ready",
		"control_sequence", policyResult.ControlSequence, "revision", policyResult.Revision,
		"generated_at", policyResult.GeneratedAt, "state_sha256", policyResult.StateSHA256,
		"artifact_sha256", hex.EncodeToString(policyResult.ArtifactSHA256[:]), "published", policyResult.Published,
	)
	return nil
}
