package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/peergo/peergo/services/settlement/internal/config"
	"github.com/peergo/peergo/services/settlement/internal/jetstreamconsumer"
	"github.com/peergo/peergo/services/settlement/internal/jetstreamprovision"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Settlement seeding snapshot consumer provisioning failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadSeedingSnapshotProvisioner()
	if err != nil {
		return fmt.Errorf("load seeding snapshot consumer provisioner: %w", err)
	}
	connection, js, err := jetstreamconsumer.Connect(settings.NATS, "peergo-settlement-seeding-snapshot-provisioner", logger)
	if err != nil {
		return err
	}
	defer connection.Close()
	manager, err := jetstreamprovision.NewNATSManager(js)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), settings.Timeout)
	defer cancel()
	created, err := jetstreamprovision.Ensure(ctx, manager, settings.Stream, settings.Consumer)
	if err != nil {
		return err
	}
	logger.Info("Settlement seeding snapshot durable verified",
		"stream", settings.Stream, "consumer", settings.Consumer.Name, "created", created)
	return nil
}
