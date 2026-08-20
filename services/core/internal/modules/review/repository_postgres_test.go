package review

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

func TestNormalizedDecideCommandCanonicalizesReason(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 8, 18, 0, 0, 0, time.UTC)
	command := DecideCommand{
		DecideInput: DecideInput{
			DecisionID: uuid.New(), TorrentID: 44, ExpectedVersion: 1,
			Decision: DecisionApprove, ReasonCode: ReasonMeetsRequirements,
			Reason: "  已核对文件清单和发布规则，允许正式发布。  ",
		},
		ReviewerID: uuid.New(), OccurredAt: now,
		Authorization: authz.Decision{ID: uuid.New(), Allow: true},
	}

	got, err := normalizedDecideCommand(command)
	if err != nil {
		t.Fatalf("normalizedDecideCommand() error = %v", err)
	}
	if got.Reason != "已核对文件清单和发布规则，允许正式发布。" {
		t.Fatalf("normalized reason = %q", got.Reason)
	}
}

func TestMapReviewWriteErrorPreservesConflictMeaning(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		constraint string
		want       error
	}{
		{name: "decision id", constraint: "torrent_decisions_pkey", want: ErrTorrentReviewIdempotencyConflict},
		{name: "aggregate version", constraint: "torrent_decisions_torrent_id_expected_torrent_version_key", want: ErrTorrentReviewVersionConflict},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := mapReviewWriteError("insert", &pgconn.PgError{Code: "23505", ConstraintName: test.constraint})
			if !errors.Is(err, test.want) {
				t.Fatalf("mapReviewWriteError() error = %v, want %v", err, test.want)
			}
		})
	}
}
