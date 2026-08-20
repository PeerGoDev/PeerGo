package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadServerBuildsBoundedRuntimeConfiguration(t *testing.T) {
	directory := t.TempDir()
	values := serverTestValues(directory)
	for name, value := range values {
		t.Setenv(name, value)
	}
	settings, err := LoadServer()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ListenAddress != "127.0.0.1:8083" || settings.MetricsListenAddress != "127.0.0.1:9093" ||
		settings.SnapshotReloadInterval != 5*time.Second ||
		settings.AnnounceInterval != 1800 || settings.MaxNumWant != 100 || settings.Swarm.ShardCount != 16 ||
		settings.Swarm.MaxPeers != 100000 || len(settings.NATSURLs) != 1 ||
		len(settings.TrustedProxyCIDRs) != 2 || settings.TrustedProxyCIDRs[0] != netip.MustParsePrefix("192.0.2.10/32") ||
		settings.TrustedProxyCIDRs[1] != netip.MustParsePrefix("2001:db8::10/128") ||
		settings.AnnounceStream != "PEERGO_TRACKER_ANNOUNCE_V1" || settings.PublishTimeout != 3*time.Second ||
		settings.SwarmSnapshotStream != "PEERGO_TRACKER_SWARM_SNAPSHOT_V1" || settings.SwarmRoutingEpoch != 1 {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestLoadServerRejectsUnsafeTrustedProxyCIDRs(t *testing.T) {
	for name, value := range map[string]string{
		"host_without_prefix": "192.0.2.10",
		"non_canonical":       "192.0.2.10/24",
		"overlapping":         "192.0.2.0/24,192.0.2.10/32",
		"mapped_ipv4":         "::ffff:192.0.2.10/128",
		"empty_member":        "192.0.2.10/32,",
	} {
		t.Run(name, func(t *testing.T) {
			values := serverTestValues(t.TempDir())
			values["PEERGO_TRACKER_TRUSTED_PROXY_CIDRS"] = value
			for envName, envValue := range values {
				t.Setenv(envName, envValue)
			}
			if _, err := LoadServer(); err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
				t.Fatalf("LoadServer() error = %v", err)
			}
		})
	}
}

func TestLoadServerRequiresAuthenticatedTLSNATSInProduction(t *testing.T) {
	values := serverTestValues(t.TempDir())
	values["PEERGO_ENV"] = "production"
	values["PEERGO_TRACKER_NATS_URLS"] = "tls://nats.internal:4222"
	for name, value := range values {
		t.Setenv(name, value)
	}
	if _, err := LoadServer(); err == nil || !strings.Contains(err.Error(), "NATS_CREDENTIALS_FILE is required") {
		t.Fatalf("LoadServer() error = %v", err)
	}
	t.Setenv("PEERGO_TRACKER_NATS_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "tracker.creds"))
	t.Setenv("PEERGO_TRACKER_NATS_URLS", "nats://nats.internal:4222")
	if _, err := LoadServer(); err == nil || !strings.Contains(err.Error(), "tls://") {
		t.Fatalf("LoadServer() insecure URL error = %v", err)
	}
}

func TestLoadServerAllowsFixedSingleServerNATS(t *testing.T) {
	values := serverTestValues(t.TempDir())
	values["PEERGO_ENV"] = "production"
	values["PEERGO_DEPLOYMENT_MODE"] = "single-server"
	values["PEERGO_TRACKER_NATS_URLS"] = "nats://peergo-nats:4222"
	values["PEERGO_TRACKER_NATS_CREDENTIALS_FILE"] = filepath.Join(t.TempDir(), "single-server.creds")
	for name, value := range values {
		t.Setenv(name, value)
	}
	if _, err := LoadServer(); err != nil {
		t.Fatalf("single-server NATS rejected: %v", err)
	}
	t.Setenv("PEERGO_TRACKER_NATS_URLS", "nats://other-nats:4222")
	if _, err := LoadServer(); err == nil {
		t.Fatal("single-server accepted another clear-text NATS host")
	}
}

func serverTestValues(directory string) map[string]string {
	publicKey := make([]byte, ed25519.PublicKeySize)
	return map[string]string{
		"PEERGO_ENV":                                      "development",
		"PEERGO_TRACKER_SNAPSHOT_PATH":                    filepath.Join(directory, "control.snapshot"),
		"PEERGO_TRACKER_SUBJECT_SNAPSHOT_PATH":            filepath.Join(directory, "subjects.snapshot"),
		"PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_PATH":     filepath.Join(directory, "runtime-policy.snapshot"),
		"PEERGO_TRACKER_SNAPSHOT_TRUSTED_KEYS":            "active=" + base64.StdEncoding.EncodeToString(publicKey),
		"PEERGO_TRACKER_SNAPSHOT_MAX_AGE":                 "15m",
		"PEERGO_TRACKER_SUBJECT_SNAPSHOT_MAX_AGE":         "1m",
		"PEERGO_TRACKER_RUNTIME_POLICY_SNAPSHOT_MAX_AGE":  "15m",
		"PEERGO_TRACKER_SNAPSHOT_MAX_FUTURE_SKEW":         "30s",
		"PEERGO_TRACKER_PASSKEY_LOOKUP_KEY":               "tracker-passkey-lookup-key-test-2026",
		"PEERGO_TRACKER_LISTEN_ADDRESS":                   "127.0.0.1:8083",
		"PEERGO_TRACKER_METRICS_LISTEN_ADDRESS":           "127.0.0.1:9093",
		"PEERGO_TRACKER_TRUSTED_PROXY_CIDRS":              "192.0.2.10/32, 2001:db8::10/128",
		"PEERGO_TRACKER_SERVICE_TOKEN":                    "peergo-test-tracker-service-token-2026",
		"PEERGO_TRACKER_SNAPSHOT_RELOAD_INTERVAL":         "5s",
		"PEERGO_TRACKER_SHUTDOWN_TIMEOUT":                 "10s",
		"PEERGO_TRACKER_ANNOUNCE_INTERVAL_SECONDS":        "1800",
		"PEERGO_TRACKER_MIN_ANNOUNCE_INTERVAL_SECONDS":    "900",
		"PEERGO_TRACKER_DEFAULT_NUMWANT":                  "50",
		"PEERGO_TRACKER_MAX_NUMWANT":                      "100",
		"PEERGO_TRACKER_SWARM_SHARDS":                     "16",
		"PEERGO_TRACKER_MAX_SWARMS":                       "10000",
		"PEERGO_TRACKER_MAX_PEERS":                        "100000",
		"PEERGO_TRACKER_MAX_PEERS_PER_SWARM":              "10000",
		"PEERGO_TRACKER_PEER_TTL":                         "35m",
		"PEERGO_TRACKER_SWEEP_BUDGET":                     "64",
		"PEERGO_TRACKER_WAL_PATH":                         filepath.Join(directory, "announce.wal"),
		"PEERGO_TRACKER_WAL_MAX_BYTES":                    "10485760",
		"PEERGO_TRACKER_WAL_COMPACT_AT_BYTES":             "1048576",
		"PEERGO_TRACKER_NATS_URLS":                        "nats://127.0.0.1:4222",
		"PEERGO_TRACKER_NATS_CREDENTIALS_FILE":            "",
		"PEERGO_TRACKER_NATS_ROOT_CA_FILE":                "",
		"PEERGO_TRACKER_NATS_CONNECT_TIMEOUT":             "2s",
		"PEERGO_TRACKER_NATS_RECONNECT_WAIT":              "1s",
		"PEERGO_TRACKER_ANNOUNCE_STREAM":                  "PEERGO_TRACKER_ANNOUNCE_V1",
		"PEERGO_TRACKER_ANNOUNCE_SUBJECT":                 "peergo.tracker.announce.v1",
		"PEERGO_TRACKER_ANNOUNCE_PUBLISH_TIMEOUT":         "3s",
		"PEERGO_TRACKER_ANNOUNCE_PUBLISH_RETRY_MIN":       "100ms",
		"PEERGO_TRACKER_ANNOUNCE_PUBLISH_RETRY_MAX":       "10s",
		"PEERGO_TRACKER_SWARM_SWEEP_INTERVAL":             "30s",
		"PEERGO_TRACKER_SWARM_SWEEP_SWARM_BUDGET":         "1000",
		"PEERGO_TRACKER_SWARM_SNAPSHOT_STREAM":            "PEERGO_TRACKER_SWARM_SNAPSHOT_V1",
		"PEERGO_TRACKER_SWARM_SNAPSHOT_SUBJECT":           "peergo.tracker.swarm.snapshot.v1",
		"PEERGO_TRACKER_SWARM_SNAPSHOT_SOURCE_ID":         "tracker-primary",
		"PEERGO_TRACKER_SWARM_ROUTING_EPOCH":              "1",
		"PEERGO_TRACKER_SWARM_SEQUENCE_PATH":              filepath.Join(directory, "swarm-sequence.json"),
		"PEERGO_TRACKER_SWARM_SNAPSHOT_INTERVAL":          "30s",
		"PEERGO_TRACKER_SWARM_SNAPSHOT_MAX_CHUNK_ENTRIES": "1000",
	}
}
