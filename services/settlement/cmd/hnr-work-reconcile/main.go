package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/settlement/internal/hnr"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger, os.Args[1:]); err != nil {
		logger.Error("Settlement H&R work reconciliation failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, arguments []string) error {
	flags := flag.NewFlagSet("settlement-hnr-work-reconcile", flag.ContinueOnError)
	batchSize := flags.Int("batch-size", 5_000, "maximum rows committed per transaction")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *batchSize < 1 || *batchSize > 10_000 || flags.NArg() != 0 {
		return errors.New("--batch-size must be between 1 and 10000")
	}
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_TRACKER_DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("PEERGO_TRACKER_DATABASE_URL is required")
	}
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(rootCtx, databaseURL)
	if err != nil {
		return fmt.Errorf("open Tracker Ledger database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(rootCtx); err != nil {
		return fmt.Errorf("ping Tracker Ledger database: %w", err)
	}
	if err := platformpostgres.RequireCurrentMigration(rootCtx, pool); err != nil {
		return err
	}
	repository, err := hnr.NewPostgresRepository(pool)
	if err != nil {
		return err
	}
	var total int64
	for {
		count, err := repository.ReconcileIrrelevant(rootCtx, time.Now(), int32(*batchSize))
		if err != nil {
			return err
		}
		total += count
		if count == 0 {
			logger.Info("Settlement H&R work reconciliation completed", "reconciled", total)
			return nil
		}
		logger.Info("Settlement H&R work reconciliation progress", "batch", count, "reconciled", total)
		if rootCtx.Err() != nil {
			return nil
		}
	}
}
