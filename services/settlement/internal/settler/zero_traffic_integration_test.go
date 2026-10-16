package settler_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/services/settlement/internal/ingest"
	platformpostgres "github.com/peergo/peergo/services/settlement/internal/platform/postgres"
	"github.com/peergo/peergo/services/settlement/internal/settler"
	"github.com/peergo/peergo/services/settlement/internal/timeline"
)

func TestIntegrationZeroTrafficRetainsActivityWithoutAccounting(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_TRACKER_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_TRACKER_DATABASE_URL is required")
	}
	for _, completion := range []bool{false, true} {
		name := "seeding-heartbeat"
		if completion {
			name = "zero-byte-completion"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, databaseURL)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
				t.Fatal(err)
			}

			baseTime := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
			userID := uuid.New()
			baseline, heartbeat := policyIntegrationEvents(t, baseTime, userID)
			if !completion {
				baseline.Left = 0
			}
			heartbeat.Uploaded, heartbeat.Downloaded = baseline.Uploaded, baseline.Downloaded
			if !completion {
				heartbeat.Event, heartbeat.CompletionID = "", ""
			}
			stream := "PEERGO_ZERO_IT_" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))
			subject := "peergo.zero.it." + strings.ToLower(stream)
			ingester, err := ingest.NewPostgresRepository(pool, stream, subject, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			process := func(sequence uint64, event trackerannouncev1.Event) []byte {
				t.Helper()
				payload, err := trackerannouncev1.Encode(event)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := ingester.Process(ctx, ingest.Delivery{Stream: stream, Subject: subject, Sequence: sequence, DeliveryCount: 1, Payload: payload}); err != nil {
					t.Fatal(err)
				}
				return payload
			}
			process(1, baseline)
			payload := process(2, heartbeat)
			repository, err := settler.NewPostgresRepository(pool)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			pending := claimZeroTrafficFixture(t, ctx, pool, heartbeat.EventID, now)
			if err := repository.Settle(ctx, pending, now); !errors.Is(err, settler.ErrPolicyCoverage) {
				t.Fatalf("zero traffic without policy coverage: %v", err)
			}
			var premature bool
			if err := pool.QueryRow(ctx, `SELECT settled_at IS NOT NULL FROM settlement.policy_work WHERE interval_event_id=$1::uuid`, heartbeat.EventID).Scan(&premature); err != nil || premature {
				t.Fatalf("zero traffic bypassed coverage: settled=%v err=%v", premature, err)
			}
			if _, err := repository.AppendRevision(ctx, timeline.Revision{ID: uuid.New(), Scope: timeline.Scope{UserID: &userID}, EffectiveAt: baseTime.Add(-time.Hour), Snapshot: policyIntegrationSnapshot(t, baseTime)}, now); err != nil {
				t.Fatal(err)
			}
			if err := repository.Settle(ctx, pending, now); err != nil {
				t.Fatal(err)
			}

			var rawRows, settlementRows, segmentRows, outboxRows, rollupRows, hnrRows int
			var settled bool
			if err := pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM ledger.raw_session_intervals WHERE event_id=$1::uuid),
 (SELECT count(*) FROM ledger.traffic_settlements WHERE settlement_id=$1::uuid),
 (SELECT count(*) FROM ledger.traffic_settlement_segments WHERE settlement_id=$1::uuid),
 (SELECT count(*) FROM settlement.traffic_outbox WHERE settlement_id=$1::uuid),
 (SELECT count(*) FROM ledger.traffic_daily_rollups WHERE user_id=$2::uuid),
 (SELECT count(*) FROM settlement.hnr_work WHERE interval_event_id=$1::uuid),
 (SELECT settled_at IS NOT NULL FROM settlement.policy_work WHERE interval_event_id=$1::uuid)`, heartbeat.EventID, userID).Scan(&rawRows, &settlementRows, &segmentRows, &outboxRows, &rollupRows, &hnrRows, &settled); err != nil {
				t.Fatal(err)
			}
			wantHNR := 0
			if completion {
				wantHNR = 1
			}
			if rawRows != 1 || settlementRows != 0 || segmentRows != 0 || outboxRows != 0 || rollupRows != 0 || hnrRows != wantHNR || !settled {
				t.Fatalf("raw=%d settlements=%d segments=%d outbox=%d rollups=%d hnr=%d settled=%v", rawRows, settlementRows, segmentRows, outboxRows, rollupRows, hnrRows, settled)
			}
			if _, err := ingester.Process(ctx, ingest.Delivery{Stream: stream, Subject: subject, Sequence: 2, DeliveryCount: 2, Payload: payload}); err != nil {
				t.Fatal(err)
			}
			if err := repository.Settle(ctx, pending, now); !errors.Is(err, settler.ErrInvariant) {
				t.Fatalf("terminal retry: %v", err)
			}

			// A real transfer after an ignored financial no-op must use the
			// persisted heartbeat's counters, not an older baseline. Free download
			// still retains its raw bytes even though charged download is zero.
			transfer := heartbeat
			transfer.EventID, transfer.Event, transfer.CompletionID = uuid.Must(uuid.NewV7()).String(), "", ""
			transfer.ReceivedAt = heartbeat.ReceivedAt.Add(10 * time.Second)
			transfer.Uploaded += 3
			transfer.Downloaded += 7
			process(3, transfer)
			pending = claimZeroTrafficFixture(t, ctx, pool, transfer.EventID, now)
			if err := repository.Settle(ctx, pending, now); err != nil {
				t.Fatal(err)
			}
			var uploaded, downloaded, credited, charged int64
			if err := pool.QueryRow(ctx, `SELECT raw_uploaded,raw_downloaded,credited_uploaded,charged_downloaded FROM ledger.traffic_settlements WHERE settlement_id=$1::uuid`, transfer.EventID).Scan(&uploaded, &downloaded, &credited, &charged); err != nil {
				t.Fatal(err)
			}
			if uploaded != 3 || downloaded != 7 || credited != 6 || charged != 0 {
				t.Fatalf("subsequent transfer: %d/%d -> %d/%d", uploaded, downloaded, credited, charged)
			}
		})
	}
}

func claimZeroTrafficFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID string, now time.Time) settler.PendingWork {
	t.Helper()
	pending := settler.PendingWork{IntervalEventID: uuid.MustParse(eventID), LeaseToken: uuid.New()}
	if err := pool.QueryRow(ctx, `UPDATE settlement.policy_work SET lease_token=$2::uuid,lease_until=$3::timestamptz,attempts=attempts+1 WHERE interval_event_id=$1::uuid AND settled_at IS NULL RETURNING attempts`, eventID, pending.LeaseToken, now.Add(time.Minute)).Scan(&pending.Attempts); err != nil {
		t.Fatal(err)
	}
	return pending
}
