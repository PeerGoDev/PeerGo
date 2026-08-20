package trackercontrol

import (
	"context"
	"crypto/ed25519"
	"errors"
	"time"

	"github.com/peergo/peergo/contracts/go/trackercontrolv1"
)

var ErrSnapshotBuilderInput = errors.New("Tracker snapshot builder input is invalid")

type SnapshotPublication struct {
	Published               bool
	PreviousControlSequence int64
}

type SnapshotPublisher interface {
	Publish(ctx context.Context, artifact trackercontrolv1.SignedArtifact) (SnapshotPublication, error)
}

type SnapshotBuildResult struct {
	ControlSequence int64
	TorrentCount    int
	GeneratedAt     time.Time
	StateSHA256     string
	ArtifactSHA256  [32]byte
	Published       bool
}

type SnapshotBuilder struct {
	source     SnapshotSource
	publisher  SnapshotPublisher
	keyID      string
	privateKey ed25519.PrivateKey
	now        func() time.Time
}

func NewSnapshotBuilder(source SnapshotSource, publisher SnapshotPublisher, keyID string, privateKey ed25519.PrivateKey, now func() time.Time) (*SnapshotBuilder, error) {
	if source == nil || publisher == nil || trackercontrolv1.ValidateKeyID(keyID) != nil ||
		len(privateKey) != ed25519.PrivateKeySize || now == nil {
		return nil, ErrSnapshotBuilderInput
	}
	return &SnapshotBuilder{
		source: source, publisher: publisher, keyID: keyID,
		privateKey: append(ed25519.PrivateKey(nil), privateKey...), now: now,
	}, nil
}

// BuildAndPublish signs only the ordered Tracker eligibility projection. The
// builder intentionally has no dependency on promotions, user entitlements or
// announce state, preventing those policies from silently changing admission.
func (builder *SnapshotBuilder) BuildAndPublish(ctx context.Context) (SnapshotBuildResult, error) {
	projection, err := builder.source.ReadSnapshot(ctx)
	if err != nil {
		return SnapshotBuildResult{}, err
	}
	if projection.PendingEvents != 0 {
		return SnapshotBuildResult{}, ErrSnapshotProjectionPending
	}
	torrents := make([]trackercontrolv1.Torrent, 0, len(projection.Torrents))
	for _, entry := range projection.Torrents {
		torrents = append(torrents, trackercontrolv1.Torrent{
			TorrentID: int64(entry.TorrentID), InfoHashV1: entry.InfoHashV1.Hex(),
			TotalSizeBytes: entry.TotalSizeBytes, CompletedDownloads: entry.CompletedDownloads,
			TorrentVersion: entry.TorrentVersion, ControlSequence: entry.ControlSequence,
		})
	}
	artifact, err := trackercontrolv1.Sign(trackercontrolv1.Snapshot{
		GeneratedAt: builder.now(), ControlSequence: projection.ControlSequence, Torrents: torrents,
	}, builder.keyID, builder.privateKey)
	if err != nil {
		return SnapshotBuildResult{}, err
	}
	publication, err := builder.publisher.Publish(ctx, artifact)
	if err != nil {
		return SnapshotBuildResult{}, err
	}
	return SnapshotBuildResult{
		ControlSequence: artifact.Snapshot.ControlSequence, TorrentCount: len(artifact.Snapshot.Torrents),
		GeneratedAt: artifact.Snapshot.GeneratedAt, StateSHA256: artifact.Snapshot.StateSHA256,
		ArtifactSHA256: artifact.ArtifactSHA256, Published: publication.Published,
	}, nil
}
