// Package swarm implements the first single-process Swarm Engine adapter. It
// uses fixed shards and bounded dense peer vectors: there is no goroutine per
// torrent, no SQL/Redis access and no unbounded request queue or peer map.
package swarm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/peergo/peergo/services/tracker/internal/protocol"
)

var (
	ErrConfig   = errors.New("Swarm Engine configuration is invalid")
	ErrRequest  = errors.New("Swarm announce request is invalid")
	ErrCapacity = errors.New("Swarm Engine capacity is exhausted")
)

const (
	peerKeyDomain           = "peergo:tracker:swarm-peer-key:v1\x00"
	completionTokenDomain   = "peergo:tracker:completion-transition:v1\x00"
	maxActivePeerInspection = 1_000
	// Profile reads are infrequent service-authenticated operations. A hard
	// inspection ceiling prevents them from turning into an unbounded global
	// scan while still avoiding a second per-user activity store.
	maxUserActivePeerInspection = 100_000
)

type Config struct {
	ShardCount       int
	MaxSwarms        int64
	MaxPeers         int64
	MaxPeersPerSwarm int
	PeerTTL          time.Duration
	SweepBudget      int
}

type Request struct {
	InfoHash     [20]byte
	UserID       string
	PeerID       [20]byte
	Key          string
	Endpoint     netip.AddrPort
	ClientFamily string
	Seedbox      bool
	Left         int64
	Uploaded     int64
	Downloaded   int64
	Event        protocol.Event
	NumWant      int
	Now          time.Time
}

type Result struct {
	Complete        int
	Incomplete      int
	Peers           []protocol.Peer
	CompletionToken [32]byte
}

type Engine struct {
	config      Config
	shards      []engineShard
	totalSwarms atomic.Int64
	totalPeers  atomic.Int64
	nextSweep   atomic.Uint64
}

type engineShard struct {
	mu          sync.Mutex
	swarms      map[[20]byte]*swarmState
	swarmKeys   [][20]byte
	swarmIndex  map[[20]byte]int
	sweepCursor int
}

type swarmState struct {
	peers        []peerState
	index        map[[32]byte]int
	complete     int
	incomplete   int
	sweepCursor  int
	sampleCursor uint64
}

type peerState struct {
	key                      [32]byte
	id                       [20]byte
	endpoint                 netip.AddrPort
	userID                   string
	clientFamily             string
	seedbox                  bool
	left                     int64
	uploaded                 int64
	downloaded               int64
	uploadSpeed              int64
	downloadSpeed            int64
	lastAnnounce             time.Time
	expiresAt                time.Time
	lastCompletionDownloaded int64
	lastCompletionToken      [32]byte
}

type SnapshotEntry struct {
	InfoHash [20]byte
	Seeders  int
	Leechers int
}

type Stats struct {
	Complete   int
	Incomplete int
}

// ActivePeer is the bounded, privacy-minimized management view of one live
// in-memory connection. Network endpoints and protocol identifiers are never
// returned.
type ActivePeer struct {
	UserID        string
	ClientFamily  string
	AddressFamily int
	Seedbox       bool
	Uploaded      int64
	Downloaded    int64
	UploadSpeed   int64
	DownloadSpeed int64
	Left          int64
	LastAnnounce  time.Time
}

// UserActivePeer associates the privacy-minimized live connection with its
// swarm. The info hash is already public torrent identity; socket endpoints
// and protocol/session identifiers remain inside the Engine.
type UserActivePeer struct {
	InfoHash [20]byte
	ActivePeer
}

func NewEngine(config Config) (*Engine, error) {
	if config.ShardCount < 1 || config.ShardCount > 256 || config.MaxSwarms < 1 ||
		config.MaxPeers < 1 || config.MaxPeersPerSwarm < 2 ||
		config.MaxPeersPerSwarm > 1_000_000 || config.PeerTTL < time.Minute ||
		config.PeerTTL > 24*time.Hour || config.SweepBudget < 1 || config.SweepBudget > 4096 {
		return nil, ErrConfig
	}
	engine := &Engine{config: config, shards: make([]engineShard, config.ShardCount)}
	for index := range engine.shards {
		engine.shards[index].swarms = make(map[[20]byte]*swarmState)
		engine.shards[index].swarmIndex = make(map[[20]byte]int)
	}
	return engine, nil
}

