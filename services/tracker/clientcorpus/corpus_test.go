package clientcorpus

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/peergo/peergo/services/tracker/internal/jetstreampublisher"
)

func TestClientCorpusRunsRealHTTPWALPipelineAndReplaysDeterministically(t *testing.T) {
	config := Config{
		Environment: "development",
		Fixture: Fixture{
			UserID: "0198f20a-6da8-7e51-9c64-111111111111", TorrentID: 42,
			InfoHashV1: "745607c7da40fbc7b073eb91bc5c595e46d81c49", TotalSizeBytes: 3 * gib,
		},
		NATSURL: "nats://127.0.0.1:4222", Stream: "PEERGO_TRACKER_ANNOUNCE_V1",
		Subject: "peergo.tracker.announce.v1", Timeout: 10 * time.Second,
	}
	sink := &memoryEvidenceSink{seen: make(map[string]uint64)}
	first, err := runWithPublisher(context.Background(), config, sink)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runWithPublisher(context.Background(), config, sink)
	if err != nil {
		t.Fatal(err)
	}
	if first.EventIDs != second.EventIDs || len(first.Published) != 4 || len(first.Requests) != 5 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	for index := range first.Published {
		if first.Published[index].Duplicate || !second.Published[index].Duplicate {
			t.Fatalf("publish %d duplicate first=%v second=%v", index, first.Published[index].Duplicate, second.Published[index].Duplicate)
		}
	}
	if !first.WAL.CanonicalPayloadsMatched || !first.WAL.CheckpointCaughtUp ||
		!first.WAL.SensitiveValuesAbsent || first.WAL.RecordCount != 4 || first.Requests[4].Accepted {
		t.Fatalf("corpus evidence = %+v requests=%+v", first.WAL, first.Requests)
	}
}

func TestClientCorpusRejectsRemoteOrNonDevelopmentConfiguration(t *testing.T) {
	config := Config{
		Environment: "production",
		Fixture:     Fixture{UserID: "x", TorrentID: 1, InfoHashV1: "x", TotalSizeBytes: 1},
		NATSURL:     "nats://nats.example:4222", Stream: "PEERGO_TRACKER_ANNOUNCE_V1", Subject: "peergo.tracker.announce.v1",
	}
	if err := validateConfig(config); err == nil {
		t.Fatal("unsafe client corpus configuration unexpectedly succeeded")
	}
}

type memoryEvidenceSink struct {
	mu       sync.Mutex
	seen     map[string]uint64
	sequence uint64
}

func (sink *memoryEvidenceSink) PublishWithEvidence(_ context.Context, eventID string, _ []byte) (jetstreampublisher.PublishEvidence, error) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sequence, duplicate := sink.seen[eventID]
	if !duplicate {
		sink.sequence++
		sequence = sink.sequence
		sink.seen[eventID] = sequence
	}
	return jetstreampublisher.PublishEvidence{
		Stream: "PEERGO_TRACKER_ANNOUNCE_V1", Sequence: sequence, Duplicate: duplicate,
	}, nil
}
