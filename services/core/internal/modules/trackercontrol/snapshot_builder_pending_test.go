package trackercontrol

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSnapshotBuilderDoesNotRefreshWhileProjectionIsBehind(t *testing.T) {
	t.Parallel()
	publisher := &recordingSnapshotPublisher{}
	builder, err := NewSnapshotBuilder(
		&fixedSnapshotSource{snapshot: ProjectionSnapshot{ControlSequence: 4, PendingEvents: 1}},
		publisher, "active", snapshotTestPrivateKey(0x54), time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.BuildAndPublish(context.Background()); !errors.Is(err, ErrSnapshotProjectionPending) {
		t.Fatalf("BuildAndPublish() error = %v", err)
	}
	if publisher.artifact.Bytes != nil {
		t.Fatal("publisher called while projection had pending events")
	}
}