func (engine *Engine) Announce(request Request) (Result, error) {
	if request.UserID == "" || len(request.UserID) > 64 || len(request.Key) > protocol.MaxKeyBytes ||
		request.ClientFamily == "" || len(request.ClientFamily) > 32 || !request.Endpoint.IsValid() ||
		request.Endpoint.Port() == 0 || request.Left < 0 || request.Uploaded < 0 || request.Downloaded < 0 || request.NumWant < 0 ||
		request.NumWant > 500 || request.Now.IsZero() || request.Event > protocol.EventCompleted {
		return Result{}, ErrRequest
	}
	address := request.Endpoint.Addr().Unmap()
	if !address.Is4() && !address.Is6() {
		return Result{}, ErrRequest
	}
	request.Endpoint = netip.AddrPortFrom(address, request.Endpoint.Port())
	key := derivePeerKey(request)
	shard := &engine.shards[shardIndex(request.InfoHash, len(engine.shards))]
	shard.mu.Lock()
	defer shard.mu.Unlock()

	state, exists := shard.swarms[request.InfoHash]
	if !exists {
		if request.Event == protocol.EventStopped {
			return Result{}, nil
		}
		if !reserve(&engine.totalSwarms, engine.config.MaxSwarms) {
			return Result{}, ErrCapacity
		}
		state = &swarmState{index: make(map[[32]byte]int)}
		shard.addSwarm(request.InfoHash, state)
	}
	state.sweepExpired(request.Now, engine.config.SweepBudget, &engine.totalPeers)
	result := Result{}

	if request.Event == protocol.EventStopped {
		if index, found := state.index[key]; found {
			state.removeAt(index)
			engine.totalPeers.Add(-1)
		}
	} else if index, found := state.index[key]; found {
		result.CompletionToken = state.updateAt(index, request, engine.config.PeerTTL)
	} else {
		if len(state.peers) >= engine.config.MaxPeersPerSwarm ||
			!reserve(&engine.totalPeers, engine.config.MaxPeers) {
			if len(state.peers) == 0 {
				shard.removeSwarm(request.InfoHash)
				engine.totalSwarms.Add(-1)
			}
			return Result{}, ErrCapacity
		}
		state.insert(key, request, engine.config.PeerTTL)
	}

	result.Complete = state.complete
	result.Incomplete = state.incomplete
	result.Peers = state.sample(key, request.Now, request.NumWant)
	if len(state.peers) == 0 {
		shard.removeSwarm(request.InfoHash)
		engine.totalSwarms.Add(-1)
	}
	return result, nil
}

// ActivePeers returns at most limit unexpired connections from one swarm. It
// performs no database or network access and intentionally stops once the
// bounded response is full instead of scanning an arbitrarily large swarm.
func (engine *Engine) ActivePeers(infoHash [20]byte, now time.Time, limit int) ([]ActivePeer, bool) {
	if now.IsZero() || limit < 1 || limit > 200 {
		return nil, false
	}
	shard := &engine.shards[shardIndex(infoHash, len(engine.shards))]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	state, exists := shard.swarms[infoHash]
	if !exists {
		return []ActivePeer{}, false
	}
	state.sweepExpired(now, engine.config.SweepBudget, &engine.totalPeers)
	inspectionLimit := min(len(state.peers), maxActivePeerInspection)
	peers := make([]ActivePeer, 0, min(inspectionLimit, limit+1))
	for _, peer := range state.peers[:inspectionLimit] {
		if !peer.expiresAt.After(now) {
			continue
		}
		addressFamily := 6
		if peer.endpoint.Addr().Is4() {
			addressFamily = 4
		}
		peers = append(peers, ActivePeer{
			UserID: peer.userID, ClientFamily: peer.clientFamily, AddressFamily: addressFamily,
			Seedbox: peer.seedbox, Uploaded: peer.uploaded, Downloaded: peer.downloaded,
			UploadSpeed: peer.uploadSpeed, DownloadSpeed: peer.downloadSpeed, Left: peer.left,
			LastAnnounce: peer.lastAnnounce,
		})
	}
	slices.SortFunc(peers, func(left, right ActivePeer) int { return right.LastAnnounce.Compare(left.LastAnnounce) })
	truncated := inspectionLimit < len(state.peers) || len(peers) > limit
	if len(peers) > limit {
		peers = peers[:limit]
	}
	return peers, truncated
}

