package hnr_test

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
	"github.com/peergo/peergo/services/settlement/internal/hnr"
	"github.com/peergo/peergo/services/settlement/internal/hnrpolicy"
	"github.com/peergo/peergo/services/settlement/internal/ingest"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
	"github.com/peergo/peergo/services/settlement/internal/timeline"
)

func TestIntegrationIncrementalHNRSustainsProgressAfterRawRetirementAndRestart(t *testing.T) {
	for _, version := range []struct {
		name      string
		sequenced bool
	}{{"v1_inbox_coordinates", false}, {"v2_raw_coordinates", true}} {
		t.Run(version.name, func(t *testing.T) { runIntegrationIncrementalHNRProgress(t, version.sequenced) })
	}
}
func runIntegrationIncrementalHNRProgress(t *testing.T, sequencedProtocol bool) {
	url := strings.TrimSpace(os.Getenv("PEERGO_TEST_TRACKER_DATABASE_URL"))
	if url == "" {
		t.Skip("PEERGO_TEST_TRACKER_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-96 * time.Hour).Truncate(time.Hour)
	user := uuid.New()
	torrent := positiveRandomInt64(t)
	stream := "INCREMENTAL_HNR_" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:10])
	subject := "peergo.incremental.hnr"
	repo, err := hnr.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	revision := hnrpolicy.Revision{ID: uuid.New(), Scope: timeline.Scope{UserID: &user}, EffectiveAt: base.Add(-time.Minute),
		Policy: hnrpolicy.Policy{Rule: hnrpolicy.RuleRef{ID: "incremental-hnr", Version: 1}, Mode: hnrpolicy.ModeEnforced,
			RequiredSeedSeconds: 72000, RequiredRatioBasisPoints: 20000, AssessmentWindowSeconds: 172800, GracePeriodSeconds: 3600, MaxIntervalCreditSeconds: 5400}}
	if _, err = repo.AppendRevision(ctx, revision, base); err != nil {
		t.Fatal(err)
	}
	// This fixture models a stream whose compact activity profile has already
	// passed its independent shadow checks. Other tests exercise those checks.
	if _, err = pool.Exec(ctx, `INSERT INTO settlement.activity_projection_profiles
  (source_stream,activates_at,max_interval_credit_seconds,shadow_verified_windows) VALUES($1,$2,3600,3)`, stream, base); err != nil {
		t.Fatal(err)
	}
	writer, err := ingest.NewPostgresRepository(pool, stream, subject, func() time.Time { return base.Add(7 * time.Hour) })
	if err != nil {
		t.Fatal(err)
	}
	baseline, completion, one, two, completionID := hnrIntegrationEvents(t, base, user, torrent)
	producerEpoch := uuid.Must(uuid.NewV7()).String()
	appendEvent := func(sequence uint64, event trackerannouncev1.Event) {
		t.Helper()
		var payload []byte
		var e error
		if sequencedProtocol {
			sequenced, err := trackerannouncev2.FromV1(event, "incremental-hnr-test", producerEpoch, int64(sequence))
			if err != nil {
				t.Fatal(err)
			}
			payload, e = trackerannouncev2.Encode(sequenced)
		} else {
			payload, e = trackerannouncev1.Encode(event)
		}
		if e != nil {
			t.Fatal(e)
		}
		if _, e = writer.Process(ctx, ingest.Delivery{Stream: stream, Subject: subject, Sequence: sequence, DeliveryCount: 1, Payload: payload}); e != nil {
			t.Fatal(e)
		}
	}
	appendEvent(1, baseline)
	appendEvent(2, completion)
	clock := base.Add(8 * time.Hour)
	worker, err := hnr.NewWorker(repo, hnr.WorkerConfig{LeaseDuration: time.Minute, IdleInterval: time.Second, RetryBase: time.Second}, func() time.Time { return clock }, nil)
	if err != nil {
		t.Fatal(err)
	}
	runHNRWork(t, ctx, worker)
	appendEvent(3, one)
	clock = clock.Add(time.Second)
	runHNRWork(t, ctx, worker)
	assertHNRProjection(t, ctx, pool, completionID, "tracking", 2, 3600, 2)
	var required, compacted bool
	if err = pool.QueryRow(ctx, `SELECT ledger.raw_hnr_is_required(raw),ledger.raw_seeding_is_compacted(raw)
  FROM ledger.raw_session_intervals raw WHERE event_id=$1`, uuid.MustParse(one.EventID)).Scan(&required, &compacted); err != nil {
		t.Fatal(err)
	}
	if required || !compacted {
		t.Fatalf("checkpoint did not release represented raw dependency: hnr=%t seeding=%t", required, compacted)
	}
	var unknownRequired, unknownCompacted bool
	if err = pool.QueryRow(ctx, `SELECT ledger.raw_hnr_is_required(unknown),ledger.raw_seeding_is_compacted(unknown)
      FROM ledger.raw_session_intervals raw,
      LATERAL(SELECT jsonb_populate_record(NULL::ledger.raw_session_intervals,
         to_jsonb(raw)||jsonb_build_object('event_id',$2::uuid,'source_stream',NULL,'source_sequence',NULL)) unknown) absent
      WHERE raw.event_id=$1`, uuid.MustParse(one.EventID), uuid.New()).Scan(&unknownRequired, &unknownCompacted); err != nil {
		t.Fatal(err)
	}
	if !unknownRequired || unknownCompacted {
		t.Fatalf("unknown legacy source lost its retention pin: %t %t", unknownRequired, unknownCompacted)
	}
	if _, err = pool.Exec(ctx, "UPDATE settlement.policy_work SET settled_at=$2 WHERE interval_event_id=$1", uuid.MustParse(one.EventID), clock); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "DELETE FROM settlement.policy_work WHERE interval_event_id=$1", uuid.MustParse(one.EventID)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "DELETE FROM settlement.hnr_work WHERE interval_event_id=$1", uuid.MustParse(one.EventID)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "DELETE FROM ledger.raw_session_intervals WHERE event_id=$1", uuid.MustParse(one.EventID)); err != nil {
		t.Fatal(err)
	}
	// Reconstruct repository and worker solely from the committed database state.
	repo, err = hnr.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	worker, err = hnr.NewWorker(repo, hnr.WorkerConfig{LeaseDuration: time.Minute, IdleInterval: time.Second, RetryBase: time.Second}, func() time.Time { return clock }, nil)
	if err != nil {
		t.Fatal(err)
	}
	appendEvent(4, two)
	clock = clock.Add(time.Second)
	runHNRWork(t, ctx, worker)
	assertHNRProjection(t, ctx, pool, completionID, "tracking", 3, 7200, 3)
	var rawCount int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM ledger.raw_session_intervals WHERE event_id=$1", uuid.MustParse(one.EventID)).Scan(&rawCount); err != nil || rawCount != 0 {
		t.Fatalf("retired raw unexpectedly restored: %d %v", rawCount, err)
	}
	// A malformed/rewound checkpoint is rejected by the database as well.
	_, err = pool.Exec(ctx, `UPDATE settlement.hnr_progress_accumulators
  SET source_cursors='{}',state_json=jsonb_set(state_json,'{cursors}','{}')
  WHERE obligation_id IN (SELECT id FROM ledger.hnr_obligations WHERE assessment_id IN
   (SELECT id FROM ledger.hnr_completion_assessments WHERE user_id=$1 AND torrent_id=$2))`, user, torrent)
	if err == nil {
		t.Fatal("checkpoint cursor rewind accepted")
	}
}
