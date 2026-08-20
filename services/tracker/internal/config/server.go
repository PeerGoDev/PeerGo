package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/peergo/peergo/contracts/go/deploymentv1"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/tracker/internal/jetstreampublisher"
	"github.com/peergo/peergo/services/tracker/internal/swarm"
)

type ServerConfig struct {
	Control                Config
	ListenAddress          string
	MetricsListenAddress   string
	TrustedProxyCIDRs      []netip.Prefix
	ServiceToken           string
	SnapshotReloadInterval time.Duration
	ShutdownTimeout        time.Duration
	AnnounceInterval       int
	MinAnnounceInterval    int
	DefaultNumWant         int
	MaxNumWant             int
	WALPath                string
	MaxWALBytes            int64
	WALCompactAtBytes      int64
	NATSURLs               []string
	NATSCredentialsFile    string
	NATSRootCAFile         string
	NATSConnectTimeout     time.Duration
	NATSReconnectWait      time.Duration
	AnnounceStream         string
	AnnounceSubject        string
	PublishTimeout         time.Duration
	PublishRetryMinimum    time.Duration
	PublishRetryMaximum    time.Duration
	SwarmSweepInterval     time.Duration
	SwarmSweepBudget       int
	SwarmSnapshotStream    string
	SwarmSnapshotSubject   string
	SwarmSnapshotSourceID  string
	SwarmRoutingEpoch      int64
	SwarmSequencePath      string
	SwarmSnapshotInterval  time.Duration
	SwarmMaxChunkEntries   int
	Swarm                  swarm.Config
}

