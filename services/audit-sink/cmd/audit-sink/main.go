package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/peergo/peergo/services/audit-sink/internal/journal"
	"github.com/peergo/peergo/services/audit-sink/internal/platform/config"
	"github.com/peergo/peergo/services/audit-sink/internal/platform/httpserver"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	settings, err := config.Load()
	if err != nil {
		logger.Error("invalid audit sink configuration", "error", err)
		os.Exit(1)
	}
	store, err := journal.Open(settings.JournalPath)
	if err != nil {
		logger.Error("open audit journal", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Error("close audit journal", "error", err)
		}
	}()

	server := &http.Server{
		Addr:              settings.Address,
		Handler:           httpserver.New(store, settings.ServiceToken, time.Now, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		logger.Info("audit sink listening", "address", settings.Address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("audit sink stopped unexpectedly", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("audit sink shutdown failed", "error", err)
		os.Exit(1)
	}
}
