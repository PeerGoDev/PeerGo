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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/legacywikis"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	action := flag.String("action", "import", "import or verify")
	flag.Parse()
	if *action != "import" && *action != "verify" {
		logger.Error("unsupported legacy Wiki action", "action", *action)
		os.Exit(2)
	}
	sourceURL := strings.TrimSpace(os.Getenv("PEERGO_LEGACY_SOURCE_DATABASE_URL"))
	coreURL := strings.TrimSpace(os.Getenv("PEERGO_CORE_DATABASE_URL"))
	if sourceURL == "" || coreURL == "" || sourceURL == coreURL {
		logger.Error("distinct PEERGO_LEGACY_SOURCE_DATABASE_URL and PEERGO_CORE_DATABASE_URL are required")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	source, err := openPool(startupCtx, sourceURL, true, "peergo-legacy-wikis-source")
	if err != nil {
		logger.Error("open read-only PtYes Wiki source", "error", err)
		os.Exit(1)
	}
	defer source.Close()
	core, err := openPool(startupCtx, coreURL, *action == "verify", "peergo-legacy-wikis-core")
	if err != nil {
		logger.Error("open PeerGo Wiki destination", "error", err)
		os.Exit(1)
	}
	defer core.Close()
	var result legacywikis.Result
	if *action == "verify" {
		result, err = legacywikis.Verify(ctx, source, core)
	} else {
		result, err = legacywikis.Import(ctx, source, core)
	}
	if err != nil {
		logger.Error("legacy Wiki operation failed", "action", *action, "error", err)
		os.Exit(1)
	}
	logger.Info("legacy Wiki operation completed",
		"action", *action,
		"source_pages", result.SourcePages,
		"imported_pages", result.ImportedPages,
		"existing_pages", result.ExistingPages,
		"verified_pages", result.VerifiedPages,
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