// ActivePeersByUser returns a bounded current view across swarms without
// creating a durable user/peer index. Each shard is held only while copied,
// and the total inspected peer count is capped independently of Engine size.
func (engine *Engine) ActivePeersByUser(userID string, now time.Time, limit int) ([]UserActivePeer, bool) {
	if userID == "" || len(userID) > 64 || now.IsZero() || limit < 1 || limit > 200 {
		return nil, false
	}
	peers := make([]UserActivePeer, 0, limit+1)
	inspected := 0
	truncated := false

scan:
	for shardIndex := range engine.shards {
		shard := &engine.shards[shardIndex]
		shard.mu.Lock()
		for _, infoHash := range shard.swarmKeys {
			state := shard.swarms[infoHash]
			if state == nil {
				continue
			}
			state.sweepExpired(now, engine.config.SweepBudget, &engine.totalPeers)
			for _, peer := range state.peers {
				if inspected >= maxUserActivePeerInspection {
					truncated = true
					shard.mu.Unlock()
					break scan
				}
				inspected++
				if peer.userID != userID || !peer.expiresAt.After(now) {
					continue
				}
				addressFamily := 6
				if peer.endpoint.Addr().Is4() {
					addressFamily = 4
				}
				peers = append(peers, UserActivePeer{
					InfoHash: infoHash,
					ActivePeer: ActivePeer{
						UserID: peer.userID, ClientFamily: peer.clientFamily, AddressFamily: addressFamily,
						Seedbox: peer.seedbox, Uploaded: peer.uploaded, Downloaded: peer.downloaded,
						UploadSpeed: peer.uploadSpeed, DownloadSpeed: peer.downloadSpeed, Left: peer.left,
						LastAnnounce: peer.lastAnnounce,
					},
				})
				if len(peers) > limit {
					truncated = true
					slices.SortFunc(peers, func(left, right UserActivePeer) int {
						return right.LastAnnounce.Compare(left.LastAnnounce)
					})
					peers = peers[:limit]
				}
			}
		}
		shard.mu.Unlock()
	}
	slices.SortFunc(peers, func(left, right UserActivePeer) int {
		return right.LastAnnounce.Compare(left.LastAnnounce)
	})
	return peers, truncated
}

// Snapshot returns one complete, stable-order copy of all active swarm counts.
// Shards are copied one at a time, so announce handlers never wait for a global
// engine lock. The result is suitable for chunking but contains no peer data.
func (engine *Engine) Snapshot() []SnapshotEntry {
	entries := make([]SnapshotEntry, 0, int(engine.totalSwarms.Load()))
	for index := range engine.shards {
		shard := &engine.shards[index]
		shard.mu.Lock()
		for infoHash, state := range shard.swarms {
			entries = append(entries, SnapshotEntry{InfoHash: infoHash, Seeders: state.complete, Leechers: state.incomplete})
		}
		shard.mu.Unlock()
	}
	slices.SortFunc(entries, func(left, right SnapshotEntry) int { return bytes.Compare(left.InfoHash[:], right.InfoHash[:]) })
	return entries
}

// Scrape returns current active counts for one swarm after applying the same
// bounded expiry work as announce. Cumulative completion history is owned by
// Core and therefore is deliberately not fabricated inside this process.
func (engine *Engine) Scrape(infoHash [20]byte, now time.Time) Stats {
	shard := &engine.shards[shardIndex(infoHash, len(engine.shards))]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	state, exists := shard.swarms[infoHash]
	if !exists {
		return Stats{}
	}
	state.sweepExpired(now, engine.config.SweepBudget, &engine.totalPeers)
	if len(state.peers) == 0 {
		shard.removeSwarm(infoHash)
		engine.totalSwarms.Add(-1)
		return Stats{}
	}
	return Stats{Complete: state.complete, Incomplete: state.incomplete}
}

func (engine *Engine) Counts() (swarms, peers int64) {
	return engine.totalSwarms.Load(), engine.totalPeers.Load()
}

