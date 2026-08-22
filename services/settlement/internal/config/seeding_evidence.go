package config

import (
	"errors"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/settlement/internal/jetstreamconsumer"
)

type SeedingSnapshotRuntimeConfig struct {
	Environment     string
	DatabaseURL     string
	NATS            jetstreamconsumer.ConnectionConfig
	Stream          string
	Subject         string
	Durable         string
	FetchWait       time.Duration
	ProcessTimeout  time.Duration
	AckTimeout      time.Duration
	RetryDelay      time.Duration
	StartupTimeout  time.Duration
	ShutdownTimeout time.Duration
	MaxFutureSkew   time.Duration
	AnnounceStream  string
	ClosureDelay    time.Duration
}

type SeedingSnapshotProvisionerConfig struct {
	Environment string
	NATS        jetstreamconsumer.ConnectionConfig
	Timeout     time.Duration
	Stream      string
	Consumer    jetstream.ConsumerConfig
}

type SeedingEvidenceWorkerConfig struct {
	Environment       string
	DatabaseURL       string
	AnnounceStream    string
	SnapshotStream    string
	SnapshotSubject   string
	InitialWindow     time.Time
	ClosureDelay      time.Duration
	MaxIntervalCredit time.Duration
	SnapshotMaxDelay  time.Duration
	MaxFutureSkew     time.Duration
	IdleInterval      time.Duration
	StartupTimeout    time.Duration
	ShutdownTimeout   time.Duration
}

func LoadSeedingSnapshotRuntime() (SeedingSnapshotRuntimeConfig, error) {
	base, err := loadSeedingBase("PEERGO_SETTLEMENT_NATS_CREDENTIALS_FILE")
	if err != nil {
		return SeedingSnapshotRuntimeConfig{}, err
	}
	databaseURL, err := seedingDatabaseURL(base.environment)
	if err != nil {
		return SeedingSnapshotRuntimeConfig{}, err
	}
	fetchWait, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_FETCH_WAIT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return SeedingSnapshotRuntimeConfig{}, err
	}
	processTimeout, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_PROCESS_TIMEOUT", 100*time.Millisecond, 10*time.Minute)
	if err != nil {
		return SeedingSnapshotRuntimeConfig{}, err
	}
	ackTimeout, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_ACK_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return SeedingSnapshotRuntimeConfig{}, err
	}
	retryDelay, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_RETRY_DELAY", 10*time.Millisecond, time.Minute)
	if err != nil {
		return SeedingSnapshotRuntimeConfig{}, err
	}
	startupTimeout, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_STARTUP_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return SeedingSnapshotRuntimeConfig{}, err
	}
	shutdownTimeout, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_SHUTDOWN_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return SeedingSnapshotRuntimeConfig{}, err
	}
	maxFutureSkew, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_MAX_FUTURE_SKEW", 0, 10*time.Minute)
	if err != nil {
		return SeedingSnapshotRuntimeConfig{}, err
	}
	closureDelay, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_MAX_CLOSURE_DELAY", time.Second, time.Hour)
	if err != nil {
		return SeedingSnapshotRuntimeConfig{}, err
	}
	return SeedingSnapshotRuntimeConfig{
		Environment: base.environment, DatabaseURL: databaseURL, NATS: base.nats,
		Stream: base.snapshotStream, Subject: base.snapshotSubject, Durable: base.durable,
		FetchWait: fetchWait, ProcessTimeout: processTimeout, AckTimeout: ackTimeout,
		RetryDelay: retryDelay, StartupTimeout: startupTimeout, ShutdownTimeout: shutdownTimeout,
		MaxFutureSkew: maxFutureSkew, AnnounceStream: base.announceStream, ClosureDelay: closureDelay,
	}, nil
}

