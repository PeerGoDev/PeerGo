package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/peergo/peergo/services/settlement/internal/config"
	"github.com/peergo/peergo/services/settlement/internal/jetstreamconsumer"
	"github.com/peergo/peergo/services/settlement/internal/resultstream"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Settlement H&R stream provisioning failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadHNRStreamProvisioner()
	if err != nil {
		return fmt.Errorf("load Settlement H&R stream provisioner configuration: %w", err)
	}
	connection, js, err := jetstreamconsumer.Connect(settings.NATS, "peergo-settlement-hnr-stream-provisioner", logger)
	if err != nil {
		return fmt.Errorf("connect Settlement H&R JetStream provisioner: %w", err)
	}
	defer connection.Close()
	manager, err := resultstream.NewNATSManager(js)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), settings.Timeout)
	defer cancel()
	created, err := resultstream.Ensure(ctx, manager, settings.Stream)
	if err != nil {
		return err
	}
	logger.Info("Settlement H&R stream verified", "stream", settings.Stream.Name, "created", created)
	return nil
}
