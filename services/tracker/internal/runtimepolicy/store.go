// Package runtimepolicy owns Tracker's verified, immutable request policy.
// Request handlers read one atomic view and never call Core or a database.
package runtimepolicy

import (
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
	"github.com/peergo/peergo/services/tracker/internal/control"
)

var (
	ErrConfig      = errors.New("Tracker runtime policy store configuration is invalid")
	ErrRollback    = errors.New("Tracker runtime policy rollback rejected")
	ErrDivergence  = errors.New("Tracker runtime policy diverges at the same sequence")
	ErrFromFuture  = errors.New("Tracker runtime policy timestamp is too far in the future")
	ErrUnavailable = errors.New("Tracker runtime policy is unavailable")
	ErrStale       = errors.New("Tracker runtime policy is stale")
)

type Status struct {
	Loaded          bool
	ControlSequence int64
	Revision        string
	GeneratedAt     time.Time
	KeyID           string
	StateSHA256     string
}

type LoadResult struct {
	Activated bool
	Status    Status
}

type immutableView struct {
	status Status
	policy trackerruntimepolicyv1.Policy
}

type Store struct {
	trustedKeys   map[string]ed25519.PublicKey
	maxFutureSkew time.Duration
	current       atomic.Pointer[immutableView]
}

func NewStore(trustedKeys map[string]ed25519.PublicKey, maxFutureSkew time.Duration) (*Store, error) {
	if len(trustedKeys) == 0 || maxFutureSkew < 0 || maxFutureSkew > time.Hour {
		return nil, ErrConfig
	}
	keys := make(map[string]ed25519.PublicKey, len(trustedKeys))
	for keyID, publicKey := range trustedKeys {
		if len(publicKey) != ed25519.PublicKeySize {
			return nil, ErrConfig
		}
		keys[keyID] = append(ed25519.PublicKey(nil), publicKey...)
	}
	return &Store{trustedKeys: keys, maxFutureSkew: maxFutureSkew}, nil
}

func (store *Store) LoadArtifact(encoded []byte, now time.Time) (LoadResult, error) {
	if now.IsZero() {
		return LoadResult{}, ErrConfig
	}
	verified, err := trackerruntimepolicyv1.Verify(encoded, store.trustedKeys)
	if err != nil {
		return LoadResult{}, err
	}
	if verified.Snapshot.GeneratedAt.After(now.UTC().Add(store.maxFutureSkew)) {
		return LoadResult{}, ErrFromFuture
	}
	next := &immutableView{
		status: Status{
			Loaded: true, ControlSequence: verified.Snapshot.ControlSequence,
			Revision: verified.Snapshot.Policy.Revision, GeneratedAt: verified.Snapshot.GeneratedAt,
			KeyID: verified.KeyID, StateSHA256: verified.Snapshot.StateSHA256,
		},
		policy: verified.Snapshot.Policy,
	}
	for {
		current := store.current.Load()
		if current != nil {
			switch {
			case current.status.ControlSequence > next.status.ControlSequence:
				return LoadResult{}, ErrRollback
			case current.status.ControlSequence == next.status.ControlSequence && current.status.StateSHA256 != next.status.StateSHA256:
				return LoadResult{}, ErrDivergence
			case current.status.ControlSequence == next.status.ControlSequence && !current.status.GeneratedAt.Before(next.status.GeneratedAt):
				return LoadResult{Status: current.status}, nil
			}
		}
		if store.current.CompareAndSwap(current, next) {
			return LoadResult{Activated: true, Status: next.status}, nil
		}
	}
}

func (store *Store) CurrentPolicy() (trackerruntimepolicyv1.Policy, bool) {
	view := store.current.Load()
	if view == nil {
		return trackerruntimepolicyv1.Policy{}, false
	}
	policy := view.policy
	policy.AllowedClients = append([]trackerruntimepolicyv1.ClientRule(nil), policy.AllowedClients...)
	policy.Seedbox.Rules = append([]trackerruntimepolicyv1.SeedboxRule(nil), policy.Seedbox.Rules...)
	return policy, true
}

func (store *Store) CurrentStatus() Status {
	view := store.current.Load()
	if view == nil {
		return Status{}
	}
	return view.status
}

func (store *Store) Ready(now time.Time, maxAge time.Duration) error {
	if now.IsZero() || maxAge <= 0 {
		return ErrConfig
	}
	view := store.current.Load()
	if view == nil {
		return ErrUnavailable
	}
	if view.status.GeneratedAt.After(now.UTC().Add(store.maxFutureSkew)) {
		return ErrFromFuture
	}
	if now.UTC().Sub(view.status.GeneratedAt) > maxAge {
		return ErrStale
	}
	return nil
}

type FileLoader struct {
	path  string
	store *Store
}

func NewFileLoader(path string, store *Store) (*FileLoader, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) || store == nil {
		return nil, ErrConfig
	}
	return &FileLoader{path: filepath.Clean(path), store: store}, nil
}

func (loader *FileLoader) LoadOnce(now time.Time) (LoadResult, error) {
	encoded, err := control.ReadArtifactFile(loader.path)
	if err != nil {
		return LoadResult{}, err
	}
	return loader.store.LoadArtifact(encoded, now)
}
