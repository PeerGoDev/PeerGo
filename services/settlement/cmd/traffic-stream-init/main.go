package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/peergo/peergo/services/settlement/internal/config"
	"github.com/peergo/peergo/services/settlement/internal/jetstreamconsumer"
	"github.com/peergo/peergo/services/settlement/internal/trafficoutbox"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Settlement traffic stream provisioning failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadTrafficStreamProvisioner()
	if err != nil {
		return fmt.Errorf("load Settlement traffic stream provisioner configuration: %w", err)
	}
	connection, js, err := jetstreamconsumer.Connect(settings.NATS, "peergo-settlement-traffic-stream-provisioner", logger)
	if err != nil {
		return fmt.Errorf("connect Settlement traffic JetStream provisioner: %w", err)
	}
	defer connection.Close()
	manager, err := trafficoutbox.NewNATSStreamManager(js)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), settings.Timeout)
	defer cancel()
	created, err := trafficoutbox.EnsureStream(ctx, manager, settings.Stream)
	if err != nil {
		return err
	}
	logger.Info("Settlement traffic stream verified", "stream", settings.Stream.Name, "created", created)
	return nil
}
