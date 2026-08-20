package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/peergo/peergo/contracts/go/schemaversionv1"
)

const ExpectedMigrationVersion int64 = schemaversionv1.Core

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// RequireCurrentMigration verifies the operator-run schema version against the
// shared release contract. Core never calls Goose or applies DDL at startup.
func RequireCurrentMigration(ctx context.Context, db rowQuerier) error {
	var version int64
	err := db.QueryRow(ctx, `
        SELECT COALESCE(MAX(version_id), 0)
        FROM goose_db_version
        WHERE is_applied = true
    `).Scan(&version)
	if err != nil {
		return fmt.Errorf("read core migration version: %w", err)
	}
	if version != ExpectedMigrationVersion {
		return fmt.Errorf("core migration version is %d, want %d", version, ExpectedMigrationVersion)
	}
	return nil
}
