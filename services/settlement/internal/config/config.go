// Package config defines narrow, fail-closed configuration contracts for the
// Settlement runtime and the operator-only JetStream consumer provisioner.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/contracts/go/deploymentv1"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/services/settlement/internal/jetstreamconsumer"
)

type RuntimeConfig struct {
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
}

type ConsumerProvisionerConfig struct {
	Environment string
	NATS        jetstreamconsumer.ConnectionConfig
	Timeout     time.Duration
	Stream      string
	Consumer    jetstream.ConsumerConfig
}

type commonConfig struct {
	environment    string
	natsURLs       []string
	natsRootCAFile string
	connectTimeout time.Duration
	reconnectWait  time.Duration
	stream         string
	subject        string
	durable        string
}

func LoadRuntime() (RuntimeConfig, error) {
	common, err := loadCommon()
	if err != nil {
		return RuntimeConfig{}, err
	}
	databaseURL, err := required("PEERGO_TRACKER_DATABASE_URL")
	if err != nil || validateDatabaseURL(databaseURL, common.environment) != nil {
		return RuntimeConfig{}, errors.New("PEERGO_TRACKER_DATABASE_URL must be a PostgreSQL connection URL with a database name")
	}
	credentials, err := credentialPath("PEERGO_SETTLEMENT_NATS_CREDENTIALS_FILE", common.environment)
	if err != nil {
		return RuntimeConfig{}, err
	}
	fetchWait, err := duration("PEERGO_SETTLEMENT_FETCH_WAIT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return RuntimeConfig{}, err
	}
	processTimeout, err := duration("PEERGO_SETTLEMENT_PROCESS_TIMEOUT", 100*time.Millisecond, 10*time.Minute)
	if err != nil {
		return RuntimeConfig{}, err
	}
	ackTimeout, err := duration("PEERGO_SETTLEMENT_ACK_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return RuntimeConfig{}, err
	}
	retryDelay, err := duration("PEERGO_SETTLEMENT_RETRY_DELAY", 10*time.Millisecond, time.Minute)
	if err != nil {
		return RuntimeConfig{}, err
	}
	startupTimeout, err := duration("PEERGO_SETTLEMENT_STARTUP_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return RuntimeConfig{}, err
	}
	shutdownTimeout, err := duration("PEERGO_SETTLEMENT_SHUTDOWN_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return RuntimeConfig{}, err
	}
	return RuntimeConfig{
		Environment: common.environment, DatabaseURL: databaseURL,
		NATS: jetstreamconsumer.ConnectionConfig{
			URLs: common.natsURLs, CredentialsFile: credentials, RootCAFile: common.natsRootCAFile,
			ConnectTimeout: common.connectTimeout, ReconnectWait: common.reconnectWait,
		},
		Stream: common.stream, Subject: common.subject, Durable: common.durable,
		FetchWait: fetchWait, ProcessTimeout: processTimeout, AckTimeout: ackTimeout,
		RetryDelay: retryDelay, StartupTimeout: startupTimeout, ShutdownTimeout: shutdownTimeout,
	}, nil
}

func LoadConsumerProvisioner() (ConsumerProvisionerConfig, error) {
	common, err := loadCommon()
	if err != nil {
		return ConsumerProvisionerConfig{}, err
	}
	credentials, err := credentialPath("PEERGO_SETTLEMENT_NATS_PROVISION_CREDENTIALS_FILE", common.environment)
	if err != nil {
		return ConsumerProvisionerConfig{}, err
	}
	timeout, err := duration("PEERGO_SETTLEMENT_CONSUMER_PROVISION_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return ConsumerProvisionerConfig{}, err
	}
	ackWait, err := duration("PEERGO_SETTLEMENT_CONSUMER_ACK_WAIT", time.Second, 10*time.Minute)
	if err != nil {
		return ConsumerProvisionerConfig{}, err
	}
	processTimeout, err := duration("PEERGO_SETTLEMENT_PROCESS_TIMEOUT", 100*time.Millisecond, 10*time.Minute)
	if err != nil {
		return ConsumerProvisionerConfig{}, err
	}
	ackTimeout, err := duration("PEERGO_SETTLEMENT_ACK_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return ConsumerProvisionerConfig{}, err
	}
	if ackWait <= processTimeout+ackTimeout {
		return ConsumerProvisionerConfig{}, errors.New("PEERGO_SETTLEMENT_CONSUMER_ACK_WAIT must exceed process timeout plus ACK timeout")
	}
	maxWaiting, err := integer("PEERGO_SETTLEMENT_CONSUMER_MAX_WAITING", 1, 512)
	if err != nil {
		return ConsumerProvisionerConfig{}, err
	}
	maxRequestExpires, err := duration("PEERGO_SETTLEMENT_CONSUMER_MAX_REQUEST_EXPIRES", 100*time.Millisecond, time.Minute)
	if err != nil {
		return ConsumerProvisionerConfig{}, err
	}
	fetchWait, err := duration("PEERGO_SETTLEMENT_FETCH_WAIT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return ConsumerProvisionerConfig{}, err
	}
	if fetchWait > maxRequestExpires {
		return ConsumerProvisionerConfig{}, errors.New("PEERGO_SETTLEMENT_FETCH_WAIT must not exceed consumer max request expiry")
	}
	return ConsumerProvisionerConfig{
		Environment: common.environment,
		NATS: jetstreamconsumer.ConnectionConfig{
			URLs: common.natsURLs, CredentialsFile: credentials, RootCAFile: common.natsRootCAFile,
			ConnectTimeout: common.connectTimeout, ReconnectWait: common.reconnectWait,
		},
		Timeout: timeout, Stream: common.stream,
		Consumer: jetstream.ConsumerConfig{
			Name: common.durable, Durable: common.durable,
			Description:   "PeerGo Settlement raw Tracker ledger ingest v1",
			DeliverPolicy: jetstream.DeliverAllPolicy, AckPolicy: jetstream.AckExplicitPolicy,
			AckWait: ackWait, MaxDeliver: -1, FilterSubject: common.subject,
			ReplayPolicy: jetstream.ReplayInstantPolicy, MaxWaiting: maxWaiting,
			// One global in-flight event preserves absolute-counter order until
			// a later design introduces explicit session-key partitioning.
			MaxAckPending: 1, MaxRequestBatch: 1, MaxRequestExpires: maxRequestExpires,
			Metadata: map[string]string{
				"peergo.owner": "settlement", "peergo.schema": trackerannouncev1.SchemaVersion,
			},
		},
	}, nil
}

