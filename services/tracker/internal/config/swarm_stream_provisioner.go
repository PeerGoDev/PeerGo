package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/deploymentv1"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
)

func LoadSwarmStreamProvisioner() (StreamProvisionerConfig, error) {
	environment, err := required("PEERGO_ENV")
	if err != nil {
		return StreamProvisionerConfig{}, err
	}
	if environment != "development" && environment != "production" {
		return StreamProvisionerConfig{}, errors.New("PEERGO_ENV must be development or production")
	}
	urls, err := parseNATSURLs(environment)
	if err != nil {
		return StreamProvisionerConfig{}, err
	}
	credentials := strings.TrimSpace(os.Getenv("PEERGO_TRACKER_NATS_PROVISION_CREDENTIALS_FILE"))
	if credentials != "" {
		if !filepath.IsAbs(credentials) {
			return StreamProvisionerConfig{}, errors.New("PEERGO_TRACKER_NATS_PROVISION_CREDENTIALS_FILE must be absolute when configured")
		}
		credentials = filepath.Clean(credentials)
	}
	if environment == "production" && credentials == "" {
		return StreamProvisionerConfig{}, errors.New("PEERGO_TRACKER_NATS_PROVISION_CREDENTIALS_FILE is required in production")
	}
	rootCA, err := optionalAbsolutePath("PEERGO_TRACKER_NATS_ROOT_CA_FILE")
	if err != nil {
		return StreamProvisionerConfig{}, err
	}
	connectTimeout, err := parseDuration("PEERGO_TRACKER_NATS_CONNECT_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return StreamProvisionerConfig{}, err
	}
	reconnectWait, err := parseDuration("PEERGO_TRACKER_NATS_RECONNECT_WAIT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return StreamProvisionerConfig{}, err
	}
	timeout, err := parseDuration("PEERGO_TRACKER_SWARM_STREAM_PROVISION_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return StreamProvisionerConfig{}, err
	}
	name, err := required("PEERGO_TRACKER_SWARM_SNAPSHOT_STREAM")
	if err != nil || !trackerswarmv1.ValidStreamName(name) {
		return StreamProvisionerConfig{}, errors.New("PEERGO_TRACKER_SWARM_SNAPSHOT_STREAM must be a valid literal JetStream name")
	}
	subject, err := required("PEERGO_TRACKER_SWARM_SNAPSHOT_SUBJECT")
	if err != nil || !trackerswarmv1.ValidLiteralSubject(subject) {
		return StreamProvisionerConfig{}, errors.New("PEERGO_TRACKER_SWARM_SNAPSHOT_SUBJECT must be a valid literal NATS subject")
	}
	maxBytes, err := parseInteger64("PEERGO_TRACKER_SWARM_STREAM_MAX_BYTES", 1<<20, 1<<50)
	if err != nil {
		return StreamProvisionerConfig{}, err
	}
	maxAge, err := parseDuration("PEERGO_TRACKER_SWARM_STREAM_MAX_AGE", 10*time.Minute, 7*24*time.Hour)
	if err != nil {
		return StreamProvisionerConfig{}, err
	}
	duplicateWindow, err := parseDuration("PEERGO_TRACKER_SWARM_STREAM_DUPLICATE_WINDOW", time.Second, time.Hour)
	if err != nil {
		return StreamProvisionerConfig{}, err
	}
	replicas, err := parseInteger("PEERGO_TRACKER_SWARM_STREAM_REPLICAS", 1, 5)
	if err != nil {
		return StreamProvisionerConfig{}, err
	}
	mode, modeErr := deploymentv1.Load()
	if modeErr != nil {
		return StreamProvisionerConfig{}, modeErr
	}
	if environment == "production" && mode != deploymentv1.SingleServer && replicas < 3 {
		return StreamProvisionerConfig{}, errors.New("PEERGO_TRACKER_SWARM_STREAM_REPLICAS must be at least 3 in production")
	}
	return StreamProvisionerConfig{
		NATSURLs: urls, NATSCredentialsFile: credentials, NATSRootCAFile: rootCA,
		NATSConnectTimeout: connectTimeout, NATSReconnectWait: reconnectWait, Timeout: timeout,
		Stream: jetstream.StreamConfig{
			Name: name, Description: "PeerGo complete Tracker swarm snapshot chunks v1", Subjects: []string{subject},
			Retention: jetstream.LimitsPolicy, MaxConsumers: 8, MaxMsgs: -1, MaxBytes: maxBytes,
			Discard: jetstream.DiscardOld, MaxAge: maxAge, MaxMsgsPerSubject: -1,
			MaxMsgSize: int32(trackerswarmv1.MaxEventBytes + (4 << 10)), Storage: jetstream.FileStorage,
			Replicas: replicas, Duplicates: duplicateWindow, DenyDelete: true, DenyPurge: true,
			Compression: jetstream.S2Compression,
			Metadata:    map[string]string{"peergo.schema": trackerswarmv1.SchemaVersion, "peergo.snapshot_scope": trackerswarmv1.ScopeAll},
		},
	}, nil
}
