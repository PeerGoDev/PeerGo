package economy_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/economy"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestPostgresEconomyRecordsBalancedIdempotentTransactions(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_CORE_DATABASE_URL")
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

	// Economy evidence is immutable. This test intentionally targets only a
	// disposable migrated database and does not weaken triggers for cleanup.
	memberID := insertEconomyUser(t, ctx, pool, "member")
	recipientID := insertEconomyUser(t, ctx, pool, "recipient")
	repository, err := economy.NewPostgresRepository(pool)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
	service, err := economy.NewService(repository)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)

	opening := economyCommand(now, economy.TransactionActivityReward, "activity", []economy.PostingInput{
		{AccountID: economy.ActivityMintAccountID(), Amount: -100},
		{AccountID: memberID, Amount: 100},
	})
	created, err := service.Record(ctx, opening)
	if err != nil {
		t.Fatalf("Record(opening) error = %v", err)
	}
	if created.Replayed || postingBalance(created, memberID) != 100 {
		t.Fatalf("created transaction = %+v", created)
	}
	replayed, err := service.Record(ctx, opening)
	if err != nil || !replayed.Replayed || replayed.ID != created.ID || postingBalance(replayed, memberID) != 100 {
		t.Fatalf("Record(replay) = %+v, %v", replayed, err)
	}
	conflict := opening
	conflict.SourceReference = "activity:changed"
	if _, err := service.Record(ctx, conflict); !errors.Is(err, economy.ErrIdempotencyConflict) {
		t.Fatalf("changed idempotency replay error = %v", err)
	}

	purchase := economyCommand(now.Add(time.Second), economy.TransactionTorrentBuy, "purchase", []economy.PostingInput{
		{AccountID: memberID, Amount: -60},
		{AccountID: economy.TorrentPurchaseSinkID(), Amount: 60},
	})
	if _, err := service.Record(ctx, purchase); err != nil {
		t.Fatalf("Record(purchase) error = %v", err)
	}
	tooExpensive := economyCommand(now.Add(2*time.Second), economy.TransactionTorrentBuy, "purchase-overdraft", []economy.PostingInput{
		{AccountID: memberID, Amount: -41},
		{AccountID: economy.TorrentPurchaseSinkID(), Amount: 41},
	})
	if _, err := service.Record(ctx, tooExpensive); !errors.Is(err, economy.ErrInsufficientBalance) {
		t.Fatalf("overdraft error = %v", err)
	}

	tip := economyCommand(now.Add(3*time.Second), economy.TransactionTip, "tip", []economy.PostingInput{
		{AccountID: memberID, Amount: -10},
		{AccountID: recipientID, Amount: 10},
	})
	if _, err := service.Record(ctx, tip); err != nil {
		t.Fatalf("Record(tip) error = %v", err)
	}

	commands := []economy.RecordCommand{
		economyCommand(now.Add(4*time.Second), economy.TransactionActivityReward, "concurrent-a", []economy.PostingInput{
			{AccountID: economy.ActivityMintAccountID(), Amount: -20}, {AccountID: memberID, Amount: 20},
		}),
		economyCommand(now.Add(5*time.Second), economy.TransactionActivityReward, "concurrent-b", []economy.PostingInput{
			{AccountID: economy.ActivityMintAccountID(), Amount: -30}, {AccountID: memberID, Amount: 30},
		}),
	}
	var wait sync.WaitGroup
	errorsFound := make(chan error, len(commands))
	for _, command := range commands {
		wait.Add(1)
		go func(command economy.RecordCommand) {
			defer wait.Done()
			_, err := service.Record(ctx, command)
			errorsFound <- err
		}(command)
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatalf("concurrent Record() error = %v", err)
		}
	}

	var balance int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM economy.magic_accounts WHERE id = $1`, memberID).Scan(&balance); err != nil {
		t.Fatalf("read final member balance: %v", err)
	}
	if balance != 80 {
		t.Fatalf("member balance = %d, want 80", balance)
	}
	if _, err := pool.Exec(ctx, `UPDATE economy.magic_transactions SET source_reference = source_reference || 'x' WHERE id = $1`, created.ID); err == nil {
		t.Fatal("immutable transaction unexpectedly accepted update")
	}

	direct, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin direct invariant test: %v", err)
	}
	directID := uuid.New()
	directDigest := sha256.Sum256([]byte("unbalanced-direct"))
	if _, err := direct.Exec(ctx, `
INSERT INTO economy.magic_transactions (
    id, transaction_type, idempotency_key, source_reference,
    posting_count, payload_sha256, occurred_at, recorded_at
) VALUES ($1, 'activity_reward', $2, $2, 2, $3, $4, $4)`,
		directID, "direct:"+directID.String(), directDigest[:], now.Add(10*time.Second)); err != nil {
		_ = direct.Rollback(ctx)
		t.Fatalf("insert direct transaction: %v", err)
	}
	if err := direct.Commit(ctx); err == nil {
		t.Fatal("incomplete direct transaction unexpectedly committed")
	}
}

func insertEconomyUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	username := fmt.Sprintf("economy-it-%s-%s", suffix, userID.String()[:8])
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status)
VALUES ($1, $2, $3, $3, 'active')`, userID, uuid.New(), username); err != nil {
		t.Fatalf("insert economy user: %v", err)
	}
	return userID
}

func economyCommand(at time.Time, transactionType economy.TransactionType, reference string, postings []economy.PostingInput) economy.RecordCommand {
	id := uuid.New()
	digest := sha256.Sum256([]byte(reference))
	return economy.RecordCommand{
		TransactionID: id, TransactionType: transactionType,
		IdempotencyKey: reference + ":" + id.String(), SourceReference: reference + ":" + id.String(),
		PolicyRevision: "peergo-economy-v1", PayloadSHA256: digest,
		OccurredAt: at, RecordedAt: at, Postings: postings,
	}
}

func postingBalance(transaction economy.Transaction, accountID uuid.UUID) int64 {
	for _, posting := range transaction.Postings {
		if posting.AccountID == accountID {
			return posting.BalanceAfter
		}
	}
	return 0
}
