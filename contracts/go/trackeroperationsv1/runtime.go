// Package trackeroperationsv1 defines the bounded, deployment-safe runtime
// view that Tracker may expose to Core. It deliberately excludes credentials,
// filesystem paths, stream names and network addresses.
package trackeroperationsv1

import (
	"time"

	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
)

// Runtime is the effective configuration of the running Tracker process.
// Values are read-only because changing them requires a coordinated process
// rollout rather than an untracked browser-side mutation.
type Runtime struct {
	GeneratedAt                time.Time                            `json:"generated_at"`
	PolicyGeneratedAt          time.Time                            `json:"policy_generated_at"`
	PolicyControlSequence      int64                                `json:"policy_control_sequence"`
	PolicyRevision             string                               `json:"policy_revision"`
	AnnounceIntervalSeconds    int64                                `json:"announce_interval_seconds"`
	MinAnnounceIntervalSeconds int64                                `json:"min_announce_interval_seconds"`
	DefaultNumWant             int64                                `json:"default_numwant"`
	MaxNumWant                 int64                                `json:"max_numwant"`
	ScrapeEnabled              bool                                 `json:"scrape_enabled"`
	MaxScrapeHashes            int64                                `json:"max_scrape_hashes"`
	ClientMode                 string                               `json:"client_mode"`
	AllowedClients             []trackerruntimepolicyv1.ClientRule  `json:"allowed_clients"`
	UserRequestsPerMinute      int64                                `json:"user_requests_per_minute"`
	UserBurst                  int64                                `json:"user_burst"`
	AddressRequestsPerMinute   int64                                `json:"address_requests_per_minute"`
	AddressBurst               int64                                `json:"address_burst"`
	Seedbox                    trackerruntimepolicyv1.SeedboxPolicy `json:"seedbox"`
	PeerTTLSeconds             int64                                `json:"peer_ttl_seconds"`
	MaxSwarms                  int64                                `json:"max_swarms"`
	MaxPeers                   int64                                `json:"max_peers"`
	MaxPeersPerSwarm           int64                                `json:"max_peers_per_swarm"`
}

// Valid rejects malformed or internally inconsistent cross-service data.
func (runtime Runtime) Valid() bool {
	return !runtime.GeneratedAt.IsZero() &&
		!runtime.PolicyGeneratedAt.IsZero() && runtime.PolicyControlSequence >= 1 && runtime.PolicyRevision != "" &&
		runtime.AnnounceIntervalSeconds >= 60 && runtime.AnnounceIntervalSeconds <= 86_400 &&
		runtime.MinAnnounceIntervalSeconds >= 30 &&
		runtime.MinAnnounceIntervalSeconds <= runtime.AnnounceIntervalSeconds &&
		runtime.DefaultNumWant >= 0 && runtime.DefaultNumWant <= runtime.MaxNumWant &&
		runtime.MaxNumWant >= 1 && runtime.MaxNumWant <= 500 &&
		runtime.MaxScrapeHashes >= 1 && runtime.MaxScrapeHashes <= 100 &&
		(runtime.ClientMode == string(trackerruntimepolicyv1.ClientModeAllowAll) || runtime.ClientMode == string(trackerruntimepolicyv1.ClientModeAllowList)) &&
		runtime.UserRequestsPerMinute >= 1 && runtime.UserBurst >= 1 &&
		runtime.AddressRequestsPerMinute >= 1 && runtime.AddressBurst >= 1 &&
		runtime.Seedbox.UploadFactorBasisPoints >= 0 && runtime.Seedbox.UploadFactorBasisPoints <= 10_000 &&
		runtime.Seedbox.SeedboxSpeedLimitBytesPerSecond >= 0 && runtime.Seedbox.StandardSpeedLimitBytesPerSecond >= 0 &&
		runtime.PeerTTLSeconds > runtime.AnnounceIntervalSeconds && runtime.PeerTTLSeconds <= 86_400 &&
		runtime.MaxSwarms >= 1 && runtime.MaxPeers >= 1 && runtime.MaxPeersPerSwarm >= 2
}