func loadCommon() (commonConfig, error) {
	environment, err := required("PEERGO_ENV")
	if err != nil {
		return commonConfig{}, err
	}
	if environment != "development" && environment != "production" {
		return commonConfig{}, errors.New("PEERGO_ENV must be development or production")
	}
	natsURLs, err := natsURLs(environment)
	if err != nil {
		return commonConfig{}, err
	}
	rootCA, err := optionalAbsolutePath("PEERGO_SETTLEMENT_NATS_ROOT_CA_FILE")
	if err != nil {
		return commonConfig{}, err
	}
	connectTimeout, err := duration("PEERGO_SETTLEMENT_NATS_CONNECT_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return commonConfig{}, err
	}
	reconnectWait, err := duration("PEERGO_SETTLEMENT_NATS_RECONNECT_WAIT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return commonConfig{}, err
	}
	stream, err := required("PEERGO_TRACKER_ANNOUNCE_STREAM")
	if err != nil || !trackerannouncev1.ValidStreamName(stream) {
		return commonConfig{}, errors.New("PEERGO_TRACKER_ANNOUNCE_STREAM must be a valid literal JetStream name")
	}
	subject, err := required("PEERGO_TRACKER_ANNOUNCE_SUBJECT")
	if err != nil || !trackerannouncev1.ValidLiteralSubject(subject) {
		return commonConfig{}, errors.New("PEERGO_TRACKER_ANNOUNCE_SUBJECT must be a valid literal NATS subject")
	}
	durable, err := required("PEERGO_SETTLEMENT_ANNOUNCE_DURABLE")
	if err != nil || !trackerannouncev1.ValidStreamName(durable) {
		return commonConfig{}, errors.New("PEERGO_SETTLEMENT_ANNOUNCE_DURABLE must be a valid literal durable name")
	}
	return commonConfig{
		environment: environment, natsURLs: natsURLs, natsRootCAFile: rootCA,
		connectTimeout: connectTimeout, reconnectWait: reconnectWait,
		stream: stream, subject: subject, durable: durable,
	}, nil
}

func natsURLs(environment string) ([]string, error) {
	value, err := required("PEERGO_SETTLEMENT_NATS_URLS")
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	result := make([]string, 0, strings.Count(value, ",")+1)
	mode, modeErr := deploymentv1.Load()
	if modeErr != nil {
		return nil, modeErr
	}
	for _, raw := range strings.Split(value, ",") {
		parsed, parseErr := url.Parse(strings.TrimSpace(raw))
		allowedSingleServerURL := parseErr == nil && parsed != nil && environment == "production" && mode == deploymentv1.SingleServer && parsed.Scheme == "nats" && parsed.Host == "peergo-nats:4222"
		if parseErr != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" ||
			parsed.RawQuery != "" || parsed.Fragment != "" ||
			(parsed.Scheme != "nats" && parsed.Scheme != "tls") ||
			(environment == "production" && parsed.Scheme != "tls" && !allowedSingleServerURL) {
			return nil, errors.New("PEERGO_SETTLEMENT_NATS_URLS must use credential-free tls:// URLs in production, except nats://peergo-nats:4222 in single-server mode")
		}
		canonical := parsed.String()
		if _, duplicate := seen[canonical]; duplicate {
			return nil, errors.New("PEERGO_SETTLEMENT_NATS_URLS contains a duplicate URL")
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	if len(result) == 0 {
		return nil, errors.New("PEERGO_SETTLEMENT_NATS_URLS requires at least one URL")
	}
	return result, nil
}

func credentialPath(name, environment string) (string, error) {
	value, err := optionalAbsolutePath(name)
	if err != nil {
		return "", err
	}
	if environment == "production" && value == "" {
		return "", fmt.Errorf("%s is required in production", name)
	}
	return value, nil
}

func optionalAbsolutePath(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", nil
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("%s must be absolute when configured", name)
	}
	return filepath.Clean(value), nil
}

func validateDatabaseURL(value, environment string) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" ||
		strings.Trim(parsed.Path, "/") == "" || parsed.Fragment != "" {
		return errors.New("invalid PostgreSQL URL")
	}
	if environment == "production" && parsed.Query().Get("sslmode") == "disable" {
		mode, modeErr := deploymentv1.Load()
		if modeErr != nil {
			return modeErr
		}
		if mode != deploymentv1.SingleServer || parsed.Host != "postgresql:5432" {
			return errors.New("PostgreSQL TLS can only be disabled for postgresql:5432 in single-server production")
		}
	}
	return nil
}

func duration(name string, minimum, maximum time.Duration) (time.Duration, error) {
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

func integer(name string, minimum, maximum int) (int, error) {
	value, err := required(name)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
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
