package config

import (
	"time"

	"github.com/peergo/peergo/services/settlement/internal/storagecleanup"
)

type StorageCleanupConfig struct {
	TrackerLedgerProcessConfig
	RunInterval       time.Duration
	TerminalRetention time.Duration
	SessionRetention  time.Duration
	DetailRetention   time.Duration
	AnomalyRetention  time.Duration
	StartupTimeout    time.Duration
	BatchSize         int
}

func LoadStorageCleanup() (StorageCleanupConfig, error) {
	database, err := LoadTrackerLedgerProcess()
	if err != nil {
		return StorageCleanupConfig{}, err
	}
	runInterval, err := duration("PEERGO_SETTLEMENT_STORAGE_CLEANUP_INTERVAL", 10*time.Second, time.Hour)
	if err != nil {
		return StorageCleanupConfig{}, err
	}
	terminalRetention, err := duration("PEERGO_SETTLEMENT_STORAGE_TERMINAL_RETENTION", storagecleanup.MinimumTerminalRetention, 30*24*time.Hour)
	if err != nil {
		return StorageCleanupConfig{}, err
	}
	sessionRetention, err := duration("PEERGO_SETTLEMENT_STORAGE_SESSION_RETENTION", storagecleanup.MinimumSessionRetention, 30*24*time.Hour)
	if err != nil {
		return StorageCleanupConfig{}, err
	}
	detailRetention, err := duration("PEERGO_SETTLEMENT_STORAGE_DETAIL_RETENTION", storagecleanup.MinimumDetailRetention, 90*24*time.Hour)
	if err != nil {
		return StorageCleanupConfig{}, err
	}
	anomalyRetention, err := duration("PEERGO_SETTLEMENT_STORAGE_ANOMALY_RETENTION", storagecleanup.MinimumAnomalyRetention, 365*24*time.Hour)
	if err != nil {
		return StorageCleanupConfig{}, err
	}
	startupTimeout, err := duration("PEERGO_SETTLEMENT_STORAGE_STARTUP_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return StorageCleanupConfig{}, err
	}
	batchSize, err := integer("PEERGO_SETTLEMENT_STORAGE_BATCH_SIZE", 100, 10_000)
	if err != nil {
		return StorageCleanupConfig{}, err
	}
	return StorageCleanupConfig{
		TrackerLedgerProcessConfig: database,
		RunInterval:                runInterval,
		TerminalRetention:          terminalRetention,
		SessionRetention:           sessionRetention,
		DetailRetention:            detailRetention,
		AnomalyRetention:           anomalyRetention,
		StartupTimeout:             startupTimeout,
		BatchSize:                  batchSize,
	}, nil
}
