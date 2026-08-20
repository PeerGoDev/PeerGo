package config

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
)

type Config struct {
	Environment         string
	SnapshotPath        string
	SubjectSnapshotPath string
	RuntimePolicyPath   string
	TrustedKeys         map[string]ed25519.PublicKey
	PasskeyLookupKey    []byte
	MaxAge              time.Duration
	SubjectMaxAge       time.Duration
	RuntimePolicyMaxAge time.Duration
	MaxFutureSkew       time.Duration
}

func Load() (Config, error) {
	environment, err := required("PEERGO_ENV")
	if err != nil {
		return Config{}, err
	}
	if environment != "development" && environment != "production" {
		return Config{}, errors.New("PEERGO_ENV must be development or production")
	}
	snapshotPath, err := required("PEERGO_TRACKER_SNAPSHOT_PATH")
	if err != nil {
		return Config{}, err
	}
	if !filepath.IsAbs(snapshotPath) {
		return Config{}, errors.New("PEERGO_TRACKER_SNAPSHOT_PATH must be absolute")
	}
	subjectSnapshotPath, err := required("PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH")
	if err != nil {
		return Config{}, err
	}
	if !filepath.IsAbs(subjectSnapshotPath) {
		return Config{}, errors.New("PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH must be absolute")
	}
	if filepath.Clean(snapshotPath) == filepath.Clean(subjectSnapshotPath) {
		return Config{}, errors.New("Tracker torrent and subject snapshot paths must differ")
	}
	runtimePolicyPath, err := required("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH")
	if err != nil {
		return Config{}, err
	}
	if !filepath.IsAbs(runtimePolicyPath) {
		return Config{}, errors.New("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH must be absolute")
	}
	runtimePolicyPath = filepath.Clean(runtimePolicyPath)
	if runtimePolicyPath == filepath.Clean(snapshotPath) || runtimePolicyPath == filepath.Clean(subjectSnapshotPath) {
		return Config{}, errors.New("Tracker runtime policy snapshot path must be distinct")
	}
	trustedValue, err := required("PEERGO_TRACKER_SNAPSHOT_TRUSTED_KEYS")
	if err != nil {
		return Config{}, err
	}
	trustedKeys, err := signedsnapshotv1.ParseTrustedKeys(trustedValue)
	if err != nil {
		return Config{}, err
	}
	maxAge, err := parseDuration("PEERGO_TRACKER_SNAPSHOT_MAX_AGE", 10*time.Second, time.Hour)
	if err != nil {
		return Config{}, err
	}
	subjectMaxAge, err := parseDuration("PEERGO_TRACKER_SUBJECT_SNAPSHOT_MAX_AGE", time.Second, 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	runtimePolicyMaxAge, err := parseDuration("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_MAX_AGE", 10*time.Second, time.Hour)
	if err != nil {
		return Config{}, err
	}
	maxFutureSkew, err := parseDuration("PEERGO_TRACKER_SNAPSHOT_MAX_FUTURE_SKEW", 0, 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	passkeyLookupKey, err := required("PEERGO_TRACKER_PASSKEY_LOOKUP_KEY")
	if err != nil {
		return Config{}, err
	}
	if len([]byte(passkeyLookupKey)) < 32 {
		return Config{}, errors.New("PEERGO_TRACKER_PASSKEY_LOOKUP_KEY must contain at least 32 bytes")
	}
	return Config{
		Environment: environment, SnapshotPath: filepath.Clean(snapshotPath),
		SubjectSnapshotPath: filepath.Clean(subjectSnapshotPath), RuntimePolicyPath: runtimePolicyPath,
		TrustedKeys:      trustedKeys,
		PasskeyLookupKey: []byte(passkeyLookupKey), MaxAge: maxAge,
		SubjectMaxAge: subjectMaxAge, RuntimePolicyMaxAge: runtimePolicyMaxAge, MaxFutureSkew: maxFutureSkew,
	}, nil
}

func parseDuration(name string, minimum, maximum time.Duration) (time.Duration, error) {
	value, err := required(name)
	if err != nil {
		return 0, err
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %s and %s", name, minimum, maximum)
	}
	return parsed, nil
}

func required(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}
