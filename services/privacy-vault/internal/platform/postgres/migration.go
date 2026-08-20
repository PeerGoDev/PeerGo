package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/peergo/peergo/contracts/go/schemaversionv1"
)

const ExpectedMigrationVersion int64 = schemaversionv1.PrivacyVault

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// RequireCurrentMigration fails startup instead of mutating schema. Migrations
// remain an explicit operator action run by the standalone Goose tool.
func RequireCurrentMigration(ctx context.Context, db rowQuerier) error {
	var version int64
	err := db.QueryRow(ctx, `
        SELECT COALESCE(MAX(version_id), 0)
        FROM goose_db_version
        WHERE is_applied = true
    `).Scan(&version)
	if err != nil {
		return fmt.Errorf("read vault migration version: %w", err)
	}
	if version != ExpectedMigrationVersion {
		return fmt.Errorf("vault migration version is %d, want %d", version, ExpectedMigrationVersion)
	}
	return nil
}
