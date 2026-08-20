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

	"github.com/peergo/peergo/services/core/internal/legacymedals"
)

type settings struct {
	SourceDatabaseURL string
	CoreDatabaseURL   string
	RunID             uuid.UUID
	OccurredAt        time.Time
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	action := flag.String("action", "import", "import or verify")
	flag.Parse()
	if *action != "import" && *action != "verify" {
		logger.Error("invalid legacy medal action", "action", *action)
		os.Exit(2)
	}
	config, err := loadSettings()
	if err != nil {
		logger.Error("invalid legacy medal migration configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	source, err := openPool(ctx, config.SourceDatabaseURL, true, "peergo-legacy-medals-source")
	if err != nil {
		logger.Error("open read-only PtYes source", "error", err)
		os.Exit(1)
	}
	defer source.Close()
	core, err := openPool(ctx, config.CoreDatabaseURL, false, "peergo-legacy-medals-core")
	if err != nil {
		logger.Error("open PeerGo Core database", "error", err)
		os.Exit(1)
	}
	defer core.Close()

	importer, err := legacymedals.NewImporter(
		source,
		core,
		legacymedals.Config{RunID: config.RunID, OccurredAt: config.OccurredAt},
		func(progress legacymedals.Progress) {
			logger.Info("legacy medal migration progress", "phase", progress.Phase, "processed", progress.Processed, "expected", progress.Expected)
		},
	)
	if err != nil {
		logger.Error("compose legacy medal importer", "error", err)
		os.Exit(1)
	}
	var result legacymedals.Result
	if *action == "verify" {
		result, err = importer.Verify(ctx)
	} else {
		result, err = importer.Run(ctx)
	}
	if err != nil {
		logger.Error("legacy medal migration failed", "error", err)
		os.Exit(1)
	}
	logger.Info(
		"legacy medal migration completed",
		"action", *action,
		"run_id", result.RunID,
		"definitions", result.Definitions,
		"user_medals", result.UserMedals,
		"wearing", result.Wearing,
		"expired_at_cutover", result.Expired,
		"benefit_users", result.BenefitUsers,
		"positive_benefit_users", result.PositiveBenefitUsers,
		"maximum_magic_bonus_bps", result.MaximumMagicBonusBPS,
		"workgroup_memberships", result.WorkgroupMemberships,
		"reseed_memberships", result.ReseedMemberships,
		"review_memberships", result.ReviewMemberships,
		"retention_memberships", result.RetentionMemberships,
		"new_definition_evidence", result.ImportedDefinitionRows,
		"new_user_medal_evidence", result.ImportedUserMedalRows,
		"new_benefit_evidence", result.ImportedBenefitRows,
		"new_workgroup_evidence", result.ImportedWorkgroupRows,
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
	result.OccurredAt, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(os.Getenv("PEERGO_LEGACY_OCCURRED_AT")))
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
