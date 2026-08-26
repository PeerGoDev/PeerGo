package moviepilot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/personalapikey"
)

// Repository contains only MoviePilot's canonical profile projection. Shared
// personal-key persistence belongs to the personalapikey module.
type Repository interface {
	Profile(context.Context, uuid.UUID, time.Time) (Profile, error)
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

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

var _ Repository = (*PostgresRepository)(nil)
