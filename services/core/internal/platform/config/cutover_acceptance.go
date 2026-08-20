package config

import (
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"time"

	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
)

type CutoverAcceptanceProcessConfig struct {
	Environment         string
	SnapshotPath        string
	SubjectSnapshotPath string
	RuntimePolicyPath   string
	TrustedKeys         map[string]ed25519.PublicKey
	SnapshotMaxAge      time.Duration
	SubjectMaxAge       time.Duration
	RuntimePolicyMaxAge time.Duration
	MaxFutureSkew       time.Duration
}

// LoadCutoverAcceptance gives the finite verifier public keys and artifact
// paths only. It never loads the snapshot signing key or Tracker passkey key.
func LoadCutoverAcceptance() (CutoverAcceptanceProcessConfig, error) {
	environment, err := loadCoreEnvironment()
	if err != nil {
		return CutoverAcceptanceProcessConfig{}, err
	}
	snapshotPath, err := required("PEERGO_TRACKER_SNAPSHOT_PATH")
	if err != nil || !filepath.IsAbs(snapshotPath) {
		return CutoverAcceptanceProcessConfig{}, errors.New("PEERGO_TRACKER_SNAPSHOT_PATH must be absolute")
	}
	subjectPath, err := required("PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH")
	if err != nil || !filepath.IsAbs(subjectPath) {
		return CutoverAcceptanceProcessConfig{}, errors.New("PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH must be absolute")
	}
	runtimePolicyPath, err := required("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH")
	if err != nil || !filepath.IsAbs(runtimePolicyPath) {
		return CutoverAcceptanceProcessConfig{}, errors.New("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH must be absolute")
	}
	cleanedPaths := map[string]struct{}{
		filepath.Clean(snapshotPath): {}, filepath.Clean(subjectPath): {}, filepath.Clean(runtimePolicyPath): {},
	}
	if len(cleanedPaths) != 3 {
		return CutoverAcceptanceProcessConfig{}, errors.New("Tracker snapshot paths must be distinct")
	}
	trustedValue, err := required("PEERGO_TRACKER_SNAPSHOT_TRUSTED_KEYS")
	if err != nil {
		return CutoverAcceptanceProcessConfig{}, err
	}
	trustedKeys, err := signedsnapshotv1.ParseTrustedKeys(trustedValue)
	if err != nil {
		return CutoverAcceptanceProcessConfig{}, err
	}
	snapshotMaxAge, err := cutoverDuration("PEERGO_TRACKER_SNAPSHOT_MAX_AGE", 10*time.Second, time.Hour)
	if err != nil {
		return CutoverAcceptanceProcessConfig{}, err
	}
	subjectMaxAge, err := cutoverDuration("PEERGO_TRACKER_SUBJECT_SNAPSHOT_MAX_AGE", time.Second, 10*time.Minute)
	if err != nil {
		return CutoverAcceptanceProcessConfig{}, err
	}
	runtimePolicyMaxAge, err := cutoverDuration("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_MAX_AGE", 10*time.Second, time.Hour)
	if err != nil {
		return CutoverAcceptanceProcessConfig{}, err
	}
	futureSkew, err := cutoverDuration("PEERGO_TRACKER_SNAPSHOT_MAX_FUTURE_SKEW", 0, 10*time.Minute)
	if err != nil {
		return CutoverAcceptanceProcessConfig{}, err
	}
	return CutoverAcceptanceProcessConfig{
		Environment: environment, SnapshotPath: filepath.Clean(snapshotPath),
		SubjectSnapshotPath: filepath.Clean(subjectPath), RuntimePolicyPath: filepath.Clean(runtimePolicyPath),
		TrustedKeys: trustedKeys, SnapshotMaxAge: snapshotMaxAge, SubjectMaxAge: subjectMaxAge,
		RuntimePolicyMaxAge: runtimePolicyMaxAge, MaxFutureSkew: futureSkew,
	}, nil
}

func cutoverDuration(name string, minimum, maximum time.Duration) (time.Duration, error) {
	value, err := required(name)
	if err != nil {
		return 0, err
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, errors.New(name + " is outside its accepted range")
	}
	return parsed, nil
}