// Sweep expires peers from inactive swarms with a hard cross-shard budget.
// The dense swarm key vectors retain a stable cursor, avoiding a full map scan
// and ensuring dormant swarms eventually release global capacity.
func (engine *Engine) Sweep(now time.Time, swarmBudget int) error {
	if now.IsZero() || swarmBudget < 1 || swarmBudget > 1_000_000 {
		return ErrRequest
	}
	processed := 0
	for attempts := 0; processed < swarmBudget && attempts < swarmBudget+len(engine.shards); attempts++ {
		shardNumber := int((engine.nextSweep.Add(1) - 1) % uint64(len(engine.shards)))
		shard := &engine.shards[shardNumber]
		shard.mu.Lock()
		if len(shard.swarmKeys) == 0 {
			shard.mu.Unlock()
			continue
		}
		if shard.sweepCursor >= len(shard.swarmKeys) {
			shard.sweepCursor = 0
		}
		hash := shard.swarmKeys[shard.sweepCursor]
		state := shard.swarms[hash]
		state.sweepExpired(now, engine.config.SweepBudget, &engine.totalPeers)
		if len(state.peers) == 0 {
			shard.removeSwarm(hash)
			engine.totalSwarms.Add(-1)
		} else {
			shard.sweepCursor++
		}
		processed++
		shard.mu.Unlock()
	}
	return nil
}

func (shard *engineShard) addSwarm(hash [20]byte, state *swarmState) {
	shard.swarms[hash] = state
	shard.swarmIndex[hash] = len(shard.swarmKeys)
	shard.swarmKeys = append(shard.swarmKeys, hash)
}

func (shard *engineShard) removeSwarm(hash [20]byte) {
	index, exists := shard.swarmIndex[hash]
	if !exists {
		return
	}
	last := len(shard.swarmKeys) - 1
	delete(shard.swarms, hash)
	delete(shard.swarmIndex, hash)
	if index != last {
		moved := shard.swarmKeys[last]
		shard.swarmKeys[index] = moved
		shard.swarmIndex[moved] = index
	}
	shard.swarmKeys[last] = [20]byte{}
	shard.swarmKeys = shard.swarmKeys[:last]
	if len(shard.swarmKeys) == 0 {
		shard.sweepCursor = 0
	} else if shard.sweepCursor >= len(shard.swarmKeys) {
		shard.sweepCursor %= len(shard.swarmKeys)
	}
}

func (state *swarmState) insert(key [32]byte, request Request, ttl time.Duration) {
	state.index[key] = len(state.peers)
	state.peers = append(state.peers, peerState{
		key: key, id: request.PeerID, endpoint: request.Endpoint,
		userID: request.UserID, clientFamily: request.ClientFamily, seedbox: request.Seedbox,
		left: request.Left, uploaded: request.Uploaded, downloaded: request.Downloaded,
		lastAnnounce: request.Now, expiresAt: request.Now.Add(ttl), lastCompletionDownloaded: -1,
	})
	state.incrementClass(request.Left)
}

func (state *swarmState) updateAt(index int, request Request, ttl time.Duration) [32]byte {
	peer := &state.peers[index]
	peer.uploadSpeed = bytesPerSecond(request.Uploaded, peer.uploaded, request.Now.Sub(peer.lastAnnounce))
	peer.downloadSpeed = bytesPerSecond(request.Downloaded, peer.downloaded, request.Now.Sub(peer.lastAnnounce))
	var completionToken [32]byte
	if request.Event == protocol.EventCompleted && request.Left == 0 && peer.left > 0 {
		completionToken = deriveCompletionToken(request, peer.key)
		peer.lastCompletionDownloaded = request.Downloaded
		peer.lastCompletionToken = completionToken
	} else if request.Event == protocol.EventCompleted && request.Left == 0 && peer.left == 0 &&
		peer.lastCompletionDownloaded == request.Downloaded && peer.lastCompletionToken != ([32]byte{}) {
		completionToken = peer.lastCompletionToken
	}
	if peer.left != request.Left {
		state.decrementClass(peer.left)
		state.incrementClass(request.Left)
	}
	peer.id = request.PeerID
	peer.endpoint = request.Endpoint
	peer.userID = request.UserID
	peer.clientFamily = request.ClientFamily
	peer.seedbox = request.Seedbox
	peer.left = request.Left
	peer.uploaded = request.Uploaded
	peer.downloaded = request.Downloaded
	peer.lastAnnounce = request.Now
	peer.expiresAt = request.Now.Add(ttl)
	return completionToken
}