func LoadServer() (ServerConfig, error) {
	control, err := Load()
	if err != nil {
		return ServerConfig{}, err
	}
	listenAddress, err := parseListenAddress("PEERGO_TRACKER_LISTEN_ADDRESS")
	if err != nil {
		return ServerConfig{}, err
	}
	metricsListenAddress, err := parseListenAddress("PEERGO_TRACKER_METRICS_LISTEN_ADDRESS")
	if err != nil {
		return ServerConfig{}, err
	}
	if metricsListenAddress == listenAddress {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_METRICS_LISTEN_ADDRESS must differ from the public Tracker listen address")
	}
	trustedProxyCIDRs, err := parseTrustedProxyCIDRs()
	if err != nil {
		return ServerConfig{}, err
	}
	serviceToken, err := required("PEERGO_TRACKER_SERVICE_TOKEN")
	if err != nil {
		return ServerConfig{}, err
	}
	if len(serviceToken) < 32 {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_SERVICE_TOKEN must contain at least 32 bytes")
	}
	reloadInterval, err := parseDuration("PEERGO_TRACKER_SNAPSHOT_RELOAD_INTERVAL", time.Second, time.Minute)
	if err != nil {
		return ServerConfig{}, err
	}
	if reloadInterval > control.SubjectMaxAge/2 {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_SNAPSHOT_RELOAD_INTERVAL must not exceed half the subject snapshot max age")
	}
	shutdownTimeout, err := parseDuration("PEERGO_TRACKER_SHUTDOWN_TIMEOUT", time.Second, time.Minute)
	if err != nil {
		return ServerConfig{}, err
	}
	announceInterval, err := parseInteger("PEERGO_TRACKER_ANNOUNCE_INTERVAL_SECONDS", 60, 86400)
	if err != nil {
		return ServerConfig{}, err
	}
	minInterval, err := parseInteger("PEERGO_TRACKER_MIN_ANNOUNCE_INTERVAL_SECONDS", 30, announceInterval)
	if err != nil {
		return ServerConfig{}, err
	}
	defaultNumWant, err := parseInteger("PEERGO_TRACKER_DEFAULT_NUMWANT", 0, 500)
	if err != nil {
		return ServerConfig{}, err
	}
	maxNumWant, err := parseInteger("PEERGO_TRACKER_MAX_NUMWANT", 1, 500)
	if err != nil {
		return ServerConfig{}, err
	}
	if defaultNumWant > maxNumWant {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_DEFAULT_NUMWANT must not exceed PEERGO_TRACKER_MAX_NUMWANT")
	}
	shardCount, err := parseInteger("PEERGO_TRACKER_SWARM_SHARDS", 1, 256)
	if err != nil {
		return ServerConfig{}, err
	}
	maxSwarms, err := parseInteger64("PEERGO_TRACKER_MAX_SWARMS", 1, 10_000_000)
	if err != nil {
		return ServerConfig{}, err
	}
	maxPeers, err := parseInteger64("PEERGO_TRACKER_MAX_PEERS", 1, 100_000_000)
	if err != nil {
		return ServerConfig{}, err
	}
	maxPeersPerSwarm, err := parseInteger("PEERGO_TRACKER_MAX_PEERS_PER_SWARM", 2, 1_000_000)
	if err != nil {
		return ServerConfig{}, err
	}
	peerTTL, err := parseDuration("PEERGO_TRACKER_PEER_TTL", time.Minute, 24*time.Hour)
	if err != nil {
		return ServerConfig{}, err
	}
	if peerTTL <= time.Duration(announceInterval)*time.Second {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_PEER_TTL must exceed the announce interval")
	}
	sweepBudget, err := parseInteger("PEERGO_TRACKER_SWEEP_BUDGET", 1, 4096)
	if err != nil {
		return ServerConfig{}, err
	}
	walPath, err := required("PEERGO_TRACKER_WAL_PATH")
	if err != nil {
		return ServerConfig{}, err
	}
	if !filepath.IsAbs(walPath) {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_WAL_PATH must be absolute")
	}
	maxWALBytes, err := parseInteger64("PEERGO_TRACKER_WAL_MAX_BYTES", 1<<20, 1<<40)
	if err != nil {
		return ServerConfig{}, err
	}
	walCompactAtBytes, err := parseInteger64("PEERGO_TRACKER_WAL_COMPACT_AT_BYTES", 1<<20, maxWALBytes)
	if err != nil {
		return ServerConfig{}, err
	}
	if walCompactAtBytes > maxWALBytes/2 {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_WAL_COMPACT_AT_BYTES must not exceed half the WAL byte limit")
	}
	natsURLs, err := parseNATSURLs(control.Environment)
	if err != nil {
		return ServerConfig{}, err
	}
	natsCredentialsFile, err := optionalAbsolutePath("PEERGO_TRACKER_NATS_CREDENTIALS_FILE")
	if err != nil {
		return ServerConfig{}, err
	}
	if control.Environment == "production" && natsCredentialsFile == "" {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_NATS_CREDENTIALS_FILE is required in production")
	}
	natsRootCAFile, err := optionalAbsolutePath("PEERGO_TRACKER_NATS_ROOT_CA_FILE")
	if err != nil {
		return ServerConfig{}, err
	}
	natsConnectTimeout, err := parseDuration("PEERGO_TRACKER_NATS_CONNECT_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return ServerConfig{}, err
	}
	natsReconnectWait, err := parseDuration("PEERGO_TRACKER_NATS_RECONNECT_WAIT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return ServerConfig{}, err
	}
	announceStream, err := required("PEERGO_TRACKER_ANNOUNCE_STREAM")
	if err != nil || !jetstreampublisher.ValidStreamName(announceStream) {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_ANNOUNCE_STREAM must be a valid literal JetStream name")
	}
	announceSubject, err := required("PEERGO_TRACKER_ANNOUNCE_SUBJECT")
	if err != nil || !jetstreampublisher.ValidLiteralSubject(announceSubject) {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_ANNOUNCE_SUBJECT must be a valid literal NATS subject")
	}
	publishTimeout, err := parseDuration("PEERGO_TRACKER_ANNOUNCE_PUBLISH_TIMEOUT", 100*time.Millisecond, time.Minute)
	if err != nil {
		return ServerConfig{}, err
	}
	publishRetryMinimum, err := parseDuration("PEERGO_TRACKER_ANNOUNCE_PUBLISH_RETRY_MIN", 10*time.Millisecond, time.Minute)
	if err != nil {
		return ServerConfig{}, err
	}
	publishRetryMaximum, err := parseDuration("PEERGO_TRACKER_ANNOUNCE_PUBLISH_RETRY_MAX", publishRetryMinimum, 5*time.Minute)
	if err != nil {
		return ServerConfig{}, err
	}
	swarmSweepInterval, err := parseDuration("PEERGO_TRACKER_SWARM_SWEEP_INTERVAL", time.Second, 10*time.Minute)
	if err != nil {
		return ServerConfig{}, err
	}
	swarmSweepBudget, err := parseInteger("PEERGO_TRACKER_SWARM_SWEEP_SWARM_BUDGET", 1, 1_000_000)
	if err != nil {
		return ServerConfig{}, err
	}
	swarmSnapshotStream, err := required("PEERGO_TRACKER_SWARM_SNAPSHOT_STREAM")
	if err != nil || !trackerswarmv1.ValidStreamName(swarmSnapshotStream) {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_SWARM_SNAPSHOT_STREAM must be a valid literal JetStream name")
	}
	swarmSnapshotSubject, err := required("PEERGO_TRACKER_SWARM_SNAPSHOT_SUBJECT")
	if err != nil || !trackerswarmv1.ValidLiteralSubject(swarmSnapshotSubject) {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_SWARM_SNAPSHOT_SUBJECT must be a valid literal NATS subject")
	}
	swarmSnapshotSourceID, err := required("PEERGO_TRACKER_SWARM_SNAPSHOT_SOURCE_ID")
	if err != nil || !trackerswarmv1.ValidSourceID(swarmSnapshotSourceID) {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_SWARM_SNAPSHOT_SOURCE_ID must be a stable lowercase source ID")
	}
	swarmRoutingEpoch, err := parseInteger64("PEERGO_TRACKER_SWARM_ROUTING_EPOCH", 1, 1<<62)
	if err != nil {
		return ServerConfig{}, err
	}
	swarmSequencePath, err := required("PEERGO_TRACKER_SWARM_SEQUENCE_PATH")
	if err != nil || !filepath.IsAbs(swarmSequencePath) {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_SWARM_SEQUENCE_PATH must be absolute")
	}
	swarmSequencePath = filepath.Clean(swarmSequencePath)
	if swarmSequencePath == filepath.Clean(walPath) {
		return ServerConfig{}, errors.New("PEERGO_TRACKER_SWARM_SEQUENCE_PATH must differ from the announce WAL path")
	}
	swarmSnapshotInterval, err := parseDuration("PEERGO_TRACKER_SWARM_SNAPSHOT_INTERVAL", 5*time.Second, 10*time.Minute)
	if err != nil {
		return ServerConfig{}, err
	}
	swarmMaxChunkEntries, err := parseInteger("PEERGO_TRACKER_SWARM_SNAPSHOT_MAX_CHUNK_ENTRIES", 1, trackerswarmv1.MaxChunkEntries)
	if err != nil {
		return ServerConfig{}, err
	}
	return ServerConfig{
		Control: control, ListenAddress: listenAddress, MetricsListenAddress: metricsListenAddress,
		TrustedProxyCIDRs: trustedProxyCIDRs,
		ServiceToken:      serviceToken, SnapshotReloadInterval: reloadInterval,
		ShutdownTimeout: shutdownTimeout, AnnounceInterval: announceInterval,
		MinAnnounceInterval: minInterval, DefaultNumWant: defaultNumWant, MaxNumWant: maxNumWant,
		WALPath: filepath.Clean(walPath), MaxWALBytes: maxWALBytes, WALCompactAtBytes: walCompactAtBytes,
		NATSURLs: natsURLs, NATSCredentialsFile: natsCredentialsFile, NATSRootCAFile: natsRootCAFile,
		NATSConnectTimeout: natsConnectTimeout, NATSReconnectWait: natsReconnectWait,
		AnnounceStream: announceStream, AnnounceSubject: announceSubject,
		PublishTimeout: publishTimeout, PublishRetryMinimum: publishRetryMinimum, PublishRetryMaximum: publishRetryMaximum,
		SwarmSweepInterval: swarmSweepInterval, SwarmSweepBudget: swarmSweepBudget,
		SwarmSnapshotStream: swarmSnapshotStream, SwarmSnapshotSubject: swarmSnapshotSubject,
		SwarmSnapshotSourceID: swarmSnapshotSourceID, SwarmRoutingEpoch: swarmRoutingEpoch,
		SwarmSequencePath: swarmSequencePath, SwarmSnapshotInterval: swarmSnapshotInterval,
		SwarmMaxChunkEntries: swarmMaxChunkEntries,
		Swarm: swarm.Config{
			ShardCount: shardCount, MaxSwarms: maxSwarms, MaxPeers: maxPeers,
			MaxPeersPerSwarm: maxPeersPerSwarm, PeerTTL: peerTTL, SweepBudget: sweepBudget,
		},
	}, nil
}

