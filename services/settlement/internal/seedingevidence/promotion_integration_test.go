package seedingevidence_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/settlement/internal/ingest"
	"github.com/peergo/peergo/services/settlement/internal/seedingevidence"
)

func TestIntegrationCompactPromotionBuildsHourAfterRawRetirement(t *testing.T) {
	ctx, pool := compactDatabase(t)
	hour := time.Now().UTC().Add(-192 * time.Hour).Truncate(time.Hour)
	clock := hour.Add(5*time.Hour + 46*time.Minute)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	stream := "PROMOTE_" + strings.ToUpper(suffix)
	snapshots := "PROMOTE_FRAMES_" + strings.ToUpper(suffix)
	subject, snapshotSubject := "peergo.promote."+suffix, "peergo.promote.frames."+suffix
	if _, err := pool.Exec(ctx, "INSERT INTO settlement.activity_projection_profiles(source_stream,activates_at,max_interval_credit_seconds) VALUES($1,$2,2100)", stream, hour); err != nil {
		t.Fatal(err)
	}
	writer, err := ingest.NewPostgresRepository(pool, stream, subject, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	user := uuid.New()
	hash := strings.Repeat("e7", 20)
	var retired uuid.UUID
	for h := 0; h < 4; h++ {
		for j := 0; j < 2; j++ {
			event := seedingEvent(t, user, 8131, hash, fmt.Sprintf("%064x", h+100), hour.Add(time.Duration(h)*time.Hour+time.Duration(1+j*10)*time.Minute), 0, 0)
			ingestSeedingEvent(t, ctx, writer, stream, subject, uint64(h*2+j+1), event)
			if h == 3 && j == 1 {
				retired = uuid.MustParse(event.EventID)
			}
		}
	}
	fence := seedingEvent(t, uuid.New(), 8132, strings.Repeat("f7", 20), strings.Repeat("61", 32), clock, 0, 0)
	ingestSeedingEvent(t, ctx, writer, stream, subject, 9, fence)
	repo, err := seedingevidence.NewPostgresRepository(pool, seedingevidence.PostgresRepositoryConfig{
		AnnounceStream: stream, SnapshotStream: snapshots, SnapshotSubject: snapshotSubject, CompactSnapshots: true,
		MaxFutureSkew: time.Minute, MaximumSnapshotClosureDelay: 15 * time.Minute, ClosureDelay: 45 * time.Minute, MaxIntervalCredit: 35 * time.Minute,
	}, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	for h := 0; h < 4; h++ {
		for j, minute := range []int{59, 61} {
			chunk := snapshotChunk(t, "promote-"+suffix, int64(h*2+j+1), hour.Add(time.Duration(h)*time.Hour+time.Duration(minute)*time.Minute), hash, 3, 1)
			payload, e := trackerswarmv1.Encode(chunk)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = repo.ApplySnapshot(ctx, seedingevidence.SnapshotDelivery{Stream: snapshots, Subject: snapshotSubject, Sequence: uint64(h*2 + j + 1), DeliveryCount: 1, Payload: payload}); e != nil {
				t.Fatal(e)
			}
		}
	}
	for h := 0; h < 3; h++ {
		if result, e := repo.BuildHour(ctx, hour.Add(time.Duration(h)*time.Hour), clock); e != nil || result.SchemaVersion != seedingevidence.SchemaVersionV3 {
			t.Fatalf("shadow hour %d %+v %v", h, result, e)
		}
	}
	var verified int
	if err = pool.QueryRow(ctx, "SELECT shadow_verified_windows FROM settlement.activity_projection_profiles WHERE source_stream=$1", stream).Scan(&verified); err != nil || verified != 3 {
		t.Fatalf("profile failed promotion: %d %v", verified, err)
	}
	// The fourth hour is unclosed, but its activity is durably represented and
	// the independent raw-shadow validation has completed. V1 inbox provenance
	// must be just as usable for retirement as V2 raw provenance.
	var compacted bool
	if err = pool.QueryRow(ctx, "SELECT ledger.raw_seeding_is_compacted(raw) FROM ledger.raw_session_intervals raw WHERE event_id=$1", retired).Scan(&compacted); err != nil || !compacted {
		t.Fatalf("represented unclosed raw is pinned: %t %v", compacted, err)
	}
	if _, err = pool.Exec(ctx, "UPDATE settlement.policy_work SET settled_at=$2 WHERE interval_event_id=$1", retired, clock); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "DELETE FROM settlement.policy_work WHERE interval_event_id=$1", retired); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "DELETE FROM ledger.raw_session_intervals WHERE event_id=$1", retired); err != nil {
		t.Fatal(err)
	}
	result, err := repo.BuildHour(ctx, hour.Add(3*time.Hour), clock)
	if err != nil || result.SchemaVersion != seedingevidence.SchemaVersionV3 || result.ItemCount != 1 {
		t.Fatalf("compact-only build %+v %v", result, err)
	}
	var seconds, rawCount int64
	if err = pool.QueryRow(ctx, "SELECT active_seconds FROM ledger.seeding_evidence_items WHERE window_start=$1 AND user_id=$2", hour.Add(3*time.Hour), user).Scan(&seconds); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM ledger.raw_session_intervals WHERE event_id=$1", retired).Scan(&rawCount); err != nil {
		t.Fatal(err)
	}
	if seconds != 600 || rawCount != 0 {
		t.Fatalf("retired source changed activity: seconds=%d raw=%d", seconds, rawCount)
	}
	if _, err = pool.Exec(ctx, "UPDATE settlement.seeding_evidence_outbox SET published_at=$3 WHERE window_start>=$1 AND window_start<$2", hour, hour.Add(4*time.Hour), clock.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
}
