// Package legacyroutealiases owns the bounded, one-way compatibility mapping
// from old PtYes torrent route UUIDs to PeerGo's canonical numeric IDs.
package legacyroutealiases

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidAlias = errors.New("legacy torrent route alias is invalid")

type Result struct {
	SourceRows   int64
	MappedRows   int64
	InsertedRows int64
	AliasRows    int64
}

// Digest canonicalizes a UUID before hashing it. The raw PtYes route value is
// never written to Core, logs or command output.
func Digest(raw string) ([sha256.Size]byte, error) {
	parsed, err := uuid.Parse(raw)
	if err != nil || parsed == uuid.Nil {
		return [sha256.Size]byte{}, ErrInvalidAlias
	}
	return sha256.Sum256([]byte(parsed.String())), nil
}

// Backfill copies only one-way alias digests for already migrated torrents.
// It is idempotent and verifies complete coverage of migration.torrent_id_map
// before committing.
func Backfill(ctx context.Context, source, core *pgxpool.Pool, createdAt time.Time) (Result, error) {
	createdAt = createdAt.UTC().Truncate(time.Microsecond)
	if source == nil || core == nil || createdAt.IsZero() {
		return Result{}, errors.New("legacy torrent route alias backfill configuration is invalid")
	}

	rows, err := source.Query(ctx, `
SELECT id::bigint, uuid::text
FROM torrents
ORDER BY id`)
	if err != nil {
		return Result{}, fmt.Errorf("read PtYes torrent route aliases: %w", err)
	}
	defer rows.Close()

	staged := make([][]any, 0, 10_000)
	var previousID int64
	for rows.Next() {
		var torrentID int64
		var rawAlias string
		if err := rows.Scan(&torrentID, &rawAlias); err != nil {
			return Result{}, fmt.Errorf("scan PtYes torrent route alias: %w", err)
		}
		if torrentID < 1 || torrentID <= previousID {
			return Result{}, errors.New("PtYes torrent route alias IDs are invalid")
		}
		previousID = torrentID
		digest, err := Digest(rawAlias)
		if err != nil {
			return Result{}, fmt.Errorf("validate PtYes torrent route alias for torrent %d: %w", torrentID, err)
		}
		staged = append(staged, []any{append([]byte(nil), digest[:]...), torrentID})
	}
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("iterate PtYes torrent route aliases: %w", err)
	}

	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("begin legacy torrent route alias backfill: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
CREATE TEMP TABLE legacy_torrent_route_alias_stage (
    alias_sha256 bytea PRIMARY KEY CHECK (octet_length(alias_sha256) = 32),
    torrent_id bigint NOT NULL UNIQUE CHECK (torrent_id > 0)
) ON COMMIT DROP`); err != nil {
		return Result{}, fmt.Errorf("create legacy torrent route alias stage: %w", err)
	}
	copied, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"legacy_torrent_route_alias_stage"},
		[]string{"alias_sha256", "torrent_id"},
		pgx.CopyFromRows(staged),
	)
	if err != nil {
		return Result{}, fmt.Errorf("stage legacy torrent route aliases: %w", err)
	}
	if copied != int64(len(staged)) {
		return Result{}, errors.New("legacy torrent route alias stage is incomplete")
	}

	inserted, err := tx.Exec(ctx, `
INSERT INTO migration.legacy_torrent_route_aliases (
    alias_sha256, torrent_id, created_at
)
SELECT staged.alias_sha256, mapping.torrent_id, $1
FROM legacy_torrent_route_alias_stage AS staged
JOIN migration.torrent_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_torrent_id = staged.torrent_id
 AND mapping.torrent_id = staged.torrent_id
JOIN torrents.torrents AS torrent ON torrent.id = mapping.torrent_id
ON CONFLICT DO NOTHING`, createdAt)
	if err != nil {
		return Result{}, fmt.Errorf("insert legacy torrent route aliases: %w", err)
	}

	var mappedRows, matchedRows, aliasRows int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)
FROM migration.torrent_id_map
WHERE source_system = 'ptyes'`).Scan(&mappedRows); err != nil {
		return Result{}, fmt.Errorf("count migrated torrent IDs: %w", err)
	}
	if err := tx.QueryRow(ctx, `
SELECT count(*)
FROM migration.torrent_id_map AS mapping
JOIN legacy_torrent_route_alias_stage AS staged
  ON staged.torrent_id = mapping.legacy_torrent_id
JOIN migration.legacy_torrent_route_aliases AS alias
  ON alias.torrent_id = mapping.torrent_id
 AND alias.alias_sha256 = staged.alias_sha256
WHERE mapping.source_system = 'ptyes'`).Scan(&matchedRows); err != nil {
		return Result{}, fmt.Errorf("verify migrated torrent route aliases: %w", err)
	}
	if matchedRows != mappedRows {
		return Result{}, fmt.Errorf("legacy torrent route alias coverage is %d, want %d", matchedRows, mappedRows)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM migration.legacy_torrent_route_aliases`).Scan(&aliasRows); err != nil {
		return Result{}, fmt.Errorf("count legacy torrent route aliases: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit legacy torrent route alias backfill: %w", err)
	}
	return Result{
		SourceRows: int64(len(staged)), MappedRows: mappedRows,
		InsertedRows: inserted.RowsAffected(), AliasRows: aliasRows,
	}, nil
}
