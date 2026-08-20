package hnr_test

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/services/settlement/internal/hnr"
	"github.com/peergo/peergo/services/settlement/internal/hnrpolicy"
	"github.com/peergo/peergo/services/settlement/internal/ingest"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
	"github.com/peergo/peergo/services/settlement/internal/timeline"
)

// This gated test requires a disposable, fully migrated Tracker Ledger. It
// verifies the transaction/trigger boundary that pure progress tests cannot:
// a post-cutover completion snapshots policy once, emits versioned outbox
// events, and becomes satisfied after two trustworthy seeding intervals.
func TestIntegrationHNRCompletionProgressAndImmutablePolicy(t *testing.T) {
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

	baseTime := time.Now().UTC().Add(-6 * time.Hour).Truncate(time.Second)
	userID := uuid.New()
	torrentID := positiveRandomInt64(t)
	repository, err := hnr.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	scopeUserID := userID
	revision := hnrpolicy.Revision{
		ID: uuid.New(), Scope: timeline.Scope{UserID: &scopeUserID}, EffectiveAt: baseTime.Add(-time.Minute),
		Policy: hnrpolicy.Policy{
			Rule: hnrpolicy.RuleRef{ID: "integration-hnr-v1", Version: 1}, Mode: hnrpolicy.ModeEnforced,
			RequiredSeedSeconds: 7200, RequiredRatioBasisPoints: 20_000,
			AssessmentWindowSeconds: 7200, GracePeriodSeconds: 3600, MaxIntervalCreditSeconds: 5400,
		},
	}
	if created, appendErr := repository.AppendRevision(ctx, revision, baseTime); appendErr != nil || !created {
		t.Fatalf("AppendRevision() created=%v error=%v", created, appendErr)
	}

	stream := "PEERGO_HNR_IT_" + strings.ToUpper(strings.ReplaceAll(uuid.NewString()[:8], "-", ""))
	subject := "peergo.hnr.it." + strings.ToLower(strings.ReplaceAll(uuid.NewString()[:8], "-", ""))
	ingestRepository, err := ingest.NewPostgresRepository(pool, stream, subject, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	baseline, completion, seedOne, seedTwo, completionID := hnrIntegrationEvents(t, baseTime, userID, torrentID)
	sequence := uint64(1)
	ingestEvent(t, ctx, ingestRepository, stream, subject, sequence, baseline)
	sequence++
	ingestEvent(t, ctx, ingestRepository, stream, subject, sequence, completion)
	sequence++

	workerNow := baseTime.Add(8 * time.Hour)
	worker, err := hnr.NewWorker(repository, hnr.WorkerConfig{
		LeaseDuration: time.Minute, IdleInterval: 100 * time.Millisecond, RetryBase: 100 * time.Millisecond,
	}, func() time.Time { return workerNow }, nil)
	if err != nil {
		t.Fatal(err)
	}
	runHNRWork(t, ctx, worker)
	assertHNRProjection(t, ctx, pool, completionID, "tracking", 1, 0, 1)

	ingestEvent(t, ctx, ingestRepository, stream, subject, sequence, seedOne)
	sequence++
	workerNow = workerNow.Add(time.Second)
	runHNRWork(t, ctx, worker)
	assertHNRProjection(t, ctx, pool, completionID, "tracking", 2, 3600, 2)

	ingestEvent(t, ctx, ingestRepository, stream, subject, sequence, seedTwo)
	workerNow = workerNow.Add(time.Second)
	runHNRWork(t, ctx, worker)
	assertHNRProjection(t, ctx, pool, completionID, "satisfied", 3, 7200, 3)

	conflicting := revision
	conflicting.ID = uuid.New()
	conflicting.EffectiveAt = revision.EffectiveAt.Add(30 * time.Second)
	if _, err := repository.AppendRevision(ctx, conflicting, workerNow); err == nil ||
		(!errors.Is(err, hnr.ErrInvariant) && !errors.Is(err, hnr.ErrTimelineConflict)) {
		t.Fatalf("retroactive AppendRevision() error=%v", err)
	}
}

func hnrIntegrationEvents(t *testing.T, baseTime time.Time, userID uuid.UUID, torrentID int64) (trackerannouncev1.Event, trackerannouncev1.Event, trackerannouncev1.Event, trackerannouncev1.Event, string) {
	t.Helper()
	infoHash := randomHex(t, 20)
	sessionToken := randomHex(t, 32)
	completionID := randomHex(t, 32)
	baseline := trackerannouncev1.Event{
		SchemaVersion: trackerannouncev1.SchemaVersion, EventID: newV7(t).String(), ReceivedAt: baseTime,
		UserID: userID.String(), TorrentID: torrentID, InfoHashV1: infoHash, SessionToken: sessionToken,
		AddressFamily: 4, Event: "started", Uploaded: 100, Downloaded: 1000, Left: 1000,
		CredentialVersion: 1, TorrentControlSequence: 7, SubjectControlSequence: 9,
	}
	completion := baseline
	completion.EventID = newV7(t).String()
	completion.ReceivedAt = baseTime.Add(10 * time.Second)
	completion.Event = "completed"
	completion.CompletionID = completionID
	completion.Left = 0
	seedOne := completion
	seedOne.EventID = newV7(t).String()
	seedOne.ReceivedAt = completion.ReceivedAt.Add(time.Hour)
	seedOne.Event = ""
	seedOne.CompletionID = ""
	seedTwo := seedOne
	seedTwo.EventID = newV7(t).String()
	seedTwo.ReceivedAt = seedOne.ReceivedAt.Add(time.Hour)
	return baseline, completion, seedOne, seedTwo, completionID
}

func ingestEvent(t *testing.T, ctx context.Context, repository *ingest.PostgresRepository, stream, subject string, sequence uint64, event trackerannouncev1.Event) {
	t.Helper()
	payload, err := trackerannouncev1.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Process(ctx, ingest.Delivery{
		Stream: stream, Subject: subject, Sequence: sequence, DeliveryCount: 1, Payload: payload,
	}); err != nil {
		t.Fatalf("ingest sequence %d: %v", sequence, err)
	}
}

func runHNRWork(t *testing.T, ctx context.Context, worker *hnr.Worker) {
	t.Helper()
	processed, err := worker.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("H&R RunOnce() processed=%v error=%v", processed, err)
	}
}

func assertHNRProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, completionID, state string, version, seededSeconds, outboxCount int64) {
	t.Helper()
	var actualState string
	var actualVersion, actualSeeded, actualOutbox int64
	err := pool.QueryRow(ctx, `
SELECT obligation.state, obligation.version, obligation.seeded_seconds,
       (SELECT count(*) FROM settlement.hnr_outbox WHERE obligation_id = obligation.id)
FROM ledger.hnr_completion_assessments AS assessment
INNER JOIN ledger.hnr_obligations AS obligation ON obligation.assessment_id = assessment.id
WHERE assessment.completion_id = decode($1, 'hex')`, completionID).Scan(
		&actualState, &actualVersion, &actualSeeded, &actualOutbox,
	)
	if err != nil {
		t.Fatal(err)
	}
	if actualState != state || actualVersion != version || actualSeeded != seededSeconds || actualOutbox != outboxCount {
		t.Fatalf("projection state=%s version=%d seeded=%d outbox=%d", actualState, actualVersion, actualSeeded, actualOutbox)
	}
}

func newV7(t *testing.T) uuid.UUID {
	t.Helper()
	value, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func randomHex(t *testing.T, size int) string {
	t.Helper()
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(value)
}

func positiveRandomInt64(t *testing.T) int64 {
	t.Helper()
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	result := int64(binary.BigEndian.Uint64(value) & uint64(^uint64(0)>>1))
	if result == 0 {
		return 1
	}
	return result
}
