// Package trackersnapshot contains delivery adapters for Core's signed Tracker
// control snapshots. It does not decide eligibility or know announce policy.
package trackersnapshot

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/peergo/peergo/contracts/go/trackercontrolv1"
	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
	"github.com/peergo/peergo/contracts/go/trackersubjectcontrolv1"
	"github.com/peergo/peergo/services/core/internal/modules/trackercontrol"
	"golang.org/x/sys/unix"
)

var (
	ErrPublicationInput    = errors.New("Tracker snapshot publication input is invalid")
	ErrPublicationUnsafe   = errors.New("Tracker snapshot publication path is unsafe")
	ErrPublicationStale    = errors.New("Tracker snapshot publication would roll back the sequence")
	ErrPublicationConflict = errors.New("Tracker snapshot publication conflicts at the same sequence")
)

type FilesystemPublisher struct {
	path     string
	lockPath string
	mu       sync.Mutex
}

// SubjectFilesystemPublisher reuses the exact same durability and monotonic
// replacement machinery while retaining a distinct subject payload schema.
type SubjectFilesystemPublisher struct {
	publisher *FilesystemPublisher
}

// RuntimePolicyFilesystemPublisher shares the hardened atomic-replacement
// machinery but validates the runtime-policy signature domain and schema.
type RuntimePolicyFilesystemPublisher struct {
	publisher *FilesystemPublisher
}

type artifactMetadata struct {
	Bytes              []byte
	KeyID              string
	PayloadSHA256      [sha256.Size]byte
	ArtifactSHA256     [sha256.Size]byte
	ControlSequence    int64
	CompletionSequence int64
	GeneratedAt        time.Time
	StateSHA256        string
}

type artifactInspection struct {
	KeyID              string
	PayloadSHA256      [sha256.Size]byte
	ControlSequence    int64
	CompletionSequence int64
	GeneratedAt        time.Time
	StateSHA256        string
}

type artifactInspector func([]byte) (artifactInspection, error)

// NewFilesystemPublisher resolves and protects a service-owned parent once.
// This first adapter is deliberately single-host; distributed publishers will
// use immutable object keys and a conditional pointer update instead.
func NewFilesystemPublisher(path string) (*FilesystemPublisher, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return nil, ErrPublicationInput
	}
	path = filepath.Clean(path)
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, fmt.Errorf("create Tracker snapshot directory: %w", err)
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return nil, fmt.Errorf("resolve Tracker snapshot directory: %w", err)
	}
	info, err := os.Stat(resolvedParent)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return nil, ErrPublicationUnsafe
	}
	target := filepath.Join(resolvedParent, filepath.Base(path))
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return nil, ErrPublicationUnsafe
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect Tracker snapshot target: %w", err)
	}
	lockPath := target + ".lock"
	if err := ensureLockFile(lockPath); err != nil {
		return nil, err
	}
	return &FilesystemPublisher{path: target, lockPath: lockPath}, nil
}

func NewSubjectFilesystemPublisher(path string) (*SubjectFilesystemPublisher, error) {
	publisher, err := NewFilesystemPublisher(path)
	if err != nil {
		return nil, err
	}
	return &SubjectFilesystemPublisher{publisher: publisher}, nil
}

func NewRuntimePolicyFilesystemPublisher(path string) (*RuntimePolicyFilesystemPublisher, error) {
	publisher, err := NewFilesystemPublisher(path)
	if err != nil {
		return nil, err
	}
	return &RuntimePolicyFilesystemPublisher{publisher: publisher}, nil
}

func (publisher *FilesystemPublisher) Publish(ctx context.Context, artifact trackercontrolv1.SignedArtifact) (trackercontrol.SnapshotPublication, error) {
	return publisher.publish(ctx, artifactMetadata{
		Bytes: artifact.Bytes, KeyID: artifact.KeyID, PayloadSHA256: artifact.PayloadSHA256,
		ArtifactSHA256: artifact.ArtifactSHA256, ControlSequence: artifact.Snapshot.ControlSequence,
		CompletionSequence: artifact.Snapshot.CompletionSequence,
		GeneratedAt:        artifact.Snapshot.GeneratedAt, StateSHA256: artifact.Snapshot.StateSHA256,
	}, inspectTorrentArtifact)
}

func (publisher *SubjectFilesystemPublisher) PublishSubject(ctx context.Context, artifact trackersubjectcontrolv1.SignedArtifact) (trackercontrol.SnapshotPublication, error) {
	return publisher.publisher.publish(ctx, artifactMetadata{
		Bytes: artifact.Bytes, KeyID: artifact.KeyID, PayloadSHA256: artifact.PayloadSHA256,
		ArtifactSHA256: artifact.ArtifactSHA256, ControlSequence: artifact.Snapshot.ControlSequence,
		GeneratedAt: artifact.Snapshot.GeneratedAt, StateSHA256: artifact.Snapshot.StateSHA256,
	}, inspectSubjectArtifact)
}

