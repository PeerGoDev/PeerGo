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
		logger.Error("Core H&R consumer provisioning failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadHNRConsumerProvisioner()
	if err != nil {
		return fmt.Errorf("load Core H&R consumer provisioner configuration: %w", err)
	}
	connection, js, err := trafficconsumer.Connect(settings.NATS, "peergo-core-hnr-consumer-provisioner", logger)
	if err != nil {
		return fmt.Errorf("connect Core H&R JetStream provisioner: %w", err)
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
	logger.Info("Core H&R consumer verified", "stream", settings.Stream, "durable", settings.Consumer.Durable, "created", created)
	return nil
}
