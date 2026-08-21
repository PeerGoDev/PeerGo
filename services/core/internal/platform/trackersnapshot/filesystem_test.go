package trackersnapshot

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/trackercontrolv1"
)

func TestFilesystemPublisherPublishesAndRefreshesMonotonically(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "control.snapshot")
	publisher, err := NewFilesystemPublisher(path)
	if err != nil {
		t.Fatal(err)
	}
	first := signedSnapshot(t, 4, time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	result, err := publisher.Publish(context.Background(), first)
	if err != nil || !result.Published {
		t.Fatalf("first Publish() = %+v, %v", result, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %v, error = %v", info.Mode().Perm(), err)
	}

	replay, err := publisher.Publish(context.Background(), first)
	if err != nil || replay.Published || replay.PreviousControlSequence != 4 {
		t.Fatalf("replay Publish() = %+v, %v", replay, err)
	}
	refresh := signedSnapshot(t, 4, first.Snapshot.GeneratedAt.Add(time.Minute), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	refreshed, err := publisher.Publish(context.Background(), refresh)
	if err != nil || !refreshed.Published {
		t.Fatalf("refresh Publish() = %+v, %v", refreshed, err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil || string(encoded) != string(refresh.Bytes) {
		t.Fatalf("published bytes differ, error = %v", err)
	}
}

func TestFilesystemPublisherRejectsRollbackAndSameSequenceDivergence(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	publisher, err := NewFilesystemPublisher(filepath.Join(directory, "control.snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	current := signedSnapshot(t, 5, time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if _, err := publisher.Publish(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(context.Background(), signedSnapshot(t, 4, current.Snapshot.GeneratedAt.Add(time.Minute), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")); !errors.Is(err, ErrPublicationStale) {
		t.Fatalf("rollback error = %v", err)
	}
	if _, err := publisher.Publish(context.Background(), signedSnapshot(t, 5, current.Snapshot.GeneratedAt.Add(time.Minute), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")); !errors.Is(err, ErrPublicationConflict) {
		t.Fatalf("divergence error = %v", err)
	}
}

func TestFilesystemPublisherOrdersCompletionStatisticsIndependently(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	publisher, err := NewFilesystemPublisher(filepath.Join(directory, "control.snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	initial := signedSnapshotWithCompletion(t, 5, 0, 0, now, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if _, err := publisher.Publish(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	refresh := signedSnapshotWithCompletion(t, 5, 1, 1, now.Add(time.Minute), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	result, err := publisher.Publish(context.Background(), refresh)
	if err != nil || !result.Published || result.PreviousCompletionSequence != 0 {
		t.Fatalf("completion refresh = %+v, %v", result, err)
	}
	rollback := signedSnapshotWithCompletion(t, 6, 0, 0, now.Add(2*time.Minute), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if _, err := publisher.Publish(context.Background(), rollback); !errors.Is(err, ErrPublicationStale) {
		t.Fatalf("completion rollback error = %v", err)
	}
	divergent := signedSnapshotWithCompletion(t, 5, 1, 1, now.Add(2*time.Minute), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if _, err := publisher.Publish(context.Background(), divergent); !errors.Is(err, ErrPublicationConflict) {
		t.Fatalf("same completion sequence divergence error = %v", err)
	}
}

func TestFilesystemPublisherRejectsSymlinkTarget(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	realPath := filepath.Join(directory, "real.snapshot")
	if err := os.WriteFile(realPath, []byte("not-a-snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(directory, "control.snapshot")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilesystemPublisher(linkPath); !errors.Is(err, ErrPublicationUnsafe) {
		t.Fatalf("NewFilesystemPublisher() error = %v", err)
	}
}

func TestFilesystemPublisherRejectsWorldReadableParent(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilesystemPublisher(filepath.Join(directory, "control.snapshot")); !errors.Is(err, ErrPublicationUnsafe) {
		t.Fatalf("insecure parent error = %v", err)
	}
}

func signedSnapshot(t *testing.T, sequence int64, generatedAt time.Time, infoHash string) trackercontrolv1.SignedArtifact {
	return signedSnapshotWithCompletion(t, sequence, 0, 0, generatedAt, infoHash)
}

func signedSnapshotWithCompletion(t *testing.T, sequence, completionSequence, completedDownloads int64, generatedAt time.Time, infoHash string) trackercontrolv1.SignedArtifact {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = 0x61
	}
	artifact, err := trackercontrolv1.Sign(trackercontrolv1.Snapshot{
		GeneratedAt: generatedAt, ControlSequence: sequence, CompletionSequence: completionSequence,
		Torrents: []trackercontrolv1.Torrent{{
			TorrentID:  1,
			InfoHashV1: infoHash, TotalSizeBytes: 42, CompletedDownloads: completedDownloads,
			TorrentVersion: 2, ControlSequence: sequence,
		}},
	}, "active", ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatal(err)
	}
	return artifact
}
