package config

import (
	"errors"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/core/internal/trafficconsumer"
)

// SwarmProjectorConfig gives one Core process read access to Tracker's two
// public-projection fact streams. It intentionally reuses the established NATS
// connection and pull-consumer binding types instead of creating a second
// transport abstraction for swarm data.
type SwarmProjectorConfig struct {
	CoreDatabaseProcessConfig
	NATS            trafficconsumer.ConnectionConfig
	Snapshot        trafficconsumer.BindingConfig
	Completion      trafficconsumer.BindingConfig
	RetryDelay      time.Duration
	StartupTimeout  time.Duration
	ShutdownTimeout time.Duration
	MaxFutureSkew   time.Duration
}

type SwarmConsumerProvision struct {
	Stream   string
	Consumer jetstream.ConsumerConfig
}

// SwarmConsumerProvisionerConfig remains separate from the runtime worker so
// the runtime credential cannot create or mutate durable consumers.
type SwarmConsumerProvisionerConfig struct {
	Environment string
	NATS        trafficconsumer.ConnectionConfig
	Timeout     time.Duration
	Consumers   []SwarmConsumerProvision
}

type swarmProjectionCommonConfig struct {
	environment       string
	natsURLs          []string
	natsRootCAFile    string
	connectTimeout    time.Duration
	reconnectWait     time.Duration
	snapshotStream    string
	snapshotSubject   string
	snapshotDurable   string
	completionStream  string
	completionSubject string
	completionDurable string
	fetchWait         time.Duration
	processTimeout    time.Duration
	ackTimeout        time.Duration
}

func LoadSwarmProjector() (SwarmProjectorConfig, error) {
	database, err := LoadCoreDatabaseProcess()
	if err != nil {
		return SwarmProjectorConfig{}, err
	}
	common, err := loadSwarmProjectionCommon(database.Environment)
	if err != nil {
		return SwarmProjectorConfig{}, err
	}
	credentials, err := projectionCredentialPath("PEERGO_CORE_SWARM_NATS_CREDENTIALS_FILE", common.environment)
	if err != nil {
		return SwarmProjectorConfig{}, err
	}
	retryDelay, err := projectionDuration("PEERGO_CORE_SWARM_RETRY_DELAY", 10*time.Millisecond, time.Minute)
	if err != nil {
		return SwarmProjectorConfig{}, err
	}
	startupTimeout, err := projectionDuration("PEERGO_CORE_SWARM_STARTUP_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return SwarmProjectorConfig{}, err
	}
	shutdownTimeout, err := projectionDuration("PEERGO_CORE_SWARM_SHUTDOWN_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return SwarmProjectorConfig{}, err
	}
	maxFutureSkew, err := projectionDuration("PEERGO_CORE_SWARM_MAX_FUTURE_SKEW", 0, 10*time.Minute)
	if err != nil {
		return SwarmProjectorConfig{}, err
	}
	connection := trafficconsumer.ConnectionConfig{
		URLs: common.natsURLs, CredentialsFile: credentials, RootCAFile: common.natsRootCAFile,
		ConnectTimeout: common.connectTimeout, ReconnectWait: common.reconnectWait,
	}
	return SwarmProjectorConfig{
		CoreDatabaseProcessConfig: database,
		NATS:                      connection,
		Snapshot: trafficconsumer.BindingConfig{
			Stream: common.snapshotStream, Subject: common.snapshotSubject, Durable: common.snapshotDurable,
			FetchWait: common.fetchWait, MaximumProcessingTime: common.processTimeout, MaximumAckTime: common.ackTimeout,
		},
		Completion: trafficconsumer.BindingConfig{
			Stream: common.completionStream, Subject: common.completionSubject, Durable: common.completionDurable,
			FetchWait: common.fetchWait, MaximumProcessingTime: common.processTimeout, MaximumAckTime: common.ackTimeout,
		},
		RetryDelay: retryDelay, StartupTimeout: startupTimeout, ShutdownTimeout: shutdownTimeout,
		MaxFutureSkew: maxFutureSkew,
	}, nil
}

