package config

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	contract "github.com/peergo/peergo/contracts/go/jetstreamv1"
	"github.com/peergo/peergo/contracts/go/settlementhnrv1"
	"github.com/peergo/peergo/contracts/go/settlementseedingv1"
	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
	"github.com/peergo/peergo/services/core/internal/trafficconsumer"
)

// ProjectionProjectorConfig grants one result projector exactly one Core
// database connection and one runtime-only NATS credential.
type ProjectionProjectorConfig struct {
	CoreDatabaseProcessConfig
	NATS            trafficconsumer.ConnectionConfig
	Stream          string
	Subject         string
	Durable         string
	FetchWait       time.Duration
	ProcessTimeout  time.Duration
	AckTimeout      time.Duration
	RetryDelay      time.Duration
	StartupTimeout  time.Duration
	ShutdownTimeout time.Duration
}

type TrafficProjectorConfig = ProjectionProjectorConfig
type HNRProjectorConfig = ProjectionProjectorConfig

type SeedingEvidenceProjectorConfig struct {
	ProjectionProjectorConfig
	MaxFutureSkew time.Duration
}

// ProjectionConsumerProvisionerConfig is separate from runtime configuration:
// management credentials may create a durable, while the projector may only
// consume and acknowledge its one result subject.
type ProjectionConsumerProvisionerConfig struct {
	Environment string
	NATS        trafficconsumer.ConnectionConfig
	Timeout     time.Duration
	Stream      string
	Consumer    jetstream.ConsumerConfig
}

type TrafficConsumerProvisionerConfig = ProjectionConsumerProvisionerConfig
type HNRConsumerProvisionerConfig = ProjectionConsumerProvisionerConfig
type SeedingEvidenceConsumerProvisionerConfig = ProjectionConsumerProvisionerConfig

type projectionSpec struct {
	prefix              string
	natsURLsName        string
	runtimeCredential   string
	provisionCredential string
	rootCAName          string
	streamName          string
	subjectName         string
	durableName         string
	schema              string
	description         string
}

type projectionCommon struct {
	environment    string
	natsURLs       []string
	natsRootCAFile string
	connectTimeout time.Duration
	reconnectWait  time.Duration
	stream         string
	subject        string
	durable        string
}

var trafficProjectionSpec = projectionSpec{
	prefix: "PEERGO_CORE_TRAFFIC", natsURLsName: "PEERGO_CORE_TRAFFIC_NATS_URLS",
	runtimeCredential:   "PEERGO_CORE_TRAFFIC_NATS_CREDENTIALS_FILE",
	provisionCredential: "PEERGO_CORE_TRAFFIC_NATS_PROVISION_CREDENTIALS_FILE",
	rootCAName:          "PEERGO_CORE_TRAFFIC_NATS_ROOT_CA_FILE",
	streamName:          "PEERGO_SETTLEMENT_TRAFFIC_STREAM", subjectName: "PEERGO_SETTLEMENT_TRAFFIC_SUBJECT",
	durableName: "PEERGO_CORE_TRAFFIC_DURABLE", schema: settlementtrafficv1.SchemaVersion,
	description: "PeerGo Core user traffic projector v1",
}

var hnrProjectionSpec = projectionSpec{
	prefix: "PEERGO_CORE_HNR", natsURLsName: "PEERGO_CORE_HNR_NATS_URLS",
	runtimeCredential:   "PEERGO_CORE_HNR_NATS_CREDENTIALS_FILE",
	provisionCredential: "PEERGO_CORE_HNR_NATS_PROVISION_CREDENTIALS_FILE",
	rootCAName:          "PEERGO_CORE_HNR_NATS_ROOT_CA_FILE",
	streamName:          "PEERGO_SETTLEMENT_HNR_STREAM", subjectName: "PEERGO_SETTLEMENT_HNR_SUBJECT",
	durableName: "PEERGO_CORE_HNR_DURABLE", schema: settlementhnrv1.SchemaVersion,
	description: "PeerGo Core user H&R projector v1",
}

var seedingEvidenceProjectionSpec = projectionSpec{
	prefix: "PEERGO_CORE_SEEDING_EVIDENCE", natsURLsName: "PEERGO_CORE_SEEDING_EVIDENCE_NATS_URLS",
	runtimeCredential:   "PEERGO_CORE_SEEDING_EVIDENCE_NATS_CREDENTIALS_FILE",
	provisionCredential: "PEERGO_CORE_SEEDING_EVIDENCE_NATS_PROVISION_CREDENTIALS_FILE",
	rootCAName:          "PEERGO_CORE_SEEDING_EVIDENCE_NATS_ROOT_CA_FILE",
	streamName:          "PEERGO_SETTLEMENT_SEEDING_EVIDENCE_STREAM",
	subjectName:         "PEERGO_SETTLEMENT_SEEDING_EVIDENCE_SUBJECT",
	durableName:         "PEERGO_CORE_SEEDING_EVIDENCE_DURABLE", schema: settlementseedingv1.SchemaVersion,
	description: "PeerGo Core hourly seeding reward evidence projector v1",
}

