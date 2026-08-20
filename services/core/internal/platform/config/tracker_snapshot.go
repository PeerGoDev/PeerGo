package config

import (
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/peergo/peergo/contracts/go/trackercontrolv1"
)

type TrackerSnapshotBuilderProcessConfig struct {
	Environment         string
	DatabaseURL         string
	KeyID               string
	SigningKey          ed25519.PrivateKey
	SnapshotPath        string
	SubjectSnapshotPath string
	RuntimePolicyPath   string
	PublishInterval     time.Duration
}

// LoadTrackerSnapshotBuilder gives the narrow builder only a Core database
// credential, one signing key and its publication path. Tracker processes must
// receive public keys instead of reusing this configuration.
func LoadTrackerSnapshotBuilder() (TrackerSnapshotBuilderProcessConfig, error) {
	database, err := LoadCoreDatabaseProcess()
	if err != nil {
		return TrackerSnapshotBuilderProcessConfig{}, err
	}
	keyID, err := required("PEERGO_TRACKER_SNAPSHOT_KEY_ID")
	if err != nil || trackercontrolv1.ValidateKeyID(keyID) != nil {
		return TrackerSnapshotBuilderProcessConfig{}, errors.New("PEERGO_TRACKER_SNAPSHOT_KEY_ID is invalid")
	}
	encodedKey, err := required("PEERGO_TRACKER_SNAPSHOT_SIGNING_KEY_BASE64")
	if err != nil {
		return TrackerSnapshotBuilderProcessConfig{}, err
	}
	rawKey, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return TrackerSnapshotBuilderProcessConfig{}, errors.New("PEERGO_TRACKER_SNAPSHOT_SIGNING_KEY_BASE64 must use standard padded Base64")
	}
	var signingKey ed25519.PrivateKey
	switch len(rawKey) {
	case ed25519.SeedSize:
		signingKey = ed25519.NewKeyFromSeed(rawKey)
	case ed25519.PrivateKeySize:
		derived := ed25519.NewKeyFromSeed(rawKey[:ed25519.SeedSize])
		if subtle.ConstantTimeCompare(rawKey, derived) != 1 {
			return TrackerSnapshotBuilderProcessConfig{}, errors.New("PEERGO_TRACKER_SNAPSHOT_SIGNING_KEY_BASE64 contains an inconsistent Ed25519 private key")
		}
		signingKey = append(ed25519.PrivateKey(nil), rawKey...)
	default:
		return TrackerSnapshotBuilderProcessConfig{}, errors.New("PEERGO_TRACKER_SNAPSHOT_SIGNING_KEY_BASE64 must decode to a 32-byte seed or 64-byte Ed25519 private key")
	}
	snapshotPath, err := required("PEERGO_TRACKER_SNAPSHOT_PATH")
	if err != nil {
		return TrackerSnapshotBuilderProcessConfig{}, err
	}
	if !filepath.IsAbs(snapshotPath) {
		return TrackerSnapshotBuilderProcessConfig{}, errors.New("PEERGO_TRACKER_SNAPSHOT_PATH must be absolute")
	}
	subjectSnapshotPath, err := required("PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH")
	if err != nil {
		return TrackerSnapshotBuilderProcessConfig{}, err
	}
	if !filepath.IsAbs(subjectSnapshotPath) {
		return TrackerSnapshotBuilderProcessConfig{}, errors.New("PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH must be absolute")
	}
	if filepath.Clean(snapshotPath) == filepath.Clean(subjectSnapshotPath) {
		return TrackerSnapshotBuilderProcessConfig{}, errors.New("Tracker torrent and subject snapshot paths must differ")
	}
	runtimePolicyPath, err := required("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH")
	if err != nil {
		return TrackerSnapshotBuilderProcessConfig{}, err
	}
	if !filepath.IsAbs(runtimePolicyPath) {
		return TrackerSnapshotBuilderProcessConfig{}, errors.New("PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH must be absolute")
	}
	runtimePolicyPath = filepath.Clean(runtimePolicyPath)
	if runtimePolicyPath == filepath.Clean(snapshotPath) || runtimePolicyPath == filepath.Clean(subjectSnapshotPath) {
		return TrackerSnapshotBuilderProcessConfig{}, errors.New("Tracker runtime policy snapshot path must be distinct")
	}
	publishInterval := time.Duration(0)
	if raw := strings.TrimSpace(os.Getenv("PEERGO_TRACKER_SNAPSHOT_PUBLISH_INTERVAL")); raw != "" {
		publishInterval, err = time.ParseDuration(raw)
		if err != nil || publishInterval < 10*time.Second || publishInterval > time.Hour {
			return TrackerSnapshotBuilderProcessConfig{}, errors.New("PEERGO_TRACKER_SNAPSHOT_PUBLISH_INTERVAL must be between 10s and 1h")
		}
	}
	return TrackerSnapshotBuilderProcessConfig{
		Environment: database.Environment, DatabaseURL: database.DatabaseURL, KeyID: keyID,
		SigningKey: signingKey, SnapshotPath: filepath.Clean(snapshotPath),
		SubjectSnapshotPath: filepath.Clean(subjectSnapshotPath), RuntimePolicyPath: runtimePolicyPath,
		PublishInterval: publishInterval,
	}, nil
}
