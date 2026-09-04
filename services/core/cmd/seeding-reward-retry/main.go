// Command seeding-reward-retry returns one exact terminal reward item to the
// worker and commits an immutable audit event in the same transaction.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/contracts/auditevent"
	"github.com/peergo/peergo/services/core/internal/modules/audit"
	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
	"github.com/peergo/peergo/services/core/internal/platform/config"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

type output struct {
	RetryID          string    `json:"retry_id"`
	WindowStart      time.Time `json:"window_start"`
	UserID           string    `json:"user_id"`
	PreviousAttempts int32     `json:"previous_attempts"`
	PreviousError    string    `json:"previous_error_code"`
	Status           string    `json:"status"`
	RequeuedAt       time.Time `json:"requeued_at"`
}

func main() {
	windowStartText := flag.String("window-start", "", "exact UTC reward window in RFC3339 format")
	userIDText := flag.String("user-id", "", "exact affected Core user UUID")
	expectedAttempts := flag.Int("expected-attempts", 0, "attempt count currently recorded on the dead item")
	expectedError := flag.String("expected-error-code", "invariant_failed", "error code currently recorded on the dead item")
	operatorReference := flag.String("operator-reference", "", "incident, change, or deployment reference")
	reason := flag.String("reason", "", "operator reason; an explicit system reason is generated when omitted")
	confirm := flag.String("confirm", "", "must equal RETRY:<UTC-window>:<user-uuid>")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	windowStart, err := time.Parse(time.RFC3339, strings.TrimSpace(*windowStartText))
	if err != nil {
		fail(logger, "invalid seeding reward retry window", err)
	}
	userID, err := uuid.Parse(strings.TrimSpace(*userIDText))
	if err != nil {
		fail(logger, "invalid seeding reward retry user", err)
	}
	if *expectedAttempts < 1 || *expectedAttempts > 1_000_000 {
		fail(logger, "invalid seeding reward retry attempt count", errors.New("expected-attempts must be between 1 and 1000000"))
	}
	expectedConfirmation := "RETRY:" + windowStart.UTC().Format(time.RFC3339) + ":" + userID.String()
	if strings.TrimSpace(*confirm) != expectedConfirmation {
		fail(logger, "seeding reward retry was not confirmed", fmt.Errorf("confirm must equal %q", expectedConfirmation))
	}
	reasonValue := strings.TrimSpace(*reason)
	if reasonValue == "" {
		reasonValue = "系统在修复做种奖励结算缺陷后重新执行终止任务。"
	}
	settings, err := config.LoadStaffBootstrap()
	if err != nil {
		fail(logger, "invalid audited operator command configuration", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, settings.DatabaseURL)
	if err != nil {
		fail(logger, "open Core database", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		fail(logger, "ping Core database", err)
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		fail(logger, "Core database is not ready", err)
	}
	eventBuilder, err := audit.NewSeedingRewardRetryEventBuilder(audit.RecorderConfig{
		PseudonymKey: settings.AuditPseudonymKey, PseudonymKeyEpoch: settings.AuditKeyEpoch,
	})
	if err != nil {
		fail(logger, "compose seeding reward retry audit builder", err)
	}
	repository, err := seedingreward.NewPostgresDeadWorkRetryRepository(
		pool, eventBuilder,
		func(tx pgx.Tx) auditevent.Appender { return audit.NewPostgresRepository(tx) },
	)
	if err != nil {
		fail(logger, "compose seeding reward retry repository", err)
	}
	result, err := repository.RequeueDead(ctx, seedingreward.DeadWorkRetryCommand{
		ID: uuid.New(), WindowStart: windowStart, UserID: userID,
		ExpectedAttempts: int32(*expectedAttempts), ExpectedErrorCode: strings.TrimSpace(*expectedError),
		OperatorReference: strings.TrimSpace(*operatorReference), Reason: reasonValue,
		OccurredAt: time.Now().UTC(),
	})
	switch {
	case errors.Is(err, seedingreward.ErrDeadWorkNotFound):
		fail(logger, "seeding reward dead work was not found", err)
	case errors.Is(err, seedingreward.ErrDeadWorkConflict):
		fail(logger, "seeding reward dead work changed; inspect it again before retrying", err)
	case err != nil:
		fail(logger, "retry seeding reward dead work", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(output{
		RetryID: result.RetryID.String(), WindowStart: result.WindowStart,
		UserID: result.UserID.String(), PreviousAttempts: result.PreviousAttempts,
		PreviousError: result.PreviousErrorCode, Status: "requeued", RequeuedAt: result.RequeuedAt,
	}); err != nil {
		fail(logger, "write seeding reward retry result", fmt.Errorf("encode output: %w", err))
	}
}

func fail(logger *slog.Logger, message string, err error) {
	logger.Error(message, "error", err)
	os.Exit(1)
}