func LoadSwarmConsumerProvisioner() (SwarmConsumerProvisionerConfig, error) {
	environment, err := loadCoreEnvironment()
	if err != nil {
		return SwarmConsumerProvisionerConfig{}, err
	}
	common, err := loadSwarmProjectionCommon(environment)
	if err != nil {
		return SwarmConsumerProvisionerConfig{}, err
	}
	credentials, err := projectionCredentialPath("PEERGO_CORE_SWARM_NATS_PROVISION_CREDENTIALS_FILE", environment)
	if err != nil {
		return SwarmConsumerProvisionerConfig{}, err
	}
	timeout, err := projectionDuration("PEERGO_CORE_SWARM_CONSUMER_PROVISION_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return SwarmConsumerProvisionerConfig{}, err
	}
	ackWait, err := projectionDuration("PEERGO_CORE_SWARM_CONSUMER_ACK_WAIT", time.Second, 10*time.Minute)
	if err != nil {
		return SwarmConsumerProvisionerConfig{}, err
	}
	if err := requireAckWindow("PEERGO_CORE_SWARM_CONSUMER_ACK_WAIT", ackWait, common.processTimeout, common.ackTimeout); err != nil {
		return SwarmConsumerProvisionerConfig{}, err
	}
	maxWaiting, err := projectionInteger("PEERGO_CORE_SWARM_CONSUMER_MAX_WAITING", 1, 512)
	if err != nil {
		return SwarmConsumerProvisionerConfig{}, err
	}
	maxRequestExpires, err := projectionDuration("PEERGO_CORE_SWARM_CONSUMER_MAX_REQUEST_EXPIRES", 100*time.Millisecond, time.Minute)
	if err != nil {
		return SwarmConsumerProvisionerConfig{}, err
	}
	if common.fetchWait > maxRequestExpires {
		return SwarmConsumerProvisionerConfig{}, errors.New("PEERGO_CORE_SWARM_FETCH_WAIT must not exceed consumer max request expiry")
	}
	consumer := func(name, description, subject, schema string) jetstream.ConsumerConfig {
		return jetstream.ConsumerConfig{
			Name: name, Durable: name, Description: description,
			DeliverPolicy: jetstream.DeliverAllPolicy, AckPolicy: jetstream.AckExplicitPolicy,
			AckWait: ackWait, MaxDeliver: -1, FilterSubject: subject,
			ReplayPolicy: jetstream.ReplayInstantPolicy, MaxWaiting: maxWaiting,
			// One in-flight message keeps each durable's database commit → ACK
			// boundary serial and makes fail-closed operator recovery auditable.
			MaxAckPending: 1, MaxRequestBatch: 1, MaxRequestExpires: maxRequestExpires,
			Metadata: map[string]string{"peergo.owner": "core", "peergo.schema": schema},
		}
	}
	return SwarmConsumerProvisionerConfig{
		Environment: environment,
		NATS: trafficconsumer.ConnectionConfig{
			URLs: common.natsURLs, CredentialsFile: credentials, RootCAFile: common.natsRootCAFile,
			ConnectTimeout: common.connectTimeout, ReconnectWait: common.reconnectWait,
		},
		Timeout: timeout,
		Consumers: []SwarmConsumerProvision{
			{Stream: common.snapshotStream, Consumer: consumer(common.snapshotDurable, "PeerGo Core swarm snapshot projector v1", common.snapshotSubject, trackerswarmv1.SchemaVersion)},
			{Stream: common.completionStream, Consumer: consumer(common.completionDurable, "PeerGo Core swarm completion projector v1", common.completionSubject, trackerannouncev1.SchemaVersion)},
		},
	}, nil
}

