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
		logger.Error("Core swarm consumer provisioning failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadSwarmConsumerProvisioner()
	if err != nil {
		return fmt.Errorf("load Core swarm consumer provisioner configuration: %w", err)
	}
	connection, js, err := trafficconsumer.Connect(settings.NATS, "peergo-core-swarm-consumer-provisioner", logger)
	if err != nil {
		return fmt.Errorf("connect Core swarm JetStream provisioner: %w", err)
	}
	defer connection.Close()
	manager, err := trafficconsumer.NewNATSConsumerManager(js)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), settings.Timeout)
	defer cancel()
	for _, provision := range settings.Consumers {
		created, ensureErr := trafficconsumer.EnsureConsumer(ctx, manager, provision.Stream, provision.Consumer)
		if ensureErr != nil {
			return ensureErr
		}
		logger.Info("Core swarm consumer verified", "stream", provision.Stream, "durable", provision.Consumer.Durable, "created", created)
	}
	return nil
}