func LoadTrafficProjector() (TrafficProjectorConfig, error) {
	return loadProjectionProjector(trafficProjectionSpec)
}

func LoadHNRProjector() (HNRProjectorConfig, error) {
	return loadProjectionProjector(hnrProjectionSpec)
}

func LoadSeedingEvidenceProjector() (SeedingEvidenceProjectorConfig, error) {
	base, err := loadProjectionProjector(seedingEvidenceProjectionSpec)
	if err != nil {
		return SeedingEvidenceProjectorConfig{}, err
	}
	maxFutureSkew, err := projectionDuration("PEERGO_CORE_SEEDING_EVIDENCE_MAX_FUTURE_SKEW", 0, 10*time.Minute)
	if err != nil {
		return SeedingEvidenceProjectorConfig{}, err
	}
	return SeedingEvidenceProjectorConfig{ProjectionProjectorConfig: base, MaxFutureSkew: maxFutureSkew}, nil
}

func LoadTrafficConsumerProvisioner() (TrafficConsumerProvisionerConfig, error) {
	return loadProjectionConsumerProvisioner(trafficProjectionSpec)
}

func LoadHNRConsumerProvisioner() (HNRConsumerProvisionerConfig, error) {
	return loadProjectionConsumerProvisioner(hnrProjectionSpec)
}

func LoadSeedingEvidenceConsumerProvisioner() (SeedingEvidenceConsumerProvisionerConfig, error) {
	return loadProjectionConsumerProvisioner(seedingEvidenceProjectionSpec)
}

func loadProjectionProjector(spec projectionSpec) (ProjectionProjectorConfig, error) {
	database, err := LoadCoreDatabaseProcess()
	if err != nil {
		return ProjectionProjectorConfig{}, err
	}
	common, err := loadProjectionCommon(database.Environment, spec)
	if err != nil {
		return ProjectionProjectorConfig{}, err
	}
	credentials, err := projectionCredentialPath(spec.runtimeCredential, common.environment)
	if err != nil {
		return ProjectionProjectorConfig{}, err
	}
	fetchWait, err := projectionDuration(spec.prefix+"_FETCH_WAIT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return ProjectionProjectorConfig{}, err
	}
	processTimeout, err := projectionDuration(spec.prefix+"_PROCESS_TIMEOUT", 100*time.Millisecond, 10*time.Minute)
	if err != nil {
		return ProjectionProjectorConfig{}, err
	}
	ackTimeout, err := projectionDuration(spec.prefix+"_ACK_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return ProjectionProjectorConfig{}, err
	}
	retryDelay, err := projectionDuration(spec.prefix+"_RETRY_DELAY", 10*time.Millisecond, time.Minute)
	if err != nil {
		return ProjectionProjectorConfig{}, err
	}
	startupTimeout, err := projectionDuration(spec.prefix+"_STARTUP_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return ProjectionProjectorConfig{}, err
	}
	shutdownTimeout, err := projectionDuration(spec.prefix+"_SHUTDOWN_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return ProjectionProjectorConfig{}, err
	}
	return ProjectionProjectorConfig{
		CoreDatabaseProcessConfig: database,
		NATS: trafficconsumer.ConnectionConfig{
			URLs: common.natsURLs, CredentialsFile: credentials, RootCAFile: common.natsRootCAFile,
			ConnectTimeout: common.connectTimeout, ReconnectWait: common.reconnectWait,
		},
		Stream: common.stream, Subject: common.subject, Durable: common.durable,
		FetchWait: fetchWait, ProcessTimeout: processTimeout, AckTimeout: ackTimeout,
		RetryDelay: retryDelay, StartupTimeout: startupTimeout, ShutdownTimeout: shutdownTimeout,
	}, nil
}

