package torrents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
)

func (repository *PostgresTorrentMaintenanceRepository) RequestTorrentReseed(ctx context.Context, torrentID TorrentID, userID uuid.UUID, now time.Time) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin torrent reseed request: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var uploaderID uuid.UUID
	var title string
	err = tx.QueryRow(ctx, `
SELECT uploader_id, title FROM torrents.torrents
WHERE id = $1 AND state = 'published'
FOR SHARE`, torrentID).Scan(&uploaderID, &title)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTorrentReseedNotFound
	}
	if err != nil {
		return fmt.Errorf("lock published torrent for reseed: %w", err)
	}

	// The snapshot projector locks this same row before resolving requests.
	// A concurrent recovery thus either includes this member or wins before
	// the member can join an already recovered swarm.
	var observedAt time.Time
	var seeders int
	err = tx.QueryRow(ctx, `
SELECT observed_at, seeders FROM catalog.torrent_swarm_stats
WHERE torrent_id = $1
FOR UPDATE`, torrentID).Scan(&observedAt, &seeders)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTorrentReseedUnavailable
	}
	if err != nil {
		return fmt.Errorf("lock torrent reseed swarm stats: %w", err)
	}
	if seeders != 0 || observedAt.Before(now.Add(-catalog.SwarmFreshness)) {
		return ErrTorrentReseedUnavailable
	}

	var requestID uuid.UUID
	err = tx.QueryRow(ctx, `
SELECT id FROM torrents.reseed_requests
WHERE torrent_id = $1 AND restored_at IS NULL`, torrentID).Scan(&requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.QueryRow(ctx, `
INSERT INTO torrents.reseed_requests (torrent_id, torrent_title, opened_at, zero_observed_at)
VALUES ($1, $2, $3, $4) RETURNING id`, torrentID, title, now, observedAt).Scan(&requestID); err != nil {
			return fmt.Errorf("open torrent reseed request: %w", err)
		}
		if _, err := tx.Exec(ctx, notifyTorrentReseedCandidates, requestID, torrentID, uploaderID, now); err != nil {
			return fmt.Errorf("notify torrent reseed candidates: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("read open torrent reseed request: %w", err)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO torrents.reseed_request_members (request_id, user_id)
VALUES ($1, $2) ON CONFLICT (request_id, user_id) DO NOTHING`, requestID, userID); err != nil {
		return fmt.Errorf("join torrent reseed request: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit torrent reseed request: %w", err)
	}
	return nil
}

const notifyTorrentReseedCandidates = `
WITH candidates AS (
    SELECT $3::uuid AS user_id
    UNION
    SELECT evidence.user_id
    FROM economy.seeding_reward_evidence_items AS evidence
    JOIN economy.seeding_reward_evidence_windows AS evidence_window
      ON evidence_window.window_start = evidence.window_start AND evidence_window.status = 'complete'
    WHERE evidence.torrent_id = $2 AND evidence.active_seconds > 0
    UNION
    SELECT pack.user_id
    FROM economy.seeding_reward_evidence_packs AS pack
    WHERE economy.seeding_reward_pack_seeded_torrents(pack.items) @> ARRAY[$2::bigint]
)
INSERT INTO community.torrent_reseed_notifications (request_id, recipient_user_id, kind, created_at)
SELECT $1, candidates.user_id, 'torrent_reseed_requested', $4
FROM candidates
JOIN identity.users AS member ON member.id = candidates.user_id AND member.status = 'active'`
