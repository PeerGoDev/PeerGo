package personalapikey

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type failingCommandExecutor struct {
	query     string
	arguments []any
}

func (executor *failingCommandExecutor) Exec(_ context.Context, query string, arguments ...any) (pgconn.CommandTag, error) {
	executor.query = query
	executor.arguments = arguments
	return pgconn.CommandTag{}, errors.New("usage metadata unavailable")
}

func TestTouchCredentialUsageIsTypedCoalescedAndBestEffort(t *testing.T) {
	executor := &failingCommandExecutor{}
	now := time.Date(2026, time.August, 26, 10, 38, 12, 0, time.UTC)
	userID := uuid.New()

	// A non-critical status write failure is intentionally swallowed.
	touchCredentialUsage(executor, context.Background(), userID, now, 7)

	if !strings.Contains(executor.query, "$2::timestamptz") || !strings.Contains(executor.query, "$4::timestamptz") {
		t.Fatalf("usage query does not pin timestamp parameter types: %s", executor.query)
	}
	if strings.Contains(executor.query, "interval '6 hours'") {
		t.Fatalf("usage query still relies on ambiguous interval inference: %s", executor.query)
	}
	if len(executor.arguments) != 4 {
		t.Fatalf("usage query arguments = %d, want 4", len(executor.arguments))
	}
	if got, ok := executor.arguments[3].(time.Time); !ok || !got.Equal(now.Add(-6*time.Hour)) {
		t.Fatalf("usage cutoff = %#v, want %s", executor.arguments[3], now.Add(-6*time.Hour))
	}
}
