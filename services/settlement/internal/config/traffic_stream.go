package config

import (
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/settlementhnrv1"
	"github.com/peergo/peergo/contracts/go/settlementseedingv1"
	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
	"github.com/peergo/peergo/services/settlement/internal/jetstreamconsumer"
)

type TrafficStreamProvisionerConfig struct {
	Environment string
	NATS        jetstreamconsumer.ConnectionConfig
	Timeout     time.Duration
	Stream      jetstream.StreamConfig
}

type HNRStreamProvisionerConfig = TrafficStreamProvisionerConfig
type SeedingEvidenceStreamProvisionerConfig = TrafficStreamProvisionerConfig

func LoadTrafficStreamProvisioner() (TrafficStreamProvisionerConfig, error) {
	return loadResultStreamProvisioner(
		"PEERGO_SETTLEMENT_TRAFFIC", "PEERGO_SETTLEMENT_NATS_TRAFFIC_PROVISION_CREDENTIALS_FILE",
		trafficStreamIdentity, "PeerGo final traffic settlement results v1", settlementtrafficv1.SchemaVersion,
		settlementtrafficv1.MaxEventBytes,
	)
}

func LoadHNRStreamProvisioner() (HNRStreamProvisionerConfig, error) {
	return loadResultStreamProvisioner(
		"PEERGO_SETTLEMENT_HNR", "PEERGO_SETTLEMENT_NATS_HNR_PROVISION_CREDENTIALS_FILE",
		hnrStreamIdentity, "PeerGo final H&R obligation snapshots v1", settlementhnrv1.SchemaVersion,
		settlementhnrv1.MaxEventBytes,
	)
}

func LoadSeedingEvidenceStreamProvisioner() (SeedingEvidenceStreamProvisionerConfig, error) {
	return loadResultStreamProvisioner(
		"PEERGO_SETTLEMENT_SEEDING_EVIDENCE", "PEERGO_SETTLEMENT_NATS_SEEDING_EVIDENCE_PROVISION_CREDENTIALS_FILE",
		seedingEvidenceStreamIdentity, "PeerGo closed hourly seeding reward evidence v1",
		settlementseedingv1.SchemaVersion, settlementseedingv1.MaxEventBytes,
	)
}

func loadResultStreamProvisioner(prefix, credentialName string, identity func() (string, string, error), description, schema string, maxMessageBytes int32) (TrafficStreamProvisionerConfig, error) {
	environment, err := settlementEnvironment()
	if err != nil {
		return TrafficStreamProvisionerConfig{}, err
	}
	connection, err := loadResultNATSConnection(environment, credentialName)
	if err != nil {
		return TrafficStreamProvisionerConfig{}, err
	}
	streamName, subject, err := identity()
	if err != nil {
		return TrafficStreamProvisionerConfig{}, err
	}
	timeout, err := duration(prefix+"_STREAM_PROVISION_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return TrafficStreamProvisionerConfig{}, err
	}
	maxBytes, err := integer64(prefix+"_STREAM_MAX_BYTES", 1<<20, 1<<50)
	if err != nil {
		return TrafficStreamProvisionerConfig{}, err
	}
	maxAge, err := duration(prefix+"_STREAM_MAX_AGE", time.Hour, 90*24*time.Hour)
	if err != nil {
		return TrafficStreamProvisionerConfig{}, err
	}
	duplicates, err := duration(prefix+"_STREAM_DUPLICATE_WINDOW", time.Second, time.Hour)
	if err != nil {
		return TrafficStreamProvisionerConfig{}, err
	}
	replicas, err := integer(prefix+"_STREAM_REPLICAS", 1, 5)
	if err != nil {
		return TrafficStreamProvisionerConfig{}, err
	}
	return TrafficStreamProvisionerConfig{
		Environment: environment, NATS: connection, Timeout: timeout,
		Stream: jetstream.StreamConfig{
			Name: streamName, Description: description, Subjects: []string{subject},
			Retention: jetstream.LimitsPolicy, MaxConsumers: 4, MaxMsgs: -1, MaxBytes: maxBytes,
			Discard: jetstream.DiscardNew, MaxAge: maxAge, MaxMsgsPerSubject: -1,
			MaxMsgSize: maxMessageBytes, Storage: jetstream.FileStorage, Replicas: replicas, Duplicates: duplicates,
			Metadata: map[string]string{"peergo.owner": "settlement", "peergo.schema": schema},
		},
	}, nil
}

func integer64(name string, minimum, maximum int64) (int64, error) {
	value, err := required(name)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return parsed, nil
}
