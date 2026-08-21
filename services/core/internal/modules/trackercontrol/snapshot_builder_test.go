package trackercontrol

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/peergo/peergo/contracts/go/trackercontrolv1"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

type fixedSnapshotSource struct {
	snapshot ProjectionSnapshot
	err      error
}

func (source *fixedSnapshotSource) ReadSnapshot(context.Context) (ProjectionSnapshot, error) {
	return source.snapshot, source.err
}

type recordingSnapshotPublisher struct {
	artifact trackercontrolv1.SignedArtifact
	result   SnapshotPublication
	err      error
}

func (publisher *recordingSnapshotPublisher) Publish(_ context.Context, artifact trackercontrolv1.SignedArtifact) (SnapshotPublication, error) {
	publisher.artifact = artifact
	return publisher.result, publisher.err
}

func TestSnapshotBuilderSignsProjectionAndPublishesIt(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 8, 20, 0, 0, 123, time.FixedZone("UTC+8", 8*60*60))
	hash, err := torrents.ParseInfoHashV1Hex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	source := &fixedSnapshotSource{snapshot: ProjectionSnapshot{
		ControlSequence: 7, CompletionSequence: 9,
		Torrents: []AllowlistEntry{{
			TorrentID: 4, InfoHashV1: hash, TotalSizeBytes: 42,
			TorrentVersion: 3, ControlSequence: 7, UpdatedAt: now,
		}},
	}}
	publisher := &recordingSnapshotPublisher{result: SnapshotPublication{Published: true}}
	privateKey := snapshotTestPrivateKey(0x51)
	builder, err := NewSnapshotBuilder(source, publisher, "active", privateKey, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	result, err := builder.BuildAndPublish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	verified, err := trackercontrolv1.Verify(publisher.artifact.Bytes, map[string]ed25519.PublicKey{
		"active": privateKey.Public().(ed25519.PublicKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ControlSequence != 7 || result.CompletionSequence != 9 || verified.Snapshot.CompletionSequence != 9 || result.TorrentCount != 1 || !result.Published ||
		!result.GeneratedAt.Equal(now) || verified.Snapshot.Torrents[0].TorrentID != 4 {
		t.Fatalf("result=%+v snapshot=%+v", result, verified.Snapshot)
	}
}

func TestSnapshotBuilderSupportsEmptyInitialProjection(t *testing.T) {
	t.Parallel()
	publisher := &recordingSnapshotPublisher{}
	builder, err := NewSnapshotBuilder(
		&fixedSnapshotSource{snapshot: ProjectionSnapshot{}}, publisher, "initial",
		snapshotTestPrivateKey(0x52), func() time.Time { return time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := builder.BuildAndPublish(context.Background())
	if err != nil || result.ControlSequence != 0 || result.TorrentCount != 0 {
		t.Fatalf("BuildAndPublish() = %+v, %v", result, err)
	}
}

func TestSnapshotBuilderStopsBeforePublicationOnSourceFailure(t *testing.T) {
	t.Parallel()
	sourceErr := errors.New("projection unavailable")
	publisher := &recordingSnapshotPublisher{}
	builder, err := NewSnapshotBuilder(
		&fixedSnapshotSource{err: sourceErr}, publisher, "active", snapshotTestPrivateKey(0x53), time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.BuildAndPublish(context.Background()); !errors.Is(err, sourceErr) {
		t.Fatalf("BuildAndPublish() error = %v", err)
	}
	if publisher.artifact.Bytes != nil {
		t.Fatal("publisher called after source failure")
	}
}

func snapshotTestPrivateKey(fill byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = fill
	}
	return ed25519.NewKeyFromSeed(seed)
}
