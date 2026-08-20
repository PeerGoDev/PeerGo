// Package subjectcontrol owns Tracker's verified, immutable user admission
// view. It derives the same domain-separated HMAC as Privacy Vault and never
// stores, logs or returns the plaintext route passkey.
package subjectcontrol

import (
	"crypto/ed25519"
	"errors"
	"sync/atomic"
	"time"

	"github.com/peergo/peergo/contracts/go/trackerpasskeyv1"
	"github.com/peergo/peergo/contracts/go/trackersubjectcontrolv1"
)

var (
	ErrStoreConfig         = errors.New("Tracker subject store configuration is invalid")
	ErrSnapshotRollback    = errors.New("Tracker subject snapshot sequence rollback rejected")
	ErrSnapshotDivergence  = errors.New("Tracker subject snapshot diverges at the same sequence")
	ErrSnapshotFromFuture  = errors.New("Tracker subject snapshot timestamp is too far in the future")
	ErrSnapshotUnavailable = errors.New("Tracker subject snapshot is unavailable")
	ErrSnapshotStale       = errors.New("Tracker subject snapshot is stale")
)

type Status struct {
	Loaded          bool
	ControlSequence int64
	GeneratedAt     time.Time
	SubjectCount    int
	KeyID           string
	StateSHA256     string
}

type LoadResult struct {
	Activated bool
	Status    Status
}

type Admission struct {
	Subject         trackersubjectcontrolv1.Subject
	ControlSequence int64
}

type immutableView struct {
	status   Status
	subjects map[[32]byte]trackersubjectcontrolv1.Subject
}

type Store struct {
	trustedKeys   map[string]ed25519.PublicKey
	lookupKey     []byte
	maxFutureSkew time.Duration
	current       atomic.Pointer[immutableView]
}

func NewStore(trustedKeys map[string]ed25519.PublicKey, lookupKey []byte, maxFutureSkew time.Duration) (*Store, error) {
	if len(trustedKeys) == 0 || len(lookupKey) < trackerpasskeyv1.LookupKeyMin ||
		maxFutureSkew < 0 || maxFutureSkew > time.Hour {
		return nil, ErrStoreConfig
	}
	keys := make(map[string]ed25519.PublicKey, len(trustedKeys))
	for keyID, publicKey := range trustedKeys {
		if trackersubjectcontrolv1.ValidateKeyID(keyID) != nil || len(publicKey) != ed25519.PublicKeySize {
			return nil, ErrStoreConfig
		}
		keys[keyID] = append(ed25519.PublicKey(nil), publicKey...)
	}
	return &Store{
		trustedKeys: keys, lookupKey: append([]byte(nil), lookupKey...), maxFutureSkew: maxFutureSkew,
	}, nil
}

// LoadArtifact performs verification and builds a complete replacement map
// before its single CAS. Request readers can never observe a partial refresh.
func (store *Store) LoadArtifact(encoded []byte, now time.Time) (LoadResult, error) {
	if now.IsZero() {
		return LoadResult{}, ErrStoreConfig
	}
	verified, err := trackersubjectcontrolv1.Verify(encoded, store.trustedKeys)
	if err != nil {
		return LoadResult{}, err
	}
	if verified.Snapshot.GeneratedAt.After(now.UTC().Add(store.maxFutureSkew)) {
		return LoadResult{}, ErrSnapshotFromFuture
	}
	subjects := make(map[[32]byte]trackersubjectcontrolv1.Subject, len(verified.Snapshot.Subjects))
	for _, subject := range verified.Snapshot.Subjects {
		lookup, err := trackersubjectcontrolv1.DecodeLookupHMAC(subject.LookupHMAC)
		if err != nil {
			return LoadResult{}, err
		}
		subjects[lookup] = subject
	}
	next := &immutableView{
		status: Status{
			Loaded: true, ControlSequence: verified.Snapshot.ControlSequence,
			GeneratedAt: verified.Snapshot.GeneratedAt, SubjectCount: len(subjects),
			KeyID: verified.KeyID, StateSHA256: verified.Snapshot.StateSHA256,
		},
		subjects: subjects,
	}
	for {
		current := store.current.Load()
		if current != nil {
			switch {
			case current.status.ControlSequence > next.status.ControlSequence:
				return LoadResult{}, ErrSnapshotRollback
			case current.status.ControlSequence == next.status.ControlSequence &&
				current.status.StateSHA256 != next.status.StateSHA256:
				return LoadResult{}, ErrSnapshotDivergence
			case current.status.ControlSequence == next.status.ControlSequence &&
				!current.status.GeneratedAt.Before(next.status.GeneratedAt):
				return LoadResult{Status: current.status}, nil
			}
		}
		if store.current.CompareAndSwap(current, next) {
			return LoadResult{Activated: true, Status: next.status}, nil
		}
	}
}

// LookupPasskey validates the canonical route format or the one isolated PtYes
// migration profile, derives its keyed lookup locally, and returns only the
// signed internal subject.
func (store *Store) LookupPasskey(passkey string) (trackersubjectcontrolv1.Subject, bool) {
	admission, found := store.LookupAdmission(passkey)
	return admission.Subject, found
}

// LookupAdmission derives the HMAC and returns the subject together with the
// exact immutable snapshot sequence used for the decision.
func (store *Store) LookupAdmission(passkey string) (Admission, bool) {
	view := store.current.Load()
	if view == nil {
		return Admission{}, false
	}
	lookup, err := trackerpasskeyv1.LookupHMACAccepted(store.lookupKey, passkey)
	if err != nil {
		return Admission{}, false
	}
	subject, exists := view.subjects[lookup]
	if !exists {
		return Admission{}, false
	}
	return Admission{Subject: subject, ControlSequence: view.status.ControlSequence}, true
}

func (store *Store) Current() Status {
	view := store.current.Load()
	if view == nil {
		return Status{}
	}
	return view.status
}

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
