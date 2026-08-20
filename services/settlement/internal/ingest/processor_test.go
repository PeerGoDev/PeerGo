package ingest

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsPermanent(t *testing.T) {
	for _, err := range []error{ErrInvalidInput, ErrSessionInvariant, ErrEventConflict, ErrSourceInvariant} {
		if !IsPermanent(fmt.Errorf("boundary: %w", err)) {
			t.Fatalf("IsPermanent(%v) = false", err)
		}
	}
	if IsPermanent(errors.New("database unavailable")) {
		t.Fatal("transient database error classified as permanent")
	}
}

func TestRepositoryOperationErrorClassifiesDatabaseInvariants(t *testing.T) {
	constraintError := &pgconn.PgError{Code: "23514", ConstraintName: "raw_interval_check"}
	classified := repositoryOperationError("insert raw interval", constraintError)
	if !IsPermanent(classified) {
		t.Fatalf("constraint error was not permanent: %v", classified)
	}
	transientError := &pgconn.PgError{Code: "40001", Message: "serialization failure"}
	classified = repositoryOperationError("commit", transientError)
	if IsPermanent(classified) || !errors.Is(classified, transientError) {
		t.Fatalf("serialization error classification = %v", classified)
	}
}