func loadSwarmProjectionCommon(environment string) (swarmProjectionCommonConfig, error) {
	natsURLs, err := projectionNATSURLs("PEERGO_CORE_SWARM_NATS_URLS", environment)
	if err != nil {
		return swarmProjectionCommonConfig{}, err
	}
	rootCA, err := projectionOptionalAbsolutePath("PEERGO_CORE_SWARM_NATS_ROOT_CA_FILE")
	if err != nil {
		return swarmProjectionCommonConfig{}, err
	}
	connectTimeout, err := projectionDuration("PEERGO_CORE_SWARM_NATS_CONNECT_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return swarmProjectionCommonConfig{}, err
	}
	reconnectWait, err := projectionDuration("PEERGO_CORE_SWARM_NATS_RECONNECT_WAIT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return swarmProjectionCommonConfig{}, err
	}
	snapshotStream, err := required("PEERGO_TRACKER_SWARM_SNAPSHOT_STREAM")
	if err != nil || !trackerswarmv1.ValidStreamName(snapshotStream) {
		return swarmProjectionCommonConfig{}, errors.New("PEERGO_TRACKER_SWARM_SNAPSHOT_STREAM must be a valid literal JetStream name")
	}
	snapshotSubject, err := required("PEERGO_TRACKER_SWARM_SNAPSHOT_SUBJECT")
	if err != nil || !trackerswarmv1.ValidLiteralSubject(snapshotSubject) {
		return swarmProjectionCommonConfig{}, errors.New("PEERGO_TRACKER_SWARM_SNAPSHOT_SUBJECT must be a valid literal NATS subject")
	}
	snapshotDurable, err := required("PEERGO_CORE_SWARM_SNAPSHOT_DURABLE")
	if err != nil || !trackerswarmv1.ValidStreamName(snapshotDurable) {
		return swarmProjectionCommonConfig{}, errors.New("PEERGO_CORE_SWARM_SNAPSHOT_DURABLE must be a valid literal durable name")
	}
	completionStream, err := required("PEERGO_TRACKER_ANNOUNCE_STREAM")
	if err != nil || !trackerannouncev1.ValidStreamName(completionStream) {
		return swarmProjectionCommonConfig{}, errors.New("PEERGO_TRACKER_ANNOUNCE_STREAM must be a valid literal JetStream name")
	}
	completionSubject, err := required("PEERGO_TRACKER_ANNOUNCE_SUBJECT")
	if err != nil || !trackerannouncev1.ValidLiteralSubject(completionSubject) {
		return swarmProjectionCommonConfig{}, errors.New("PEERGO_TRACKER_ANNOUNCE_SUBJECT must be a valid literal NATS subject")
	}
	completionDurable, err := required("PEERGO_CORE_SWARM_COMPLETION_DURABLE")
	if err != nil || !trackerannouncev1.ValidStreamName(completionDurable) {
		return swarmProjectionCommonConfig{}, errors.New("PEERGO_CORE_SWARM_COMPLETION_DURABLE must be a valid literal durable name")
	}
	if snapshotDurable == completionDurable {
		return swarmProjectionCommonConfig{}, errors.New("Core swarm snapshot and completion durable names must differ")
	}
	fetchWait, err := projectionDuration("PEERGO_CORE_SWARM_FETCH_WAIT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return swarmProjectionCommonConfig{}, err
	}
	processTimeout, err := projectionDuration("PEERGO_CORE_SWARM_PROCESS_TIMEOUT", 100*time.Millisecond, 10*time.Minute)
	if err != nil {
		return swarmProjectionCommonConfig{}, err
	}
	ackTimeout, err := projectionDuration("PEERGO_CORE_SWARM_ACK_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return swarmProjectionCommonConfig{}, err
	}
	return swarmProjectionCommonConfig{
		environment: environment, natsURLs: natsURLs, natsRootCAFile: rootCA,
		connectTimeout: connectTimeout, reconnectWait: reconnectWait,
		snapshotStream: snapshotStream, snapshotSubject: snapshotSubject, snapshotDurable: snapshotDurable,
		completionStream: completionStream, completionSubject: completionSubject, completionDurable: completionDurable,
		fetchWait: fetchWait, processTimeout: processTimeout, ackTimeout: ackTimeout,
	}, nil
}