func bytesPerSecond(current, previous int64, elapsed time.Duration) int64 {
	if current <= previous || elapsed <= 0 {
		return 0
	}
	rate := float64(current-previous) / elapsed.Seconds()
	if rate >= math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(rate)
}

func (state *swarmState) removeAt(index int) {
	last := len(state.peers) - 1
	removed := state.peers[index]
	state.decrementClass(removed.left)
	delete(state.index, removed.key)
	if index != last {
		state.peers[index] = state.peers[last]
		state.index[state.peers[index].key] = index
	}
	state.peers[last] = peerState{}
	state.peers = state.peers[:last]
	if len(state.peers) == 0 {
		state.sweepCursor = 0
	} else if state.sweepCursor >= len(state.peers) {
		state.sweepCursor %= len(state.peers)
	}
}

func (state *swarmState) sweepExpired(now time.Time, budget int, total *atomic.Int64) {
	for inspected := 0; inspected < budget && len(state.peers) > 0; inspected++ {
		if state.sweepCursor >= len(state.peers) {
			state.sweepCursor = 0
		}
		if !state.peers[state.sweepCursor].expiresAt.After(now) {
			state.removeAt(state.sweepCursor)
			total.Add(-1)
			continue
		}
		state.sweepCursor++
	}
}

func (state *swarmState) sample(self [32]byte, now time.Time, numWant int) []protocol.Peer {
	if numWant == 0 || len(state.peers) < 2 {
		return nil
	}
	limit := min(numWant, len(state.peers)-1)
	peers := make([]protocol.Peer, 0, limit)
	start := int((binary.BigEndian.Uint64(self[:8]) + state.sampleCursor) % uint64(len(state.peers)))
	state.sampleCursor++
	maxAttempts := min(len(state.peers), numWant*4+16)
	for attempt := 0; attempt < maxAttempts && len(peers) < limit; attempt++ {
		peer := state.peers[(start+attempt)%len(state.peers)]
		if peer.key == self || !peer.expiresAt.After(now) {
			continue
		}
		peers = append(peers, protocol.Peer{ID: peer.id, Endpoint: peer.endpoint})
	}
	return peers
}

func (state *swarmState) incrementClass(left int64) {
	if left == 0 {
		state.complete++
	} else {
		state.incomplete++
	}
}

func (state *swarmState) decrementClass(left int64) {
	if left == 0 {
		state.complete--
	} else {
		state.incomplete--
	}
}

func derivePeerKey(request Request) [32]byte {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(peerKeyDomain))
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(request.UserID)))
	_, _ = hasher.Write(size[:])
	_, _ = hasher.Write([]byte(request.UserID))
	_, _ = hasher.Write(request.PeerID[:])
	binary.BigEndian.PutUint16(size[:], uint16(len(request.Key)))
	_, _ = hasher.Write(size[:])
	_, _ = hasher.Write([]byte(request.Key))
	if request.Endpoint.Addr().Is4() {
		_, _ = hasher.Write([]byte{4})
	} else {
		_, _ = hasher.Write([]byte{6})
	}
	var key [32]byte
	copy(key[:], hasher.Sum(nil))
	return key
}

// deriveCompletionToken identifies one observed completion transition, not a
// downloaded-byte value. The token is retained in peer state so a retry after
// WAL failure receives the same identity, while a later re-download gets a new
// identity even when the client reports the same absolute downloaded counter.
func deriveCompletionToken(request Request, peerKey [32]byte) [32]byte {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(completionTokenDomain))
	_, _ = hasher.Write(request.InfoHash[:])
	_, _ = hasher.Write(peerKey[:])
	var number [8]byte
	binary.BigEndian.PutUint64(number[:], uint64(request.Downloaded))
	_, _ = hasher.Write(number[:])
	binary.BigEndian.PutUint64(number[:], uint64(request.Now.UTC().UnixNano()))
	_, _ = hasher.Write(number[:])
	var token [32]byte
	copy(token[:], hasher.Sum(nil))
	return token
}

func shardIndex(infoHash [20]byte, count int) int {
	return int(binary.BigEndian.Uint64(infoHash[:8]) % uint64(count))
}

func reserve(counter *atomic.Int64, maximum int64) bool {
	for {
		current := counter.Load()
		if current >= maximum {
			return false
		}
		if counter.CompareAndSwap(current, current+1) {
			return true
		}
	}
}
