package torrentpurchase

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestTorrentTransactionSourceReferenceScopesEachSaleAndRefund(t *testing.T) {
	requestID := uuid.MustParse("0198f20a-6da8-7e51-9c64-444444444444")

	if got, want := torrentTransactionSourceReference("purchase", 42, requestID), "torrent:42:purchase:0198f20a6da87e519c64444444444444"; got != want {
		t.Fatalf("purchase source reference = %q, want %q", got, want)
	}
	if got, want := torrentTransactionSourceReference("refund", 42, requestID), "torrent:42:refund:0198f20a6da87e519c64444444444444"; got != want {
		t.Fatalf("refund source reference = %q, want %q", got, want)
	}
}

func TestClassifyErrorMapsOnlyRequestIdentityConstraintsToIdempotency(t *testing.T) {
	idempotencyViolation := &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "torrent_purchase_entitlements_request_id_key",
	}
	if err := classifyError("insert entitlement", idempotencyViolation); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("request identity violation = %v, want ErrIdempotencyConflict", err)
	}

	statementViolation := &pgconn.PgError{
		Code:           "23505",
		ConstraintName: "magic_ledger_entries_user_source_unique",
	}
	err := classifyError("record purchase", statementViolation)
	if errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("statement uniqueness violation was misclassified as idempotency: %v", err)
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.ConstraintName != statementViolation.ConstraintName {
		t.Fatalf("statement uniqueness violation lost its database cause: %v", err)
	}
}
