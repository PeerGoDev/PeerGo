package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestRequireCurrentMigration(t *testing.T) {
	if err := RequireCurrentMigration(context.Background(), migrationQuerier{version: ExpectedMigrationVersion}); err != nil {
		t.Fatal(err)
	}
	if err := RequireCurrentMigration(context.Background(), migrationQuerier{version: ExpectedMigrationVersion - 1}); err == nil {
		t.Fatal("stale migration version accepted")
	}
	readError := errors.New("database unavailable")
	if err := RequireCurrentMigration(context.Background(), migrationQuerier{err: readError}); !errors.Is(err, readError) {
		t.Fatalf("RequireCurrentMigration() error = %v", err)
	}
}

type migrationQuerier struct {
	version int64
	err     error
}

func (querier migrationQuerier) QueryRow(context.Context, string, ...any) pgx.Row {
	return migrationRow{version: querier.version, err: querier.err}
}

type migrationRow struct {
	version int64
	err     error
}

func (row migrationRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != 1 {
		return fmt.Errorf("expected one destination, got %d", len(destinations))
	}
	version, ok := destinations[0].(*int64)
	if !ok {
		return errors.New("destination is not *int64")
	}
	*version = row.version
	return nil
}
