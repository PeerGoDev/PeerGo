package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ImageDerivativeWorkerConfig struct {
	Environment   string
	DatabaseURL   string
	Store         ObjectStoreConfig
	VipsBinary    string
	TempDir       string
	PollInterval  time.Duration
	LeaseDuration time.Duration
}

// LoadImageDerivativeWorker deliberately reuses the same storage configuration
// contract as torrent uploads. A worker may read and write image bytes but does
// not receive Vault, Tracker, NATS or audit-service credentials.
func LoadImageDerivativeWorker() (ImageDerivativeWorkerConfig, error) {
	storage, err := LoadTorrentUploadStorageTool()
	if err != nil {
		return ImageDerivativeWorkerConfig{}, err
	}
	tempDir, err := required("PEERGO_IMAGE_DERIVATIVE_TEMP_DIR")
	if err != nil || !filepath.IsAbs(tempDir) {
		return ImageDerivativeWorkerConfig{}, errors.New("PEERGO_IMAGE_DERIVATIVE_TEMP_DIR must be absolute")
	}
	info, err := os.Stat(tempDir)
	if err != nil || !info.IsDir() {
		return ImageDerivativeWorkerConfig{}, errors.New("PEERGO_IMAGE_DERIVATIVE_TEMP_DIR must be an existing directory")
	}
	pollInterval, err := imageWorkerDuration("PEERGO_IMAGE_DERIVATIVE_POLL_INTERVAL", 2*time.Second, 100*time.Millisecond, time.Minute)
	if err != nil {
		return ImageDerivativeWorkerConfig{}, err
	}
	leaseDuration, err := imageWorkerDuration("PEERGO_IMAGE_DERIVATIVE_LEASE_DURATION", 5*time.Minute, time.Second, 10*time.Minute)
	if err != nil {
		return ImageDerivativeWorkerConfig{}, err
	}
	return ImageDerivativeWorkerConfig{
		Environment: storage.Environment,
		DatabaseURL: storage.DatabaseURL,
		Store:       storage.Store,
		VipsBinary:  strings.TrimSpace(os.Getenv("PEERGO_IMAGE_DERIVATIVE_VIPS_BINARY")),
		TempDir:     filepath.Clean(tempDir), PollInterval: pollInterval, LeaseDuration: leaseDuration,
	}, nil
}

func imageWorkerDuration(name string, fallback, minimum, maximum time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, errors.New(name + " is outside the supported duration range")
	}
	return value, nil
}
