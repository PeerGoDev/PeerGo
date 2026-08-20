package trackercontrol

import (
	"context"
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/contracts/go/trackersubjectcontrolv1"
)

type subjectSnapshotSourceFixture struct {
	projection SubjectProjectionSnapshot
}

func (fixture subjectSnapshotSourceFixture) ReadSubjectSnapshot(_ context.Context, asOf time.Time) (SubjectProjectionSnapshot, error) {
	result := fixture.projection
	result.GeneratedAt = asOf
	return result, nil
}

type subjectSnapshotPublisherRecorder struct {
	artifact trackersubjectcontrolv1.SignedArtifact
}

func (recorder *subjectSnapshotPublisherRecorder) PublishSubject(_ context.Context, artifact trackersubjectcontrolv1.SignedArtifact) (SnapshotPublication, error) {
	recorder.artifact = artifact
	return SnapshotPublication{Published: true}, nil
}

func TestSubjectSnapshotBuilderPublishesMinimalAdmissionState(t *testing.T) {
	t.Parallel()
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = 0x61
	}
	now := time.Date(2026, 8, 8, 20, 30, 0, 123456789, time.UTC)
	publisher := &subjectSnapshotPublisherRecorder{}
	lookup := [32]byte{1, 2, 3}
	builder, err := NewSubjectSnapshotBuilder(subjectSnapshotSourceFixture{projection: SubjectProjectionSnapshot{
		ControlSequence: 4,
		Subjects: []SubjectAllowlistEntry{{
			UserID:     uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"),
			LookupHMAC: lookup, CredentialVersion: 2, DownloadRestricted: true,
		}},
	}}, publisher, "active", ed25519.NewKeyFromSeed(seed), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	result, err := builder.BuildAndPublish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || result.SubjectCount != 1 || result.ControlSequence != 4 ||
		!result.GeneratedAt.Equal(now.Truncate(time.Microsecond)) ||
		publisher.artifact.Snapshot.Subjects[0].CredentialVersion != 2 ||
		!publisher.artifact.Snapshot.Subjects[0].DownloadRestricted {
		t.Fatalf("result=%+v artifact=%+v", result, publisher.artifact.Snapshot)
	}
}
