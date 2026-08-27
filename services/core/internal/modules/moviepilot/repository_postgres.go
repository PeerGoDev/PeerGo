package moviepilot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/personalapikey"
)

// Repository contains only canonical profile and settled reward projections
// shared by external clients. Personal-key persistence belongs to the
// personalapikey module.
type Repository interface {
	Profile(context.Context, uuid.UUID, time.Time) (Profile, error)
	LatestSeedingReward(context.Context, uuid.UUID) (int64, error)
}

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("MoviePilot PostgreSQL pool is required")
	}
	return &PostgresRepository{pool: pool}, nil
}

func (repository *PostgresRepository) Profile(ctx context.Context, userID uuid.UUID, now time.Time) (Profile, error) {
	var result Profile
	var lastActive, vipUntil pgtype.Timestamptz
	var experienceText string
	err := repository.pool.QueryRow(ctx, `
SELECT
    users.numeric_id,
    users.username,
    users.display_name,
    COALESCE(progress.level, 1),
    users.created_at,
    activity.last_active_at,
    COALESCE(traffic.credited_uploaded, 0),
    COALESCE(traffic.charged_downloaded, 0),
    COALESCE(magic.balance, 0),
    COALESCE(progress.experience, 0)::text,
    (users.email_verified_at IS NOT NULL),
    COALESCE(access.vip_enabled AND (access.vip_until IS NULL OR access.vip_until > $2), false),
    access.vip_until
FROM identity.users AS users
LEFT JOIN identity.user_activity AS activity ON activity.user_id = users.id
LEFT JOIN identity.user_access_states AS access ON access.user_id = users.id
LEFT JOIN traffic.user_totals AS traffic ON traffic.user_id = users.id
LEFT JOIN economy.magic_accounts AS magic ON magic.user_id = users.id
LEFT JOIN progression.user_progress AS progress ON progress.user_id = users.id
WHERE users.id = $1
  AND users.status = 'active'`, userID, now).Scan(
		&result.NumericID,
		&result.Username,
		&result.DisplayName,
		&result.Level,
		&result.RegisteredAt,
		&lastActive,
		&result.Uploaded,
		&result.Downloaded,
		&result.Magic,
		&experienceText,
		&result.EmailVerified,
		&result.VIP,
		&vipUntil,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, personalapikey.ErrInvalid
	}
	if err != nil {
		return Profile{}, fmt.Errorf("read MoviePilot profile: %w", err)
	}
	experience, err := strconv.ParseFloat(experienceText, 64)
	if err != nil {
		return Profile{}, fmt.Errorf("parse MoviePilot profile experience: %w", err)
	}
	result.Experience = experience
	result.RegisteredAt = result.RegisteredAt.UTC()
	result.LastActiveAt = optionalTime(lastActive)
	result.VIPUntil = optionalTime(vipUntil)
	return result, nil
}

// LatestSeedingReward reads the most recent completed hourly settlement. It
// deliberately does not calculate or persist a second real-time reward model
// for integrations, so polling clients cannot grow PostgreSQL history.
func (repository *PostgresRepository) LatestSeedingReward(ctx context.Context, userID uuid.UUID) (int64, error) {
	var reward int64
	err := repository.pool.QueryRow(ctx, `
SELECT reward
FROM economy.seeding_reward_calculations
WHERE user_id = $1
ORDER BY window_start DESC
LIMIT 1`, userID).Scan(&reward)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read integration seeding reward: %w", err)
	}
	return reward, nil
}

