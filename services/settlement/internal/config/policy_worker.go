package config

import (
	"errors"
	"fmt"
	"time"
)

type TrackerLedgerProcessConfig struct {
	Environment string
	DatabaseURL string
}

type PolicyWorkerConfig struct {
	TrackerLedgerProcessConfig
	LeaseDuration  time.Duration
	IdleInterval   time.Duration
	RetryBase      time.Duration
	StartupTimeout time.Duration
}

// HNRWorkerConfig reuses the same bounded database-worker contract. Separate
// environment variable prefixes keep the two processes independently tunable
// without introducing a second near-identical configuration type.
type HNRWorkerConfig = PolicyWorkerConfig

// LoadTrackerLedgerProcess is shared by narrow, database-only Settlement
// commands. It intentionally exposes neither Tracker announce credentials nor
// JetStream management credentials.
func LoadTrackerLedgerProcess() (TrackerLedgerProcessConfig, error) {
	environment, err := settlementEnvironment()
	if err != nil {
		return TrackerLedgerProcessConfig{}, err
	}
	databaseURL, err := required("PEERGO_TRACKER_DATABASE_URL")
	if err != nil || validateDatabaseURL(databaseURL, environment) != nil {
		return TrackerLedgerProcessConfig{}, errors.New("PEERGO_TRACKER_DATABASE_URL must be a PostgreSQL connection URL with a database name")
	}
	return TrackerLedgerProcessConfig{Environment: environment, DatabaseURL: databaseURL}, nil
}

func LoadPolicyWorker() (PolicyWorkerConfig, error) {
	return loadLedgerWorker("PEERGO_SETTLEMENT_POLICY")
}

func LoadHNRWorker() (HNRWorkerConfig, error) {
	return loadLedgerWorker("PEERGO_SETTLEMENT_HNR")
}

func loadLedgerWorker(prefix string) (PolicyWorkerConfig, error) {
	database, err := LoadTrackerLedgerProcess()
	if err != nil {
		return PolicyWorkerConfig{}, err
	}
	leaseDuration, err := duration(prefix+"_LEASE_DURATION", time.Second, 10*time.Minute)
	if err != nil {
		return PolicyWorkerConfig{}, err
	}
	idleInterval, err := duration(prefix+"_IDLE_INTERVAL", 50*time.Millisecond, time.Minute)
	if err != nil {
		return PolicyWorkerConfig{}, err
	}
	retryBase, err := duration(prefix+"_RETRY_BASE", 100*time.Millisecond, time.Minute)
	if err != nil {
		return PolicyWorkerConfig{}, err
	}
	startupTimeout, err := duration(prefix+"_STARTUP_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return PolicyWorkerConfig{}, err
	}
	return PolicyWorkerConfig{
		TrackerLedgerProcessConfig: database, LeaseDuration: leaseDuration, IdleInterval: idleInterval, RetryBase: retryBase,
		StartupTimeout: startupTimeout,
	}, nil
}

func settlementEnvironment() (string, error) {
	environment, err := required("PEERGO_ENV")
	if err != nil {
		return "", err
	}
	if environment != "development" && environment != "production" {
		return "", fmt.Errorf("PEERGO_ENV must be development or production")
	}
	return environment, nil
}
