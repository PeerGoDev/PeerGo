package attendance

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/economy"
	"github.com/peergo/peergo/services/core/internal/modules/economy/magicstore"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestPostgresMakeupFillsOnlyTheNextMissedDay(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_CORE_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatalf("RequireCurrentMigration() error = %v", err)
	}
	repository, err := NewPostgresRepository(pool)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	userID := uuid.New()
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status, created_at, updated_at)
VALUES ($1, $2, $3, $3, 'active', $4, $4)`, userID, uuid.New(), fmt.Sprintf("makeup-it-%s", userID.String()[:8]), now.AddDate(0, 0, -30)); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if _, err := repository.Claim(ctx, ClaimCommand{RequestID: uuid.New(), UserID: userID, Mode: ModeFixed, Now: now.AddDate(0, 0, -3)}); err != nil {
		t.Fatalf("Claim(three days ago) error = %v", err)
	}
	overview, err := repository.Overview(ctx, userID, now, 10)
	if err != nil || overview.MakeupDate == nil || overview.MakeupCards != 0 {
		t.Fatalf("Overview() makeup = %v cards=%d, %v", overview.MakeupDate, overview.MakeupCards, err)
	}
	firstGap := *overview.MakeupDate
	if _, err := repository.Makeup(ctx, ClaimCommand{RequestID: uuid.New(), UserID: userID, Now: now}); !errors.Is(err, ErrNoMakeupCard) {
		t.Fatalf("Makeup(without card) error = %v, want ErrNoMakeupCard", err)
	}
	ledger, err := economy.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	creditID := uuid.New()
	reference := "makeup-test:" + creditID.String()
	if _, err := ledger.Record(ctx, economy.RecordCommand{
		TransactionID: creditID, TransactionType: economy.TransactionActivityReward,
		IdempotencyKey: reference, SourceReference: reference, PolicyRevision: "makeup-test",
		PayloadSHA256: sha256.Sum256([]byte(reference)), OccurredAt: now, RecordedAt: now,
		Postings: []economy.PostingInput{
			{AccountID: economy.ActivityMintAccountID(), Amount: -2000},
			{AccountID: userID, Amount: 2000},
		},
	}); err != nil {
		t.Fatalf("fund makeup card purchase: %v", err)
	}
	store, err := magicstore.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Purchase(ctx, magicstore.PurchaseCommand{
		RequestID: uuid.New(), UserID: userID, ItemKey: magicstore.ItemMakeupCard, Quantity: 2, Now: now,
	}); err != nil {
		t.Fatalf("purchase makeup cards: %v", err)
	}

	request := uuid.New()
	type result struct {
		record Record
		err    error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			record, err := repository.Makeup(ctx, ClaimCommand{RequestID: request, UserID: userID, Now: now})
			results <- result{record, err}
		}()
	}
	close(start)
	first, retry := <-results, <-results
	for _, outcome := range []result{first, retry} {
		if outcome.err != nil || !outcome.record.Makeup || outcome.record.AttendanceDate != firstGap || outcome.record.CurrentStreak != 2 {
			t.Fatalf("Makeup(concurrent request) = %+v, %v", outcome.record, outcome.err)
		}
	}
	if first.record.ID != retry.record.ID || first.record.Replayed == retry.record.Replayed {
		t.Fatalf("expected one makeup receipt and one replay, got %+v and %+v", first.record, retry.record)
	}
	if _, err := repository.Claim(ctx, ClaimCommand{RequestID: request, UserID: userID, Mode: ModeFixed, Now: now}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("Claim(reusing makeup request) error = %v, want ErrIdempotencyConflict", err)
	}
	second, err := repository.Makeup(ctx, ClaimCommand{RequestID: uuid.New(), UserID: userID, Now: now})
	if err != nil || second.CurrentStreak != 3 {
		t.Fatalf("Makeup(second) = %+v, %v", second, err)
	}
	overview, err = repository.Overview(ctx, userID, now, 10)
	if err != nil || overview.MakeupDate != nil || overview.MakeupCards != 0 || overview.CurrentStreak != 3 {
		t.Fatalf("Overview(after makeups) date=%v cards=%d streak=%d, %v", overview.MakeupDate, overview.MakeupCards, overview.CurrentStreak, err)
	}
	today, err := repository.Claim(ctx, ClaimCommand{RequestID: uuid.New(), UserID: userID, Mode: ModeFixed, Now: now})
	if err != nil || today.CurrentStreak != 4 || today.Makeup {
		t.Fatalf("Claim(today) = %+v, %v", today, err)
	}
	if _, err := repository.Makeup(ctx, ClaimCommand{RequestID: uuid.New(), UserID: userID, Now: now}); !errors.Is(err, ErrMakeupUnavailable) {
		t.Fatalf("Makeup(after today) error = %v, want ErrMakeupUnavailable", err)
	}
	var events int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM economy.member_item_events WHERE user_id = $1 AND delta = -1`, userID).Scan(&events); err != nil || events != 2 {
		t.Fatalf("makeup card events = %d, %v", events, err)
	}
}
