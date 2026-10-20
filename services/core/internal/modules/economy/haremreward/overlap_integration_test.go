package haremreward_test

import (
	"context"
	"crypto/sha256"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/core/internal/modules/economy/haremreward"
)

func TestIntegrationDoesNotPayAcrossAnExistingWindow(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("PEERGO_TEST_CORE_DATABASE_URL"))
	if url == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	start := time.Date(2026, 8, 21, 5, 0, 0, 0, time.UTC)
	digest := sha256.Sum256([]byte("overlap guard"))
	for h := 0; h < 12; h++ {
		insertCompleteHaremEvidenceWindow(t, ctx, pool, start.Add(time.Duration(h)*time.Hour), int64(h+1))
	}
	// Represent a noncanonical historic receipt. Neither adjacent canonical
	// period is allowed to cover the already accounted source hours again.
	if _, err := pool.Exec(ctx, `INSERT INTO economy.harem_reward_windows(window_start,window_end,policy_revision,
 source_calculation_count,eligible_relationship_count,recipient_count,total_reward,payload_sha256,completed_at)
 VALUES($1,$2,'harem-rousi-v1',0,0,0,0,$3,$2)`, start.Add(3*time.Hour), start.Add(9*time.Hour), digest[:]); err != nil {
		t.Fatal(err)
	}
	repo, err := haremreward.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	result, err := repo.SettleNext(ctx, start.Add(12*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if result.Processed {
		t.Fatalf("overlapping window settled: %+v", result)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM economy.harem_reward_windows`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("overlap guard created %d receipts", count)
	}
}