func LoadSeedingSnapshotProvisioner() (SeedingSnapshotProvisionerConfig, error) {
	base, err := loadSeedingBase("PEERGO_SETTLEMENT_NATS_PROVISION_CREDENTIALS_FILE")
	if err != nil {
		return SeedingSnapshotProvisionerConfig{}, err
	}
	timeout, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_PROVISION_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return SeedingSnapshotProvisionerConfig{}, err
	}
	ackWait, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_ACK_WAIT", time.Second, 10*time.Minute)
	if err != nil {
		return SeedingSnapshotProvisionerConfig{}, err
	}
	processTimeout, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_PROCESS_TIMEOUT", 100*time.Millisecond, 10*time.Minute)
	if err != nil {
		return SeedingSnapshotProvisionerConfig{}, err
	}
	ackTimeout, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_ACK_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return SeedingSnapshotProvisionerConfig{}, err
	}
	if ackWait <= processTimeout+ackTimeout {
		return SeedingSnapshotProvisionerConfig{}, errors.New("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_ACK_WAIT must exceed process timeout plus ACK timeout")
	}
	maxWaiting, err := integer("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_MAX_WAITING", 1, 512)
	if err != nil {
		return SeedingSnapshotProvisionerConfig{}, err
	}
	maxRequestExpires, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_MAX_REQUEST_EXPIRES", 100*time.Millisecond, time.Minute)
	if err != nil {
		return SeedingSnapshotProvisionerConfig{}, err
	}
	fetchWait, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_FETCH_WAIT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return SeedingSnapshotProvisionerConfig{}, err
	}
	if fetchWait > maxRequestExpires {
		return SeedingSnapshotProvisionerConfig{}, errors.New("seeding snapshot fetch wait must not exceed max request expiry")
	}
	return SeedingSnapshotProvisionerConfig{
		Environment: base.environment, NATS: base.nats, Timeout: timeout, Stream: base.snapshotStream,
		Consumer: jetstream.ConsumerConfig{
			Name: base.durable, Durable: base.durable,
			Description:   "PeerGo Settlement historical seeding snapshot evidence v1",
			DeliverPolicy: jetstream.DeliverAllPolicy, AckPolicy: jetstream.AckExplicitPolicy,
			AckWait: ackWait, MaxDeliver: -1, FilterSubject: base.snapshotSubject,
			ReplayPolicy: jetstream.ReplayInstantPolicy, MaxWaiting: maxWaiting,
			MaxAckPending: 1, MaxRequestBatch: 1, MaxRequestExpires: maxRequestExpires,
			Metadata: map[string]string{
				"peergo.owner": "settlement", "peergo.schema": trackerswarmv1.SchemaVersion,
			},
		},
	}, nil
}

func LoadSeedingEvidenceWorker() (SeedingEvidenceWorkerConfig, error) {
	environment, databaseURL, announceStream, snapshotStream, snapshotSubject, err := loadSeedingDatabaseAndStreams()
	if err != nil {
		return SeedingEvidenceWorkerConfig{}, err
	}
	initialRaw, err := required("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT")
	if err != nil {
		return SeedingEvidenceWorkerConfig{}, err
	}
	initial, err := time.Parse(time.RFC3339, initialRaw)
	_, offset := initial.Zone()
	if err != nil || offset != 0 || !initial.Equal(initial.Truncate(time.Hour)) {
		return SeedingEvidenceWorkerConfig{}, errors.New("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_START_AT must be an exact UTC RFC3339 hour")
	}
	closureDelay, err := duration("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_CLOSURE_DELAY", time.Minute, time.Hour)
	if err != nil {
		return SeedingEvidenceWorkerConfig{}, err
	}
	maxIntervalCredit, err := duration("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_MAX_INTERVAL_CREDIT", time.Minute, time.Hour)
	if err != nil {
		return SeedingEvidenceWorkerConfig{}, err
	}
	if closureDelay < maxIntervalCredit {
		return SeedingEvidenceWorkerConfig{}, errors.New("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_CLOSURE_DELAY must be at least PEERGO_SETTLEMENT_SEEDING_EVIDENCE_MAX_INTERVAL_CREDIT")
	}
	if closureDelay%time.Second != 0 || maxIntervalCredit%time.Second != 0 {
		return SeedingEvidenceWorkerConfig{}, errors.New("seeding evidence closure and interval limits must use whole seconds")
	}
	snapshotMaxDelay, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_MAX_CLOSURE_DELAY", time.Second, time.Hour)
	if err != nil {
		return SeedingEvidenceWorkerConfig{}, err
	}
	maxFutureSkew, err := duration("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_MAX_FUTURE_SKEW", 0, 10*time.Minute)
	if err != nil {
		return SeedingEvidenceWorkerConfig{}, err
	}
	idleInterval, err := duration("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_IDLE_INTERVAL", 100*time.Millisecond, time.Minute)
	if err != nil {
		return SeedingEvidenceWorkerConfig{}, err
	}
	startupTimeout, err := duration("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STARTUP_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return SeedingEvidenceWorkerConfig{}, err
	}
	shutdownTimeout, err := duration("PEERGO_SETTLEMENT_SEEDING_EVIDENCE_SHUTDOWN_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return SeedingEvidenceWorkerConfig{}, err
	}
	return SeedingEvidenceWorkerConfig{
		Environment: environment, DatabaseURL: databaseURL, AnnounceStream: announceStream,
		SnapshotStream: snapshotStream, SnapshotSubject: snapshotSubject,
		InitialWindow: initial.UTC(), ClosureDelay: closureDelay, MaxIntervalCredit: maxIntervalCredit,
		SnapshotMaxDelay: snapshotMaxDelay,
		MaxFutureSkew:    maxFutureSkew, IdleInterval: idleInterval,
		StartupTimeout: startupTimeout, ShutdownTimeout: shutdownTimeout,
	}, nil
}