func (publisher *RuntimePolicyFilesystemPublisher) PublishRuntimePolicy(ctx context.Context, artifact trackerruntimepolicyv1.SignedArtifact) (trackercontrol.SnapshotPublication, error) {
	return publisher.publisher.publish(ctx, artifactMetadata{
		Bytes: artifact.Bytes, KeyID: artifact.KeyID, PayloadSHA256: artifact.PayloadSHA256,
		ArtifactSHA256: artifact.ArtifactSHA256, ControlSequence: artifact.Snapshot.ControlSequence,
		GeneratedAt: artifact.Snapshot.GeneratedAt, StateSHA256: artifact.Snapshot.StateSHA256,
	}, inspectRuntimePolicyArtifact)
}

func (publisher *FilesystemPublisher) publish(ctx context.Context, artifact artifactMetadata, inspect artifactInspector) (trackercontrol.SnapshotPublication, error) {
	if err := ctx.Err(); err != nil {
		return trackercontrol.SnapshotPublication{}, err
	}
	observedArtifactDigest := sha256.Sum256(artifact.Bytes)
	inspection, err := inspect(artifact.Bytes)
	if err != nil || observedArtifactDigest != artifact.ArtifactSHA256 ||
		inspection.PayloadSHA256 != artifact.PayloadSHA256 || inspection.KeyID != artifact.KeyID ||
		inspection.ControlSequence != artifact.ControlSequence || inspection.CompletionSequence != artifact.CompletionSequence ||
		!inspection.GeneratedAt.Equal(artifact.GeneratedAt) || inspection.StateSHA256 != artifact.StateSHA256 {
		return trackercontrol.SnapshotPublication{}, ErrPublicationInput
	}

	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	unlock, err := publisher.lock(ctx)
	if err != nil {
		return trackercontrol.SnapshotPublication{}, err
	}
	defer unlock()
	current, found, err := publisher.readCurrent(inspect)
	if err != nil {
		return trackercontrol.SnapshotPublication{}, err
	}
	result := trackercontrol.SnapshotPublication{}
	if found {
		result.PreviousControlSequence = current.ControlSequence
		result.PreviousCompletionSequence = current.CompletionSequence
		switch {
		case current.ControlSequence > inspection.ControlSequence:
			return trackercontrol.SnapshotPublication{}, ErrPublicationStale
		case current.CompletionSequence > inspection.CompletionSequence:
			return trackercontrol.SnapshotPublication{}, ErrPublicationStale
		case current.ControlSequence == inspection.ControlSequence &&
			current.CompletionSequence == inspection.CompletionSequence &&
			current.StateSHA256 != inspection.StateSHA256:
			return trackercontrol.SnapshotPublication{}, ErrPublicationConflict
		case current.ControlSequence == inspection.ControlSequence &&
			current.CompletionSequence == inspection.CompletionSequence &&
			!current.GeneratedAt.Before(inspection.GeneratedAt):
			return result, nil
		}
	}
	if err := publisher.replace(ctx, artifact.Bytes); err != nil {
		return trackercontrol.SnapshotPublication{}, err
	}
	result.Published = true
	return result, nil
}

func ensureLockFile(path string) error {
	for attempt := 0; attempt < 2; attempt++ {
		info, err := os.Lstat(path)
		if err == nil {
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return ErrPublicationUnsafe
			}
			if err := os.Chmod(path, 0o600); err != nil {
				return fmt.Errorf("protect Tracker snapshot lock: %w", err)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect Tracker snapshot lock: %w", err)
		}
		handle, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("create Tracker snapshot lock: %w", err)
		}
		if err := handle.Close(); err != nil {
			return fmt.Errorf("close Tracker snapshot lock: %w", err)
		}
		return nil
	}
	return ErrPublicationUnsafe
}

