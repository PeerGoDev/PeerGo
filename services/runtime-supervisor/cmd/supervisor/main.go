package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/peergo/peergo/services/runtime-supervisor/internal/supervisor"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) != 2 {
		logger.Error("usage: peergo-runtime-supervisor <api|worker>")
		os.Exit(2)
	}
	mode, err := supervisor.ParseMode(os.Args[1])
	if err != nil {
		logger.Error("invalid PeerGo runtime mode", "error", err)
		os.Exit(2)
	}
	components, err := supervisor.Components(mode)
	if err != nil {
		logger.Error("compose PeerGo runtime manifest", "error", err)
		os.Exit(1)
	}
	options, err := supervisor.OptionsFromEnvironment()
	if err != nil {
		logger.Error("invalid PeerGo supervisor configuration", "error", err)
		os.Exit(1)
	}
	runner, err := supervisor.New(mode, components, options, logger.With("runtime", mode))
	if err != nil {
		logger.Error("compose PeerGo runtime supervisor", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("PeerGo compact runtime starting", "runtime", mode, "components", len(components), "health_address", options.HealthAddress)
	if err := runner.Run(ctx); err != nil {
		logger.Error("PeerGo compact runtime stopped unexpectedly", "runtime", mode, "error", err)
		os.Exit(1)
	}
	logger.Info("PeerGo compact runtime stopped", "runtime", mode)
}
