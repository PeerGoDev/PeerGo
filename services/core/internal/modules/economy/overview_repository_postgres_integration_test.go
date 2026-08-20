package economy

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This opt-in test is intentionally read-only so it can verify the restored
// Rousi development database without rewriting imported account history.
func TestPostgresOverviewRepositoryReadsRestoredMemberProgress(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_CORE_DATABASE_URL")
	username := os.Getenv("PEERGO_TEST_OVERVIEW_USERNAME")
	if databaseURL == "" || username == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL and PEERGO_TEST_OVERVIEW_USERNAME are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var userID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM identity.users WHERE username = $1`, username).Scan(&userID); err != nil {
		t.Fatalf("find restored member: %v", err)
	}
	repository, err := NewPostgresOverviewRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	overview, err := repository.Overview(ctx, userID, 30)
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if overview.Progress.PolicyVersion == "" || overview.Progress.Experience == "" {
		t.Fatalf("progress = %+v", overview.Progress)
	}
	var expectedLevel int16
	var expectedMinimum string
	err = pool.QueryRow(ctx, `
SELECT definition.level, definition.minimum_experience::text
FROM progression.level_definitions AS definition
WHERE definition.policy_version = $1
  AND definition.minimum_experience > $2::numeric(38, 20)
ORDER BY definition.minimum_experience
LIMIT 1`, overview.Progress.PolicyVersion, overview.Progress.Experience).Scan(&expectedLevel, &expectedMinimum)
	if err == nil {
		if overview.Progress.Next == nil || overview.Progress.Next.Level != expectedLevel || overview.Progress.Next.MinimumExperience != expectedMinimum {
			t.Fatalf("next level = %+v, want level=%d minimum=%s", overview.Progress.Next, expectedLevel, expectedMinimum)
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		if overview.Progress.Next != nil {
			t.Fatalf("next level = %+v, want nil", overview.Progress.Next)
		}
	} else {
		t.Fatalf("read expected next level: %v", err)
	}
}
