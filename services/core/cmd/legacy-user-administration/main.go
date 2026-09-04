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

	"github.com/peergo/peergo/services/core/internal/legacyuseradmin"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sourceURL := strings.TrimSpace(os.Getenv("PEERGO_LEGACY_SOURCE_DATABASE_URL"))
	coreURL := strings.TrimSpace(os.Getenv("PEERGO_CORE_DATABASE_URL"))
	runID, runErr := uuid.Parse(strings.TrimSpace(os.Getenv("PEERGO_LEGACY_RUN_ID")))
	if sourceURL == "" || coreURL == "" || sourceURL == coreURL || runErr != nil || runID == uuid.Nil {
		logger.Error("invalid legacy user administration migration configuration")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	source, err := openPool(ctx, sourceURL, true, "peergo-legacy-user-admin-source")
	if err != nil {
		logger.Error("open PtYes source", "error", err)
		os.Exit(1)
	}
	defer source.Close()
	core, err := openPool(ctx, coreURL, false, "peergo-legacy-user-admin-core")
	if err != nil {
		logger.Error("open Core target", "error", err)
		os.Exit(1)
	}
	defer core.Close()
	result, err := legacyuseradmin.Import(ctx, source, core, legacyuseradmin.Config{
		RunID: runID, ImportedAt: time.Now(),
	})
	if err != nil {
		logger.Error("legacy user administration migration failed", "error", err)
		os.Exit(1)
	}
	logger.Info("legacy user administration migration completed",
		"run_id", result.RunID,
		"observed_at", result.ObservedAt,
		"donation_users", result.DonationSourceRows,
		"positive_donors", result.PositiveDonors,
		"donation_total", result.DonationTotal,
		"network_source_rows", result.NetworkSourceRows,
		"retained_network_rows", result.RetainedNetworkRows,
		"duplicate", result.Duplicate)
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
