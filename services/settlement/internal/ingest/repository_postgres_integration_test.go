package ingest

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackerannouncev2"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
)

// This gated test needs a disposable, migrated Tracker Ledger database. It
// proves that v2 keeps only bounded cursors while preserving interval source
// provenance and exact replay behavior.
func TestIntegrationV2AdvancesCursorsWithoutPermanentPayloadInbox(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_TRACKER_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_TRACKER_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatal(err)
	}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	stream := "PEERGO_SETTLEMENT_V2_IT_" + strings.ToUpper(suffix)
	subject := "peergo.settlement.v2.it." + suffix
	producerID := "tracker-it-" + suffix
	producerEpoch, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	userID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	baseTime := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	baseline := integrationV2Event(t, baseTime, userID, 100, 200, 1000, "started")
	interval := integrationV2Event(t, baseTime.Add(10*time.Second), userID, 600, 500, 0, "completed")
	interval.SessionToken = baseline.SessionToken
	repository, err := NewPostgresRepository(pool, stream, subject, time.Now)
	if err != nil {
		t.Fatal(err)
	}

	var intervalPayload []byte
	for index, event := range []trackerannouncev1.Event{baseline, interval} {
		sequenced, encodeErr := trackerannouncev2.FromV1(event, producerID, producerEpoch.String(), int64(index+1))
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		payload, encodeErr := trackerannouncev2.Encode(sequenced)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if index == 1 {
			intervalPayload = payload
		}
		result, processErr := repository.Process(ctx, Delivery{
			Stream: stream, Subject: subject, Sequence: uint64(index + 1), DeliveryCount: 1, Payload: payload,
		})
		if processErr != nil || result.Duplicate {
			t.Fatalf("process v2 event %d: result=%+v error=%v", index+1, result, processErr)
		}
	}

	var inboxCount, rawCount int
	var streamSequence, producerSequence int64
	var rawStream, rawProducer string
	if err := pool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM settlement.event_inbox WHERE source_stream = $1),
    (SELECT count(*) FROM ledger.raw_session_intervals WHERE event_id = $2::uuid),
    (SELECT last_source_sequence FROM settlement.ingest_stream_cursors WHERE source_stream = $1),
    (SELECT last_producer_sequence FROM settlement.ingest_producer_cursors
        WHERE producer_id = $3 AND producer_epoch = $4::uuid),
    (SELECT source_stream FROM ledger.raw_session_intervals WHERE event_id = $2::uuid),
    (SELECT producer_id FROM ledger.raw_session_intervals WHERE event_id = $2::uuid)`,
		stream, interval.EventID, producerID, producerEpoch).Scan(
		&inboxCount, &rawCount, &streamSequence, &producerSequence, &rawStream, &rawProducer,
	); err != nil {
		t.Fatal(err)
	}
	if inboxCount != 0 || rawCount != 1 || streamSequence != 2 || producerSequence != 2 ||
		rawStream != stream || rawProducer != producerID {
		t.Fatalf("v2 persistence inbox=%d raw=%d stream_seq=%d producer_seq=%d raw_stream=%q raw_producer=%q",
			inboxCount, rawCount, streamSequence, producerSequence, rawStream, rawProducer)
	}
	replayed, err := repository.Process(ctx, Delivery{
		Stream: stream, Subject: subject, Sequence: 2, DeliveryCount: 2, Payload: intervalPayload,
	})
	if err != nil || !replayed.Duplicate || replayed.Outcome != OutcomeInterval {
		t.Fatalf("replay result=%+v error=%v", replayed, err)
	}
}

func integrationV2Event(
	t *testing.T,
	receivedAt time.Time,
	userID uuid.UUID,
	uploaded, downloaded, left int64,
	kind string,
) trackerannouncev1.Event {
	t.Helper()
	eventID, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	event := trackerannouncev1.Event{
		SchemaVersion:          trackerannouncev1.SchemaVersion,
		EventID:                eventID.String(),
		ReceivedAt:             receivedAt,
		UserID:                 userID.String(),
		TorrentID:              42,
		InfoHashV1:             strings.Repeat("ab", 20),
		SessionToken:           strings.Repeat("cd", 32),
		AddressFamily:          4,
		Event:                  kind,
		Uploaded:               uploaded,
		Downloaded:             downloaded,
		Left:                   left,
		CredentialVersion:      1,
		TorrentControlSequence: 7,
		SubjectControlSequence: 9,
	}
	if kind == "completed" && left == 0 {
		event.CompletionID = strings.Repeat("ef", 32)
	}
	return event
}
