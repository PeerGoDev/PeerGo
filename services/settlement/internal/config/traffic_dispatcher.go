package config

import (
	"errors"
	"time"

	"github.com/peergo/peergo/contracts/go/settlementhnrv1"
	"github.com/peergo/peergo/contracts/go/settlementseedingv1"
	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
	"github.com/peergo/peergo/services/settlement/internal/jetstreamconsumer"
)

type TrafficDispatcherConfig struct {
	TrackerLedgerProcessConfig
	NATS            jetstreamconsumer.ConnectionConfig
	Stream          string
	Subject         string
	LeaseDuration   time.Duration
	IdleInterval    time.Duration
	RetryBase       time.Duration
	PublishTimeout  time.Duration
	StartupTimeout  time.Duration
	ShutdownTimeout time.Duration
}

type HNRDispatcherConfig = TrafficDispatcherConfig
type SeedingEvidenceDispatcherConfig = TrafficDispatcherConfig

func LoadTrafficDispatcher() (TrafficDispatcherConfig, error) {
	return loadResultDispatcher(
		"PEERGO_SETTLEMENT_TRAFFIC", "PEERGO_SETTLEMENT_NATS_PUBLISH_CREDENTIALS_FILE", trafficStreamIdentity,
	)
}

func LoadHNRDispatcher() (HNRDispatcherConfig, error) {
	return loadResultDispatcher(
		"PEERGO_SETTLEMENT_HNR", "PEERGO_SETTLEMENT_NATS_HNR_PUBLISH_CREDENTIALS_FILE", hnrStreamIdentity,
	)
}

func LoadSeedingEvidenceDispatcher() (SeedingEvidenceDispatcherConfig, error) {
	return loadResultDispatcher(
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE", "PEERGO_SETTLEMENT_NATS_SEEDING_EVIDENCE_PUBLISH_CREDENTIALS_FILE", seedingEvidenceStreamIdentity,
	)
}

func loadResultDispatcher(prefix, credentialName string, identity func() (string, string, error)) (TrafficDispatcherConfig, error) {
	database, err := LoadTrackerLedgerProcess()
	if err != nil {
		return TrafficDispatcherConfig{}, err
	}
	connection, err := loadResultNATSConnection(database.Environment, credentialName)
	if err != nil {
		return TrafficDispatcherConfig{}, err
	}
	stream, subject, err := identity()
	if err != nil {
		return TrafficDispatcherConfig{}, err
	}
	leaseDuration, err := duration(prefix+"_OUTBOX_LEASE_DURATION", time.Second, 10*time.Minute)
	if err != nil {
		return TrafficDispatcherConfig{}, err
	}
	idleInterval, err := duration(prefix+"_OUTBOX_IDLE_INTERVAL", 50*time.Millisecond, time.Minute)
	if err != nil {
		return TrafficDispatcherConfig{}, err
	}
	retryBase, err := duration(prefix+"_OUTBOX_RETRY_BASE", 100*time.Millisecond, time.Minute)
	if err != nil {
		return TrafficDispatcherConfig{}, err
	}
	publishTimeout, err := duration(prefix+"_PUBLISH_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return TrafficDispatcherConfig{}, err
	}
	startupTimeout, err := duration(prefix+"_STARTUP_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return TrafficDispatcherConfig{}, err
	}
	shutdownTimeout, err := duration(prefix+"_SHUTDOWN_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return TrafficDispatcherConfig{}, err
	}
	return TrafficDispatcherConfig{
		TrackerLedgerProcessConfig: database, NATS: connection, Stream: stream, Subject: subject,
		LeaseDuration: leaseDuration, IdleInterval: idleInterval, RetryBase: retryBase, PublishTimeout: publishTimeout,
		StartupTimeout: startupTimeout, ShutdownTimeout: shutdownTimeout,
	}, nil
}

func hnrStreamIdentity() (string, string, error) {
	stream, err := required("PEERGO_SETTLEMENT_HNR_STREAM")
	if err != nil || !settlementhnrv1.ValidStreamName(stream) {
		return "", "", errors.New("PEERGO_SETTLEMENT_HNR_STREAM must be a valid literal JetStream name")
	}
	subject, err := required("PEERGO_SETTLEMENT_HNR_SUBJECT")
	if err != nil || !settlementhnrv1.ValidLiteralSubject(subject) {
		return "", "", errors.New("PEERGO_SETTLEMENT_HNR_SUBJECT must be a valid literal NATS subject")
	}
	return stream, subject, nil
}

func trafficStreamIdentity() (string, string, error) {
	stream, err := required("PEERGO_SETTLEMENT_TRAFFIC_STREAM")
	if err != nil || !settlementtrafficv1.ValidStreamName(stream) {
		return "", "", errors.New("PEERGO_SETTLEMENT_TRAFFIC_STREAM must be a valid literal JetStream name")
	}
	subject, err := required("PEERGO_SETTLEMENT_TRAFFIC_SUBJECT")
	if err != nil || !settlementtrafficv1.ValidLiteralSubject(subject) {
		return "", "", errors.New("PEERGO_SETTLEMENT_TRAFFIC_SUBJECT must be a valid literal NATS subject")
	}
	return stream, subject, nil
}

func seedingEvidenceStreamIdentity() (string, string, error) {
	stream, err := required("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STREAM")
	if err != nil || !settlementseedingv1.ValidStreamName(stream) {
		return "", "", errors.New("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STREAM must be a valid literal JetStream name")
	}
	subject, err := required("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_SUBJECT")
	if err != nil || !settlementseedingv1.ValidLiteralSubject(subject) {
		return "", "", errors.New("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_SUBJECT must be a valid literal NATS subject")
	}
	return stream, subject, nil
}

func loadResultNATSConnection(environment, credentialName string) (jetstreamconsumer.ConnectionConfig, error) {
	urls, err := natsURLs(environment)
	if err != nil {
		return jetstreamconsumer.ConnectionConfig{}, err
	}
	rootCA, err := optionalAbsolutePath("PEERGO_SETTLEMENT_NATS_ROOT_CA_FILE")
	if err != nil {
		return jetstreamconsumer.ConnectionConfig{}, err
	}
	credentials, err := credentialPath(credentialName, environment)
	if err != nil {
		return jetstreamconsumer.ConnectionConfig{}, err
	}
	connectTimeout, err := duration("PEERGO_SETTLEMENT_NATS_CONNECT_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return jetstreamconsumer.ConnectionConfig{}, err
	}
	reconnectWait, err := duration("PEERGO_SETTLEMENT_NATS_RECONNECT_WAIT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return jetstreamconsumer.ConnectionConfig{}, err
	}
	return jetstreamconsumer.ConnectionConfig{
		URLs: urls, CredentialsFile: credentials, RootCAFile: rootCA, ConnectTimeout: connectTimeout, ReconnectWait: reconnectWait,
	}, nil
}
