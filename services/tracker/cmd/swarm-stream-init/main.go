package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/peergo/peergo/services/tracker/internal/config"
	"github.com/peergo/peergo/services/tracker/internal/jetstreamprovision"
	"github.com/peergo/peergo/services/tracker/internal/jetstreampublisher"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("Tracker swarm snapshot stream provisioning failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	settings, err := config.LoadSwarmStreamProvisioner()
	if err != nil {
		return fmt.Errorf("load Tracker swarm stream provisioner configuration: %w", err)
	}
	connection, js, err := jetstreampublisher.Connect(jetstreampublisher.ConnectionConfig{
		URLs: settings.NATSURLs, CredentialsFile: settings.NATSCredentialsFile,
		RootCAFile: settings.NATSRootCAFile, ConnectTimeout: settings.NATSConnectTimeout,
		ReconnectWait: settings.NATSReconnectWait,
	}, logger)
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
	created, err := jetstreamprovision.Ensure(ctx, manager, settings.Stream)
	if err != nil {
		return err
	}
	logger.Info("Tracker swarm snapshot stream configuration verified", "stream", settings.Stream.Name, "created", created)
	return nil
}
