// Package control owns Tracker's verified, immutable admission view. Announce
// handlers may query this package in memory; they must not query Core or a
// database on the request path.
package control

import (
	"crypto/ed25519"
	"errors"
	"sync/atomic"
	"time"

	"github.com/peergo/peergo/contracts/go/trackercontrolv1"
)

var (
	ErrStoreConfig         = errors.New("Tracker control store configuration is invalid")
	ErrSnapshotRollback    = errors.New("Tracker control snapshot sequence rollback rejected")
	ErrSnapshotDivergence  = errors.New("Tracker control snapshot diverges at the same sequence")
	ErrSnapshotFromFuture  = errors.New("Tracker control snapshot timestamp is too far in the future")
	ErrSnapshotUnavailable = errors.New("Tracker control snapshot is unavailable")
	ErrSnapshotStale       = errors.New("Tracker control snapshot is stale")
)

type Status struct {
	Loaded             bool
	ControlSequence    int64
	CompletionSequence int64
	GeneratedAt        time.Time
	TorrentCount       int
	KeyID              string
	StateSHA256        string
}

type LoadResult struct {
	Activated bool
	Status    Status
}

type Admission struct {
	Torrent         trackercontrolv1.Torrent
	ControlSequence int64
}

type immutableView struct {
	status   Status
	torrents map[[20]byte]trackercontrolv1.Torrent
}

type Store struct {
	trustedKeys   map[string]ed25519.PublicKey
	maxFutureSkew time.Duration
	current       atomic.Pointer[immutableView]
}

func NewStore(trustedKeys map[string]ed25519.PublicKey, maxFutureSkew time.Duration) (*Store, error) {
	if len(trustedKeys) == 0 || maxFutureSkew < 0 || maxFutureSkew > time.Hour {
		return nil, ErrStoreConfig
	}
	keys := make(map[string]ed25519.PublicKey, len(trustedKeys))
	for keyID, publicKey := range trustedKeys {
		if trackercontrolv1.ValidateKeyID(keyID) != nil || len(publicKey) != ed25519.PublicKeySize {
			return nil, ErrStoreConfig
		}
		keys[keyID] = append(ed25519.PublicKey(nil), publicKey...)
	}
	return &Store{trustedKeys: keys, maxFutureSkew: maxFutureSkew}, nil
}

// LoadArtifact performs every expensive or fallible operation before the CAS.
// Readers therefore observe either the complete old map or the complete new
// map, never a partially cleared/repopulated allowlist.
func (store *Store) LoadArtifact(encoded []byte, now time.Time) (LoadResult, error) {
	if now.IsZero() {
		return LoadResult{}, ErrStoreConfig
	}
	verified, err := trackercontrolv1.Verify(encoded, store.trustedKeys)
	if err != nil {
		return LoadResult{}, err
	}
	if verified.Snapshot.GeneratedAt.After(now.UTC().Add(store.maxFutureSkew)) {
		return LoadResult{}, ErrSnapshotFromFuture
	}
	torrents := make(map[[20]byte]trackercontrolv1.Torrent, len(verified.Snapshot.Torrents))
	for _, torrent := range verified.Snapshot.Torrents {
		infoHash, err := trackercontrolv1.DecodeInfoHash(torrent.InfoHashV1)
		if err != nil {
			return LoadResult{}, err
		}
		torrents[infoHash] = torrent
	}
	next := &immutableView{
		status: Status{
			Loaded: true, ControlSequence: verified.Snapshot.ControlSequence,
			CompletionSequence: verified.Snapshot.CompletionSequence,
			GeneratedAt:        verified.Snapshot.GeneratedAt, TorrentCount: len(torrents),
			KeyID: verified.KeyID, StateSHA256: verified.Snapshot.StateSHA256,
		},
		torrents: torrents,
	}
	for {
		current := store.current.Load()
		if current != nil {
			switch {
			case current.status.ControlSequence > next.status.ControlSequence:
				return LoadResult{}, ErrSnapshotRollback
			case current.status.CompletionSequence > next.status.CompletionSequence:
				return LoadResult{}, ErrSnapshotRollback
			case current.status.ControlSequence == next.status.ControlSequence &&
				current.status.CompletionSequence == next.status.CompletionSequence &&
				current.status.StateSHA256 != next.status.StateSHA256:
				return LoadResult{}, ErrSnapshotDivergence
			case current.status.ControlSequence == next.status.ControlSequence &&
				current.status.CompletionSequence == next.status.CompletionSequence &&
				!current.status.GeneratedAt.Before(next.status.GeneratedAt):
				return LoadResult{Status: current.status}, nil
			}
		}
		if store.current.CompareAndSwap(current, next) {
			return LoadResult{Activated: true, Status: next.status}, nil
		}
	}
}

func (store *Store) LookupTorrent(infoHash [20]byte) (trackercontrolv1.Torrent, bool) {
	admission, found := store.LookupAdmission(infoHash)
	return admission.Torrent, found
}

// LookupAdmission binds the torrent entry to the exact immutable snapshot
// sequence observed by this request, so a concurrent refresh cannot make the
// durable announce event claim a different control version.
func (store *Store) LookupAdmission(infoHash [20]byte) (Admission, bool) {
	view := store.current.Load()
	if view == nil {
		return Admission{}, false
	}
	torrent, exists := view.torrents[infoHash]
	if !exists {
		return Admission{}, false
	}
	return Admission{Torrent: torrent, ControlSequence: view.status.ControlSequence}, true
}

func (store *Store) Current() Status {
	view := store.current.Load()
	if view == nil {
		return Status{}
	}
	return view.status
}

// Ready controls load-balancer admission. A stale view remains available to
// existing readers while the node reports not-ready, avoiding a destructive
// clear that could turn an operational problem into inconsistent admission.
func (store *Store) Ready(now time.Time, maxAge time.Duration) error {
	if now.IsZero() || maxAge <= 0 {
		return ErrStoreConfig
	}
	view := store.current.Load()
	if view == nil {
		return ErrSnapshotUnavailable
	}
	if view.status.GeneratedAt.After(now.UTC().Add(store.maxFutureSkew)) {
		return ErrSnapshotFromFuture
	}
	if now.UTC().Sub(view.status.GeneratedAt) > maxAge {
		return ErrSnapshotStale
	}
	return nil
}