func loadProjectionConsumerProvisioner(spec projectionSpec) (ProjectionConsumerProvisionerConfig, error) {
	environment, err := loadCoreEnvironment()
	if err != nil {
		return ProjectionConsumerProvisionerConfig{}, err
	}
	common, err := loadProjectionCommon(environment, spec)
	if err != nil {
		return ProjectionConsumerProvisionerConfig{}, err
	}
	credentials, err := projectionCredentialPath(spec.provisionCredential, common.environment)
	if err != nil {
		return ProjectionConsumerProvisionerConfig{}, err
	}
	timeout, err := projectionDuration(spec.prefix+"_CONSUMER_PROVISION_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return ProjectionConsumerProvisionerConfig{}, err
	}
	ackWait, err := projectionDuration(spec.prefix+"_CONSUMER_ACK_WAIT", time.Second, 10*time.Minute)
	if err != nil {
		return ProjectionConsumerProvisionerConfig{}, err
	}
	processTimeout, err := projectionDuration(spec.prefix+"_PROCESS_TIMEOUT", 100*time.Millisecond, 10*time.Minute)
	if err != nil {
		return ProjectionConsumerProvisionerConfig{}, err
	}
	ackTimeout, err := projectionDuration(spec.prefix+"_ACK_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return ProjectionConsumerProvisionerConfig{}, err
	}
	if err := requireAckWindow(spec.prefix+"_CONSUMER_ACK_WAIT", ackWait, processTimeout, ackTimeout); err != nil {
		return ProjectionConsumerProvisionerConfig{}, err
	}
	maxWaiting, err := projectionInteger(spec.prefix+"_CONSUMER_MAX_WAITING", 1, 512)
	if err != nil {
		return ProjectionConsumerProvisionerConfig{}, err
	}
	maxRequestExpires, err := projectionDuration(spec.prefix+"_CONSUMER_MAX_REQUEST_EXPIRES", 100*time.Millisecond, time.Minute)
	if err != nil {
		return ProjectionConsumerProvisionerConfig{}, err
	}
	fetchWait, err := projectionDuration(spec.prefix+"_FETCH_WAIT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return ProjectionConsumerProvisionerConfig{}, err
	}
	if fetchWait > maxRequestExpires {
		return ProjectionConsumerProvisionerConfig{}, fmt.Errorf("%s_FETCH_WAIT must not exceed consumer max request expiry", spec.prefix)
	}
	return ProjectionConsumerProvisionerConfig{
		Environment: environment,
		NATS: trafficconsumer.ConnectionConfig{
			URLs: common.natsURLs, CredentialsFile: credentials, RootCAFile: common.natsRootCAFile,
			ConnectTimeout: common.connectTimeout, ReconnectWait: common.reconnectWait,
		},
		Timeout: timeout, Stream: common.stream,
		Consumer: jetstream.ConsumerConfig{
			Name: common.durable, Durable: common.durable, Description: spec.description,
			DeliverPolicy: jetstream.DeliverAllPolicy, AckPolicy: jetstream.AckExplicitPolicy,
			AckWait: ackWait, MaxDeliver: -1, FilterSubject: common.subject,
			ReplayPolicy: jetstream.ReplayInstantPolicy, MaxWaiting: maxWaiting,
			MaxAckPending: 1, MaxRequestBatch: 1, MaxRequestExpires: maxRequestExpires,
			Metadata: map[string]string{"peergo.owner": "core", "peergo.schema": spec.schema},
		},
	}, nil
}

func loadProjectionCommon(environment string, spec projectionSpec) (projectionCommon, error) {
	natsURLs, err := projectionNATSURLs(spec.natsURLsName, environment)
	if err != nil {
		return projectionCommon{}, err
	}
	rootCA, err := projectionOptionalAbsolutePath(spec.rootCAName)
	if err != nil {
		return projectionCommon{}, err
	}
	connectTimeout, err := projectionDuration(spec.prefix+"_NATS_CONNECT_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return projectionCommon{}, err
	}
	reconnectWait, err := projectionDuration(spec.prefix+"_NATS_RECONNECT_WAIT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return projectionCommon{}, err
	}
	stream, err := required(spec.streamName)
	if err != nil || !contract.ValidStreamName(stream) {
		return projectionCommon{}, fmt.Errorf("%s must be a valid literal JetStream name", spec.streamName)
	}
	subject, err := required(spec.subjectName)
	if err != nil || !contract.ValidLiteralSubject(subject) {
		return projectionCommon{}, fmt.Errorf("%s must be a valid literal NATS subject", spec.subjectName)
	}
	durable, err := required(spec.durableName)
	if err != nil || !contract.ValidStreamName(durable) {
		return projectionCommon{}, fmt.Errorf("%s must be a valid literal durable name", spec.durableName)
	}
	return projectionCommon{
		environment: environment, natsURLs: natsURLs, natsRootCAFile: rootCA,
		connectTimeout: connectTimeout, reconnectWait: reconnectWait,
		stream: stream, subject: subject, durable: durable,
	}, nil
}
