package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/peergo/peergo/services/settlement/internal/config"
	"github.com/peergo/peergo/services/settlement/internal/jetstreamconsumer"
	"github.com/peergo/peergo/services/settlement/internal/seedingoutbox"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Settlement seeding evidence stream provisioning failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadSeedingEvidenceStreamProvisioner()
	if err != nil {
		return fmt.Errorf("load Settlement seeding evidence stream provisioner configuration: %w", err)
	}
	connection, js, err := jetstreamconsumer.Connect(settings.NATS, "peergo-settlement-seeding-evidence-stream-provisioner", logger)
	if err != nil {
		return fmt.Errorf("connect Settlement seeding evidence JetStream provisioner: %w", err)
	}
	defer connection.Close()
	manager, err := seedingoutbox.NewNATSStreamManager(js)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), settings.Timeout)
	defer cancel()
	created, err := seedingoutbox.EnsureStream(ctx, manager, settings.Stream)
	if err != nil {
		return err
	}
	logger.Info("Settlement seeding evidence stream verified", "stream", settings.Stream.Name, "created", created)
	return nil
}