func (publisher *FilesystemPublisher) lock(ctx context.Context) (func(), error) {
	linkInfo, err := os.Lstat(publisher.lockPath)
	if err != nil || !linkInfo.Mode().IsRegular() || linkInfo.Mode()&os.ModeSymlink != 0 {
		return nil, ErrPublicationUnsafe
	}
	handle, err := os.OpenFile(publisher.lockPath, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open Tracker snapshot lock: %w", err)
	}
	fileInfo, err := handle.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() || !os.SameFile(linkInfo, fileInfo) {
		_ = handle.Close()
		return nil, ErrPublicationUnsafe
	}
	for {
		err = unix.Flock(int(handle.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() {
				_ = unix.Flock(int(handle.Fd()), unix.LOCK_UN)
				_ = handle.Close()
			}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			_ = handle.Close()
			return nil, fmt.Errorf("lock Tracker snapshot publication: %w", err)
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = handle.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (publisher *FilesystemPublisher) readCurrent(inspect artifactInspector) (artifactInspection, bool, error) {
	linkInfo, err := os.Lstat(publisher.path)
	if errors.Is(err, os.ErrNotExist) {
		return artifactInspection{}, false, nil
	}
	if err != nil {
		return artifactInspection{}, false, fmt.Errorf("inspect current Tracker snapshot: %w", err)
	}
	if !linkInfo.Mode().IsRegular() || linkInfo.Mode()&os.ModeSymlink != 0 ||
		linkInfo.Mode().Perm()&0o077 != 0 || linkInfo.Size() < 2 ||
		linkInfo.Size() > trackercontrolv1.MaxArtifactBytes {
		return artifactInspection{}, false, ErrPublicationUnsafe
	}
	handle, err := os.Open(publisher.path)
	if err != nil {
		return artifactInspection{}, false, fmt.Errorf("open current Tracker snapshot: %w", err)
	}
	defer handle.Close()
	fileInfo, err := handle.Stat()
	if err != nil || !fileInfo.Mode().IsRegular() || !os.SameFile(linkInfo, fileInfo) {
		return artifactInspection{}, false, ErrPublicationUnsafe
	}
	encoded, err := io.ReadAll(io.LimitReader(handle, trackercontrolv1.MaxArtifactBytes+1))
	if err != nil || len(encoded) > trackercontrolv1.MaxArtifactBytes {
		return artifactInspection{}, false, ErrPublicationUnsafe
	}
	inspection, err := inspect(encoded)
	if err != nil {
		// A valid artifact from the trusted builder may repair a truncated or
		// corrupt service-owned file left by external operational damage.
		return artifactInspection{}, false, nil
	}
	return inspection, true, nil
}

func inspectTorrentArtifact(encoded []byte) (artifactInspection, error) {
	inspection, err := trackercontrolv1.InspectUnverified(encoded)
	if err != nil {
		return artifactInspection{}, err
	}
	return artifactInspection{
		KeyID: inspection.KeyID, PayloadSHA256: inspection.PayloadSHA256,
		ControlSequence: inspection.Snapshot.ControlSequence, GeneratedAt: inspection.Snapshot.GeneratedAt,
		CompletionSequence: inspection.Snapshot.CompletionSequence,
		StateSHA256:        inspection.Snapshot.StateSHA256,
	}, nil
}

func inspectSubjectArtifact(encoded []byte) (artifactInspection, error) {
	inspection, err := trackersubjectcontrolv1.InspectUnverified(encoded)
	if err != nil {
		return artifactInspection{}, err
	}
	return artifactInspection{
		KeyID: inspection.KeyID, PayloadSHA256: inspection.PayloadSHA256,
		ControlSequence: inspection.Snapshot.ControlSequence, GeneratedAt: inspection.Snapshot.GeneratedAt,
		StateSHA256: inspection.Snapshot.StateSHA256,
	}, nil
}

func inspectRuntimePolicyArtifact(encoded []byte) (artifactInspection, error) {
	inspection, err := trackerruntimepolicyv1.InspectUnverified(encoded)
	if err != nil {
		return artifactInspection{}, err
	}
	return artifactInspection{
		KeyID: inspection.KeyID, PayloadSHA256: inspection.PayloadSHA256,
		ControlSequence: inspection.Snapshot.ControlSequence, GeneratedAt: inspection.Snapshot.GeneratedAt,
		StateSHA256: inspection.Snapshot.StateSHA256,
	}, nil
}

func (publisher *FilesystemPublisher) replace(ctx context.Context, encoded []byte) error {
	parent := filepath.Dir(publisher.path)
	temporary, err := os.CreateTemp(parent, ".peergo-tracker-snapshot-*")
	if err != nil {
		return fmt.Errorf("create temporary Tracker snapshot: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("protect temporary Tracker snapshot: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		return fmt.Errorf("write temporary Tracker snapshot: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary Tracker snapshot: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary Tracker snapshot: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, publisher.path); err != nil {
		return fmt.Errorf("publish Tracker snapshot: %w", err)
	}
	directory, err := os.Open(parent)
	if err != nil {
		return fmt.Errorf("open Tracker snapshot directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync Tracker snapshot directory: %w", err)
	}
	return nil
}

var _ trackercontrol.SnapshotPublisher = (*FilesystemPublisher)(nil)
var _ trackercontrol.SubjectSnapshotPublisher = (*SubjectFilesystemPublisher)(nil)
var _ trackercontrol.RuntimePolicySnapshotPublisher = (*RuntimePolicyFilesystemPublisher)(nil)
