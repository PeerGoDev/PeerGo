package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/legacyuserstate"
)

type settings struct {
	SourceDatabaseURL string
	CoreDatabaseURL   string
	RunID             uuid.UUID
	OccurredAt        time.Time
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config, err := loadSettings()
	if err != nil {
		logger.Error("invalid legacy user state migration configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	source, err := openPool(ctx, config.SourceDatabaseURL, true, "peergo-legacy-user-state-source")
	if err != nil {
		logger.Error("open read-only PtYes source", "error", err)
		os.Exit(1)
	}
	defer source.Close()
	core, err := openPool(ctx, config.CoreDatabaseURL, false, "peergo-legacy-user-state-core")
	if err != nil {
		logger.Error("open PeerGo Core database", "error", err)
		os.Exit(1)
	}
	defer core.Close()

	importer, err := legacyuserstate.NewImporter(
		source,
		core,
		legacyuserstate.Config{RunID: config.RunID, OccurredAt: config.OccurredAt},
		func(progress legacyuserstate.Progress) {
			logger.Info(
				"legacy user state migration progress",
				"processed", progress.Processed,
				"expected", progress.Expected,
			)
		},
	)
	if err != nil {
		logger.Error("compose legacy user state importer", "error", err)
		os.Exit(1)
	}
	result, err := importer.Run(ctx)
	if err != nil {
		logger.Error("legacy user state migration failed", "error", err)
		os.Exit(1)
	}
	logger.Info(
		"legacy user state migration completed",
		"run_id", result.RunID,
		"users", result.Users,
		"new_evidence", result.ImportedEvidence,
		"new_status_evidence", result.ImportedStatusEvidence,
		"new_attendance_evidence", result.ImportedAttendanceEvidence,
		"integer_magic_total", result.IntegerMagicTotal,
		"exact_magic_total", result.ExactMagicTotal,
		"rounding_delta_total", result.RoundingDeltaTotal,
		"raw_uploaded_total", result.RawUploadedTotal,
		"raw_downloaded_total", result.RawDownloadedTotal,
		"attendance_total_days", result.AttendanceTotalDays,
		"attendance_retroactive_cards", result.AttendanceRetroactiveCards,
	)
}

func loadSettings() (settings, error) {
	var result settings
	var err error
	result.SourceDatabaseURL, err = required("PEERGO_LEGACY_SOURCE_DATABASE_URL")
	if err != nil {
		return settings{}, err
	}
	result.CoreDatabaseURL, err = required("PEERGO_CORE_DATABASE_URL")
	if err != nil {
		return settings{}, err
	}
	if result.SourceDatabaseURL == result.CoreDatabaseURL {
		return settings{}, errors.New("source and Core database URLs must be distinct")
	}
	result.RunID, err = uuid.Parse(strings.TrimSpace(os.Getenv("PEERGO_LEGACY_RUN_ID")))
	if err != nil || result.RunID == uuid.Nil {
		return settings{}, errors.New("PEERGO_LEGACY_RUN_ID must be a non-zero UUID")
	}
	result.OccurredAt, err = time.Parse(
		time.RFC3339Nano,
		strings.TrimSpace(os.Getenv("PEERGO_LEGACY_OCCURRED_AT")),
	)
	if err != nil || result.OccurredAt.IsZero() {
		return settings{}, errors.New("PEERGO_LEGACY_OCCURRED_AT must be a fixed RFC3339 timestamp")
	}
	return result, nil
}

func required(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", errors.New(name + " is required")
	}
	return value, nil
}

func openPool(ctx context.Context, databaseURL string, readOnly bool, applicationName string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("database URL is invalid")
	}
	config.MaxConns = 4
	config.MinConns = 0
	config.ConnConfig.RuntimeParams["application_name"] = applicationName
	if readOnly {
		config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(probeCtx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
