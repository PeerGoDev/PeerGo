package progression_test

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

	"github.com/peergo/peergo/services/core/internal/modules/progression"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestPostgresProgressionRecordsExactIdempotentEntries(t *testing.T) {
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

	userID := insertProgressionUser(t, ctx, pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	activityPolicy := "progression-activity-it-" + userID.String()[:8]
	adminPolicy := "progression-admin-it-" + userID.String()[:8]
	insertExperiencePolicy(t, ctx, pool, activityPolicy, "activity", now.Add(-time.Hour))
	insertExperiencePolicy(t, ctx, pool, adminPolicy, "administrator_adjustment", now.Add(-time.Hour))
	repository, err := progression.NewPostgresRepository(pool)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
	service, err := progression.NewService(repository)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	first := progressionCommand(t, userID, now, "first", "999.5", progression.EntryEarn, progression.SourceActivity, activityPolicy)
	created, err := service.Record(ctx, first)
	if err != nil || created.Replayed || created.BalanceAfter.String() != "999.5" || created.LevelAfter != 1 || created.LevelTransition {
		t.Fatalf("Record(first) = %+v, %v", created, err)
	}
	replayed, err := service.Record(ctx, first)
	if err != nil || !replayed.Replayed || replayed.ID != created.ID || replayed.BalanceAfter.String() != "999.5" {
		t.Fatalf("Record(replay) = %+v, %v", replayed, err)
	}
	conflict := first
	conflict.Amount, _ = progression.ParseAmount("999.6")
	if _, err := service.Record(ctx, conflict); !errors.Is(err, progression.ErrIdempotencyConflict) {
		t.Fatalf("changed replay error = %v", err)
	}

	upgrade := progressionCommand(t, userID, now.Add(time.Second), "upgrade", "0.5", progression.EntryEarn, progression.SourceActivity, activityPolicy)
	upgraded, err := service.Record(ctx, upgrade)
	if err != nil || upgraded.BalanceAfter.String() != "1000" || upgraded.LevelAfter != 2 || !upgraded.LevelTransition {
		t.Fatalf("Record(upgrade) = %+v, %v", upgraded, err)
	}
	overdraft := progressionCommand(t, userID, now.Add(2*time.Second), "overdraft", "-1000.25", progression.EntryAdjustment, progression.SourceAdministratorAdjust, adminPolicy)
	if _, err := service.Record(ctx, overdraft); !errors.Is(err, progression.ErrInsufficientXP) {
		t.Fatalf("overdraft error = %v", err)
	}
	reset := progressionCommand(t, userID, now.Add(3*time.Second), "reset", "-1000", progression.EntryAdjustment, progression.SourceAdministratorAdjust, adminPolicy)
	resetResult, err := service.Record(ctx, reset)
	if err != nil || resetResult.BalanceAfter.String() != "0" || resetResult.LevelAfter != 1 || !resetResult.LevelTransition {
		t.Fatalf("Record(reset) = %+v, %v", resetResult, err)
	}

	commands := []progression.RecordCommand{
		progressionCommand(t, userID, now.Add(4*time.Second), "concurrent-a", "600.1", progression.EntryEarn, progression.SourceActivity, activityPolicy),
		progressionCommand(t, userID, now.Add(5*time.Second), "concurrent-b", "599.9", progression.EntryEarn, progression.SourceActivity, activityPolicy),
	}
	var wait sync.WaitGroup
	errorsFound := make(chan error, len(commands))
	for _, command := range commands {
		wait.Add(1)
		go func(command progression.RecordCommand) {
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

	var balance string
	var level int16
	if err := pool.QueryRow(ctx, `
SELECT experience::text, level
FROM progression.user_progress
WHERE user_id = $1`, userID).Scan(&balance, &level); err != nil {
		t.Fatalf("read final progression: %v", err)
	}
	if balance != "1200.00000000000000000000" || level != 2 {
		t.Fatalf("final progression = %s/Lv.%d, want 1200/Lv.2", balance, level)
	}
	if _, err := pool.Exec(ctx, `UPDATE progression.experience_entries SET source_reference = source_reference || 'x' WHERE id = $1`, created.ID); err == nil {
		t.Fatal("immutable experience entry unexpectedly accepted update")
	}
	var broken int64
	if err := pool.QueryRow(ctx, `
SELECT count(*)::bigint
FROM progression.experience_entries AS entry
JOIN LATERAL (
    SELECT COALESCE(sum(previous.amount), 0)::numeric(38, 20) AS expected
    FROM progression.experience_entries AS previous
    WHERE previous.user_id = entry.user_id
      AND previous.entry_sequence <= entry.entry_sequence
) AS chain ON true
WHERE entry.user_id = $1
  AND entry.balance_after <> chain.expected`, userID).Scan(&broken); err != nil || broken != 0 {
		t.Fatalf("experience balance chain broken=%d, error=%v", broken, err)
	}
}

func insertProgressionUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	username := fmt.Sprintf("progression-it-%s", userID.String()[:8])
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status)
VALUES ($1, $2, $3, $3, 'active')`, userID, uuid.New(), username); err != nil {
		t.Fatalf("insert progression user: %v", err)
	}
	return userID
}

func insertExperiencePolicy(t *testing.T, ctx context.Context, pool *pgxpool.Pool, revision, source string, effective time.Time) {
	t.Helper()
	digest := sha256.Sum256([]byte(revision + ":" + source))
	if _, err := pool.Exec(ctx, `
INSERT INTO progression.experience_policy_revisions (
    revision, source_kind, effective_from, payload_sha256, created_at
) VALUES ($1, $2, $3, $4, $3)`, revision, source, effective, digest[:]); err != nil {
		t.Fatalf("insert experience policy: %v", err)
	}
}

func progressionCommand(t *testing.T, userID uuid.UUID, at time.Time, reference, amount string, entryType progression.EntryType, source progression.SourceKind, policy string) progression.RecordCommand {
	t.Helper()
	parsed, err := progression.ParseAmount(amount)
	if err != nil {
		t.Fatalf("ParseAmount(%q) error = %v", amount, err)
	}
	entryID := uuid.New()
	digest := sha256.Sum256([]byte(reference + ":" + amount))
	return progression.RecordCommand{
		EntryID: entryID, IdempotencyKey: "progression:" + reference + ":" + entryID.String(),
		UserID: userID, EntryType: entryType, Amount: parsed,
		SourceReference: "progression:" + reference + ":" + entryID.String(), SourceKind: source,
		PolicyRevision: policy, LevelPolicyVersion: "rousi-v1", PayloadSHA256: digest,
		OccurredAt: at, RecordedAt: at,
	}
}
