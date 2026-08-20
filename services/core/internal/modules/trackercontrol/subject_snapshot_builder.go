package trackercontrol

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"time"

	"github.com/peergo/peergo/contracts/go/trackersubjectcontrolv1"
)

type SubjectSnapshotPublisher interface {
	PublishSubject(context.Context, trackersubjectcontrolv1.SignedArtifact) (SnapshotPublication, error)
}

type SubjectSnapshotBuildResult struct {
	ControlSequence int64
	SubjectCount    int
	GeneratedAt     time.Time
	StateSHA256     string
	ArtifactSHA256  [32]byte
	Published       bool
}

type SubjectSnapshotBuilder struct {
	source     SubjectSnapshotSource
	publisher  SubjectSnapshotPublisher
	keyID      string
	privateKey ed25519.PrivateKey
	now        func() time.Time
}

func NewSubjectSnapshotBuilder(source SubjectSnapshotSource, publisher SubjectSnapshotPublisher, keyID string, privateKey ed25519.PrivateKey, now func() time.Time) (*SubjectSnapshotBuilder, error) {
	if source == nil || publisher == nil || trackersubjectcontrolv1.ValidateKeyID(keyID) != nil ||
		len(privateKey) != ed25519.PrivateKeySize || now == nil {
		return nil, ErrSnapshotBuilderInput
	}
	return &SubjectSnapshotBuilder{
		source: source, publisher: publisher, keyID: keyID,
		privateKey: append(ed25519.PrivateKey(nil), privateKey...), now: now,
	}, nil
}

// BuildAndPublish contains only admission identity. Promotions, ratios, H&R,
// client rules and plaintext credentials cannot enter this construction path.
func (builder *SubjectSnapshotBuilder) BuildAndPublish(ctx context.Context) (SubjectSnapshotBuildResult, error) {
	// PostgreSQL timestamptz has microsecond precision. Using the same instant
	// for the reserved revision, eligibility query and signed payload avoids a
	// false mismatch after the database round trip.
	generatedAt := builder.now().UTC().Truncate(time.Microsecond)
	projection, err := builder.source.ReadSubjectSnapshot(ctx, generatedAt)
	if err != nil {
		return SubjectSnapshotBuildResult{}, err
	}
	if !projection.GeneratedAt.Equal(generatedAt) || projection.ControlSequence < 1 {
		return SubjectSnapshotBuildResult{}, ErrSnapshotProjection
	}
	subjects := make([]trackersubjectcontrolv1.Subject, 0, len(projection.Subjects))
	for _, subject := range projection.Subjects {
		subjects = append(subjects, trackersubjectcontrolv1.Subject{
			UserID: subject.UserID.String(), NumericUserID: subject.NumericUserID, LookupHMAC: hex.EncodeToString(subject.LookupHMAC[:]),
			CredentialVersion: subject.CredentialVersion, DownloadRestricted: subject.DownloadRestricted,
		})
	}
	artifact, err := trackersubjectcontrolv1.Sign(trackersubjectcontrolv1.Snapshot{
		GeneratedAt: generatedAt, ControlSequence: projection.ControlSequence, Subjects: subjects,
	}, builder.keyID, builder.privateKey)
	if err != nil {
		return SubjectSnapshotBuildResult{}, err
	}
	publication, err := builder.publisher.PublishSubject(ctx, artifact)
	if err != nil {
		return SubjectSnapshotBuildResult{}, err
	}
	return SubjectSnapshotBuildResult{
		ControlSequence: artifact.Snapshot.ControlSequence, SubjectCount: len(artifact.Snapshot.Subjects),
		GeneratedAt: artifact.Snapshot.GeneratedAt, StateSHA256: artifact.Snapshot.StateSHA256,
		ArtifactSHA256: artifact.ArtifactSHA256, Published: publication.Published,
	}, nil
}
