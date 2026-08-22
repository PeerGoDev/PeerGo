// Command seeding-reward-compensation-preview performs a read-only rebuild of
// historical v1 reward hours that received credible announce intervals after
// closure. It writes a private, deterministic approval artifact and never
// changes evidence, balances, experience or current projections.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

type commandOutput struct {
	ArtifactPath string `json:"artifact_path"`
	seedingreward.CompensationPreviewSummary
}

func main() {
	outputPath := flag.String("output", "", "new operator-only JSONL artifact path")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if flag.NArg() != 0 {
		fail(logger, "invalid compensation preview command", errors.New("positional arguments are not accepted"))
	}
	cleanOutput, err := validateOutputPath(*outputPath)
	if err != nil {
		fail(logger, "invalid compensation preview output", err)
	}
	coreURL := strings.TrimSpace(os.Getenv("PEERGO_CORE_DATABASE_URL"))
	trackerURL := strings.TrimSpace(os.Getenv("PEERGO_TRACKER_DATABASE_URL"))
	if coreURL == "" || trackerURL == "" || coreURL == trackerURL {
		fail(logger, "invalid compensation preview databases", errors.New("distinct Core and Tracker database URLs are required"))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	core, err := openReadOnlyPool(startupCtx, coreURL, "peergo-seeding-compensation-preview-core")
	if err != nil {
		fail(logger, "open compensation Core snapshot", err)
	}
	defer core.Close()
	tracker, err := openReadOnlyPool(startupCtx, trackerURL, "peergo-seeding-compensation-preview-tracker")
	if err != nil {
		fail(logger, "open compensation Tracker snapshot", err)
	}
	defer tracker.Close()
	if err := platformpostgres.RequireCurrentMigration(startupCtx, core); err != nil {
		fail(logger, "Core database is not ready", err)
	}

	repository, err := seedingreward.NewPostgresSettlementRepository(core)
	if err != nil {
		fail(logger, "compose compensation preview", err)
	}
	result, err := buildArtifact(ctx, logger, repository, tracker, cleanOutput)
	if err != nil {
		fail(logger, "build compensation preview", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fail(logger, "write compensation preview summary", err)
	}
}

func buildArtifact(
	ctx context.Context,
	logger *slog.Logger,
	repository *seedingreward.PostgresSettlementRepository,
	tracker *pgxpool.Pool,
	outputPath string,
) (commandOutput, error) {
	artifact, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return commandOutput{}, fmt.Errorf("create compensation preview artifact: %w", err)
	}
	keepArtifact := false
	defer func() {
		_ = artifact.Close()
		if !keepArtifact {
			_ = os.Remove(outputPath)
		}
	}()
	hasher := sha256.New()
	progress := func(item seedingreward.CompensationPreviewProgress) {
		logger.Info("seeding reward compensation preview progress",
			"window", item.WindowStart.Format(time.RFC3339),
			"window_index", item.WindowIndex, "window_count", item.WindowCount,
			"affected_user_hours", item.AffectedUserHours,
			"positive_corrections", item.PositiveCorrections,
		)
	}
	summary, err := repository.PreviewHistoricalCompensation(ctx, tracker, io.MultiWriter(artifact, hasher), progress)
	if err != nil {
		return commandOutput{}, err
	}
	if err := artifact.Sync(); err != nil {
		return commandOutput{}, fmt.Errorf("sync compensation preview artifact: %w", err)
	}
	if err := artifact.Close(); err != nil {
		return commandOutput{}, fmt.Errorf("close compensation preview artifact: %w", err)
	}
	summary.ArtifactSHA256 = hex.EncodeToString(hasher.Sum(nil))
	keepArtifact = true
	return commandOutput{ArtifactPath: outputPath, CompensationPreviewSummary: summary}, nil
}

func validateOutputPath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed != raw || !filepath.IsAbs(trimmed) || filepath.Ext(trimmed) != ".jsonl" {
		return "", errors.New("--output must be an absolute .jsonl path")
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned != trimmed || filepath.Base(cleaned) == ".jsonl" {
		return "", errors.New("--output must be a clean, named .jsonl path")
	}
	info, err := os.Stat(filepath.Dir(cleaned))
	if err != nil || !info.IsDir() {
		return "", errors.New("--output parent directory must already exist")
	}
	return cleaned, nil
}

func openReadOnlyPool(ctx context.Context, databaseURL, applicationName string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("database URL is invalid")
	}
	config.MaxConns = 2
	config.MinConns = 0
	config.ConnConfig.RuntimeParams["application_name"] = applicationName
	config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func fail(logger *slog.Logger, message string, err error) {
	logger.Error(message, "error", fmt.Sprint(err))
	os.Exit(1)
}
