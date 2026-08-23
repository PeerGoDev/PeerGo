package config

import (
	"time"

	"github.com/peergo/peergo/services/core/internal/trafficcleanup"
)

type CoreStorageCleanupConfig struct {
	CoreDatabaseProcessConfig
	RunInterval      time.Duration
	DetailRetention  time.Duration
	HistoryRetention time.Duration
	StartupTimeout   time.Duration
	BatchSize        int
}

func LoadCoreStorageCleanup() (CoreStorageCleanupConfig, error) {
	database, err := LoadCoreDatabaseProcess()
	if err != nil {
		return CoreStorageCleanupConfig{}, err
	}
	historyRetention, err := projectionDuration(
		"PEERGO_CORE_STORAGE_HISTORY_RETENTION", trafficcleanup.MinimumHistoryRetention, 365*24*time.Hour,
	)
	if err != nil {
		return CoreStorageCleanupConfig{}, err
	}
	runInterval, err := projectionDuration("PEERGO_CORE_STORAGE_CLEANUP_INTERVAL", 10*time.Second, time.Hour)
	if err != nil {
		return CoreStorageCleanupConfig{}, err
	}
	detailRetention, err := projectionDuration(
		"PEERGO_CORE_STORAGE_DETAIL_RETENTION", trafficcleanup.MinimumDetailRetention, 7*24*time.Hour,
	)
	if err != nil {
		return CoreStorageCleanupConfig{}, err
	}
	startupTimeout, err := projectionDuration("PEERGO_CORE_STORAGE_STARTUP_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return CoreStorageCleanupConfig{}, err
	}
	batchSize, err := projectionInteger("PEERGO_CORE_STORAGE_BATCH_SIZE", 100, 10_000)
	if err != nil {
		return CoreStorageCleanupConfig{}, err
	}
	return CoreStorageCleanupConfig{
		CoreDatabaseProcessConfig: database,
		RunInterval:               runInterval, DetailRetention: detailRetention, HistoryRetention: historyRetention,
		StartupTimeout: startupTimeout, BatchSize: batchSize,
	}, nil
}
