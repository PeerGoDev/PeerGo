package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/peergo/peergo/services/core/internal/platform/config"
	"github.com/peergo/peergo/services/core/internal/trafficconsumer"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Core seeding evidence consumer provisioning failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadSeedingEvidenceConsumerProvisioner()
	if err != nil {
		return fmt.Errorf("load Core seeding evidence consumer provisioner configuration: %w", err)
	}
	connection, js, err := trafficconsumer.Connect(settings.NATS, "peergo-core-seeding-evidence-consumer-provisioner", logger)
	if err != nil {
		return fmt.Errorf("connect Core seeding evidence JetStream provisioner: %w", err)
	}
	defer connection.Close()
	manager, err := trafficconsumer.NewNATSConsumerManager(js)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), settings.Timeout)
	defer cancel()
	created, err := trafficconsumer.EnsureConsumer(ctx, manager, settings.Stream, settings.Consumer)
	if err != nil {
		return err
	}
	logger.Info("Core seeding evidence consumer verified", "stream", settings.Stream, "durable", settings.Consumer.Durable, "created", created)
	return nil
}
