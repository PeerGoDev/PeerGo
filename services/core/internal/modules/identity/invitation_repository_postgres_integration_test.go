package identity

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestReadLiveHaremRewardQueryExecutes(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_CORE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatalf("RequireCurrentMigration() error = %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rollback-only integration transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	network := InvitationNetwork{}
	if err := readLiveHaremReward(ctx, tx, uuid.New(), time.Now().UTC(), &network); err != nil {
		t.Fatalf("readLiveHaremReward() error = %v", err)
	}
	if network.LiveHaremReward.Policy.Revision == "" {
		t.Fatal("readLiveHaremReward() returned an empty policy revision")
	}
}