func (repository *PostgresRepository) PublicProfile(ctx context.Context, username string, now time.Time) (Profile, error) {
	var result Profile
	var lastActive, vipUntil pgtype.Timestamptz
	var experienceText string
	err := repository.pool.QueryRow(ctx, `
SELECT
    users.numeric_id,
    users.username,
    users.display_name,
    COALESCE(progress.level, 1),
    users.created_at,
    activity.last_active_at,
    COALESCE(traffic.credited_uploaded, 0),
    COALESCE(traffic.charged_downloaded, 0),
    COALESCE(magic.balance, 0),
    COALESCE(progress.experience, 0)::text,
    (users.email_verified_at IS NOT NULL),
    COALESCE(access.vip_enabled AND (access.vip_until IS NULL OR access.vip_until > $2), false),
    access.vip_until
FROM identity.users AS users
LEFT JOIN identity.user_activity AS activity ON activity.user_id = users.id
LEFT JOIN identity.user_access_states AS access ON access.user_id = users.id
LEFT JOIN traffic.user_totals AS traffic ON traffic.user_id = users.id
LEFT JOIN economy.magic_accounts AS magic ON magic.user_id = users.id
LEFT JOIN progression.user_progress AS progress ON progress.user_id = users.id
WHERE lower(users.username) = lower($1)
  AND users.status = 'active'`, username, now).Scan(
		&result.NumericID, &result.Username, &result.DisplayName, &result.Level,
		&result.RegisteredAt, &lastActive, &result.Uploaded, &result.Downloaded,
		&result.Magic, &experienceText, &result.EmailVerified, &result.VIP, &vipUntil,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("read legacy public profile: %w", err)
	}
	experience, err := strconv.ParseFloat(experienceText, 64)
	if err != nil {
		return Profile{}, fmt.Errorf("parse legacy public profile experience: %w", err)
	}
	result.Experience = experience
	result.RegisteredAt = result.RegisteredAt.UTC()
	result.LastActiveAt = optionalTime(lastActive)
	result.VIPUntil = optionalTime(vipUntil)
	return result, nil
}

func (repository *PostgresRepository) ResolveTorrentID(ctx context.Context, raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if numericID, err := strconv.ParseInt(raw, 10, 64); err == nil && numericID > 0 {
		return numericID, nil
	}
	return 0, ErrInput
}

func (repository *PostgresRepository) TorrentMetadata(ctx context.Context, torrentID int64) (TorrentMetadata, error) {
	metadata, err := scanTorrentMetadata(repository.pool.QueryRow(ctx, legacyTorrentMetadataSQL+` WHERE torrent.id = $1 AND torrent.state = 'published'`, torrentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return TorrentMetadata{}, ErrNotFound
	}
	if err != nil {
		return TorrentMetadata{}, fmt.Errorf("read legacy torrent metadata: %w", err)
	}
	return metadata, nil
}

func (repository *PostgresRepository) TorrentMetadataBatch(ctx context.Context, torrentIDs []int64) (map[int64]TorrentMetadata, error) {
	result := make(map[int64]TorrentMetadata, len(torrentIDs))
	if len(torrentIDs) == 0 {
		return result, nil
	}
	rows, err := repository.pool.Query(ctx, legacyTorrentMetadataSQL+`
WHERE torrent.id = ANY($1::bigint[])
  AND torrent.state = 'published'`, torrentIDs)
	if err != nil {
		return nil, fmt.Errorf("list legacy torrent metadata: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		metadata, err := scanTorrentMetadata(rows)
		if err != nil {
			return nil, fmt.Errorf("scan legacy torrent metadata: %w", err)
		}
		result[metadata.TorrentID] = metadata
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate legacy torrent metadata: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) UserNumericIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	result := make(map[uuid.UUID]int64, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}
	rows, err := repository.pool.Query(ctx, `
SELECT id, numeric_id
FROM identity.users
WHERE id = ANY($1::uuid[])`, userIDs)
	if err != nil {
		return nil, fmt.Errorf("list legacy comment author IDs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var userID uuid.UUID
		var numericID int64
		if err := rows.Scan(&userID, &numericID); err != nil {
			return nil, fmt.Errorf("scan legacy comment author ID: %w", err)
		}
		result[userID] = numericID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate legacy comment author IDs: %w", err)
	}
	return result, nil
}

const legacyTorrentMetadataSQL = `
SELECT
    torrent.id,
    torrent.id::text,
    CASE WHEN torrent.anonymous THEN '匿名' ELSE uploader.username END,
    CASE WHEN torrent.anonymous THEN 0 ELSE uploader.numeric_id END,
    torrent.anonymous,
    torrent.purchase_price
FROM torrents.torrents AS torrent
JOIN identity.users AS uploader ON uploader.id = torrent.uploader_id`

type legacyMetadataRow interface {
	Scan(...any) error
}

func scanTorrentMetadata(row legacyMetadataRow) (TorrentMetadata, error) {
	var result TorrentMetadata
	err := row.Scan(
		&result.TorrentID, &result.LegacyRouteID, &result.Uploader,
		&result.UploaderID, &result.Anonymous, &result.PurchasePrice,
	)
	return result, err
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

var _ Repository = (*PostgresRepository)(nil)
var _ LegacyRepository = (*PostgresRepository)(nil)