func parseTrustedProxyCIDRs() ([]netip.Prefix, error) {
	const maximumTrustedProxyCIDRs = 16
	value := strings.TrimSpace(os.Getenv("PEERGO_TRACKER_TRUSTED_PROXY_CIDRS"))
	if value == "" {
		return nil, nil
	}
	rawPrefixes := strings.Split(value, ",")
	if len(rawPrefixes) > maximumTrustedProxyCIDRs {
		return nil, fmt.Errorf("PEERGO_TRACKER_TRUSTED_PROXY_CIDRS must contain at most %d CIDRs", maximumTrustedProxyCIDRs)
	}
	prefixes := make([]netip.Prefix, 0, len(rawPrefixes))
	for _, raw := range rawPrefixes {
		candidate := strings.TrimSpace(raw)
		prefix, err := netip.ParsePrefix(candidate)
		if err != nil || prefix.Addr().Is4In6() || prefix != prefix.Masked() {
			return nil, errors.New("PEERGO_TRACKER_TRUSTED_PROXY_CIDRS must contain canonical IPv4 or IPv6 CIDRs")
		}
		for _, existing := range prefixes {
			if existing.Contains(prefix.Addr()) || prefix.Contains(existing.Addr()) {
				return nil, errors.New("PEERGO_TRACKER_TRUSTED_PROXY_CIDRS must not contain duplicate or overlapping CIDRs")
			}
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

func parseListenAddress(name string) (string, error) {
	value, err := required(name)
	if err != nil {
		return "", err
	}
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		return "", fmt.Errorf("%s must be a host:port address", name)
	}
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		return "", fmt.Errorf("%s must contain a non-zero port", name)
	}
	return value, nil
}

func parseNATSURLs(environment string) ([]string, error) {
	value, err := required("PEERGO_TRACKER_NATS_URLS")
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
		parsed, err := url.Parse(strings.TrimSpace(raw))
		allowedSingleServerURL := err == nil && parsed != nil && environment == "production" && mode == deploymentv1.SingleServer && parsed.Scheme == "nats" && parsed.Host == "peergo-nats:4222"
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
			(parsed.Scheme != "nats" && parsed.Scheme != "tls") || (environment == "production" && parsed.Scheme != "tls" && !allowedSingleServerURL) {
			return nil, errors.New("PEERGO_TRACKER_NATS_URLS must use credential-free tls:// URLs in production, except nats://peergo-nats:4222 in single-server mode")
		}
		canonical := parsed.String()
		if _, duplicate := seen[canonical]; duplicate {
			return nil, errors.New("PEERGO_TRACKER_NATS_URLS contains a duplicate URL")
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	if len(result) == 0 {
		return nil, errors.New("PEERGO_TRACKER_NATS_URLS requires at least one URL")
	}
	return result, nil
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

func parseInteger(name string, minimum, maximum int) (int, error) {
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

func parseInteger64(name string, minimum, maximum int64) (int64, error) {
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
