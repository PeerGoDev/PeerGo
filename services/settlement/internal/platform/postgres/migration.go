package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const ExpectedMigrationVersion int64 = 202608220005

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// RequireCurrentMigration verifies operator-applied Tracker Ledger DDL.
// Settlement never mutates its schema during startup because a partially
// upgraded accounting database must fail closed, not continue opportunistically.
func RequireCurrentMigration(ctx context.Context, db rowQuerier) error {
	var version int64
	err := db.QueryRow(ctx, `
        SELECT COALESCE(MAX(version_id), 0)
        FROM goose_db_version
        WHERE is_applied = true
    `).Scan(&version)
	if err != nil {
		return fmt.Errorf("read Tracker Ledger migration version: %w", err)
	}
	if version != ExpectedMigrationVersion {
		return fmt.Errorf("Tracker Ledger migration version is %d, want %d", version, ExpectedMigrationVersion)
	}
	return nil
}