type seedingBase struct {
	environment     string
	nats            jetstreamconsumer.ConnectionConfig
	announceStream  string
	snapshotStream  string
	snapshotSubject string
	durable         string
}

func loadSeedingBase(credentialsVariable string) (seedingBase, error) {
	environment, announceStream, snapshotStream, snapshotSubject, err := loadSeedingEnvironmentAndStreams()
	if err != nil {
		return seedingBase{}, err
	}
	urls, err := natsURLs(environment)
	if err != nil {
		return seedingBase{}, err
	}
	credentials, err := credentialPath(credentialsVariable, environment)
	if err != nil {
		return seedingBase{}, err
	}
	rootCA, err := optionalAbsolutePath("PEERGO_SETTLEMENT_NATS_ROOT_CA_FILE")
	if err != nil {
		return seedingBase{}, err
	}
	connectTimeout, err := duration("PEERGO_SETTLEMENT_NATS_CONNECT_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return seedingBase{}, err
	}
	reconnectWait, err := duration("PEERGO_SETTLEMENT_NATS_RECONNECT_WAIT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return seedingBase{}, err
	}
	durable, err := required("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_DURABLE")
	if err != nil || !trackerswarmv1.ValidStreamName(durable) {
		return seedingBase{}, errors.New("PEERGO_SETTLEMENT_SEEDING_SNAPSHOT_DURABLE must be a valid literal durable name")
	}
	return seedingBase{
		environment: environment,
		nats: jetstreamconsumer.ConnectionConfig{
			URLs: urls, CredentialsFile: credentials, RootCAFile: rootCA,
			ConnectTimeout: connectTimeout, ReconnectWait: reconnectWait,
		},
		announceStream: announceStream, snapshotStream: snapshotStream,
		snapshotSubject: snapshotSubject, durable: durable,
	}, nil
}

func loadSeedingDatabaseAndStreams() (string, string, string, string, string, error) {
	environment, announceStream, snapshotStream, snapshotSubject, err := loadSeedingEnvironmentAndStreams()
	if err != nil {
		return "", "", "", "", "", err
	}
	databaseURL, err := seedingDatabaseURL(environment)
	if err != nil {
		return "", "", "", "", "", err
	}
	return environment, databaseURL, announceStream, snapshotStream, snapshotSubject, nil
}

func loadSeedingEnvironmentAndStreams() (string, string, string, string, error) {
	environment, err := required("PEERGO_ENV")
	if err != nil || (environment != "development" && environment != "production") {
		return "", "", "", "", errors.New("PEERGO_ENV must be development or production")
	}
	announceStream, err := required("PEERGO_TRACKER_ANNOUNCE_STREAM")
	if err != nil || !trackerannouncev1.ValidStreamName(announceStream) {
		return "", "", "", "", errors.New("PEERGO_TRACKER_ANNOUNCE_STREAM must be a valid literal JetStream name")
	}
	snapshotStream, err := required("PEERGO_TRACKER_SWARM_SNAPSHOT_STREAM")
	if err != nil || !trackerswarmv1.ValidStreamName(snapshotStream) {
		return "", "", "", "", errors.New("PEERGO_TRACKER_SWARM_SNAPSHOT_STREAM must be a valid literal JetStream name")
	}
	snapshotSubject, err := required("PEERGO_TRACKER_SWARM_SNAPSHOT_SUBJECT")
	if err != nil || !trackerswarmv1.ValidLiteralSubject(snapshotSubject) {
		return "", "", "", "", errors.New("PEERGO_TRACKER_SWARM_SNAPSHOT_SUBJECT must be a valid literal NATS subject")
	}
	return environment, announceStream, snapshotStream, snapshotSubject, nil
}

func seedingDatabaseURL(environment string) (string, error) {
	databaseURL, err := required("PEERGO_TRACKER_DATABASE_URL")
	if err != nil || validateDatabaseURL(databaseURL, environment) != nil {
		return "", errors.New("PEERGO_TRACKER_DATABASE_URL must be a PostgreSQL connection URL with a database name")
	}
	return databaseURL, nil
}
