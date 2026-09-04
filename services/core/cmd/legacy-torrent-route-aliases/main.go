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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/legacyroutealiases"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	sourceURL := strings.TrimSpace(os.Getenv("PEERGO_LEGACY_SOURCE_DATABASE_URL"))
	coreURL := strings.TrimSpace(os.Getenv("PEERGO_CORE_DATABASE_URL"))
	if sourceURL == "" || coreURL == "" || sourceURL == coreURL {
		logger.Error("distinct legacy source and Core database URLs are required")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	source, err := openPool(startupCtx, sourceURL, true, "peergo-legacy-torrent-route-alias-source")
	if err != nil {
		logger.Error("open read-only PtYes torrent alias source", "error", err)
		os.Exit(1)
	}
	defer source.Close()
	core, err := openPool(startupCtx, coreURL, false, "peergo-legacy-torrent-route-alias-core")
	if err != nil {
		logger.Error("open PeerGo torrent alias destination", "error", err)
		os.Exit(1)
	}
	defer core.Close()

	result, err := legacyroutealiases.Backfill(ctx, source, core, time.Now())
	if err != nil {
		logger.Error("legacy torrent route alias backfill failed", "error", err)
		os.Exit(1)
	}
	logger.Info("legacy torrent route alias backfill completed",
		"source_rows", result.SourceRows,
		"mapped_rows", result.MappedRows,
		"inserted_rows", result.InsertedRows,
		"alias_rows", result.AliasRows,
	)
}

func openPool(ctx context.Context, databaseURL string, readOnly bool, applicationName string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("database URL is invalid")
	}
	config.MaxConns = 2
	config.MinConns = 0
	config.ConnConfig.RuntimeParams["application_name"] = applicationName
	if readOnly {
		config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	}
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
