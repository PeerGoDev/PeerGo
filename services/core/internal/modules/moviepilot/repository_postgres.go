package moviepilot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/identity"
)

type Repository interface {
	Credential(context.Context, uuid.UUID) (Credential, error)
	RotateCredential(context.Context, uuid.UUID, *int64, []byte, string, time.Time) (Credential, error)
	RevokeCredential(context.Context, uuid.UUID, int64) error
	Authenticate(context.Context, []byte, time.Time) (AuthenticatedCredential, error)
	ResolveCapabilityUser(context.Context, uuid.UUID, int64, time.Time) (identity.User, error)
	Profile(context.Context, uuid.UUID, time.Time) (Profile, error)
}

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("MoviePilot PostgreSQL pool is required")
	}
	return &PostgresRepository{pool: pool}, nil
}

func (repository *PostgresRepository) Credential(ctx context.Context, userID uuid.UUID) (Credential, error) {
	var result Credential
	var lastUsed pgtype.Timestamptz
	err := repository.pool.QueryRow(ctx, `
SELECT user_id, token_prefix, version, created_at, last_used_at
FROM identity.moviepilot_credentials
WHERE user_id = $1`, userID).Scan(
		&result.UserID, &result.KeyPrefix, &result.Version, &result.CreatedAt, &lastUsed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credential{}, ErrCredentialNotFound
	}
	if err != nil {
		return Credential{}, fmt.Errorf("read MoviePilot credential: %w", err)
	}
	result.CreatedAt = result.CreatedAt.UTC()
	result.LastUsedAt = optionalTime(lastUsed)
	return result, nil
}

func (repository *PostgresRepository) RotateCredential(ctx context.Context, userID uuid.UUID, expectedVersion *int64, tokenHash []byte, prefix string, now time.Time) (Credential, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Credential{}, fmt.Errorf("begin MoviePilot credential rotation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentVersion int64
	err = tx.QueryRow(ctx, `
SELECT version
FROM identity.moviepilot_credentials
WHERE user_id = $1
FOR UPDATE`, userID).Scan(&currentVersion)
	switch {
	case errors.Is(err, pgx.ErrNoRows) && expectedVersion != nil:
		return Credential{}, ErrCredentialConflict
	case errors.Is(err, pgx.ErrNoRows):
		currentVersion = 0
	case err != nil:
		return Credential{}, fmt.Errorf("lock MoviePilot credential: %w", err)
	case expectedVersion == nil || *expectedVersion != currentVersion:
		return Credential{}, ErrCredentialConflict
	}

	version := currentVersion + 1
	_, err = tx.Exec(ctx, `
INSERT INTO identity.moviepilot_credentials (
    user_id, token_hash, token_prefix, version, created_at, last_used_at, updated_at
) VALUES ($1, $2, $3, $4, $5, NULL, $5)
ON CONFLICT (user_id) DO UPDATE SET
    token_hash = EXCLUDED.token_hash,
    token_prefix = EXCLUDED.token_prefix,
    version = EXCLUDED.version,
    created_at = EXCLUDED.created_at,
    last_used_at = NULL,
    updated_at = EXCLUDED.updated_at`,
		userID, tokenHash, prefix, version, now,
	)
	if err != nil {
		if moviePilotCredentialWriteConflict(err) {
			return Credential{}, ErrCredentialConflict
		}
		return Credential{}, fmt.Errorf("write MoviePilot credential: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		if moviePilotCredentialWriteConflict(err) {
			return Credential{}, ErrCredentialConflict
		}
		return Credential{}, fmt.Errorf("commit MoviePilot credential rotation: %w", err)
	}
	return Credential{UserID: userID, KeyPrefix: prefix, Version: version, CreatedAt: now}, nil
}

func (repository *PostgresRepository) RevokeCredential(ctx context.Context, userID uuid.UUID, expectedVersion int64) error {
	result, err := repository.pool.Exec(ctx, `
DELETE FROM identity.moviepilot_credentials
WHERE user_id = $1 AND version = $2`, userID, expectedVersion)
	if err != nil {
		return fmt.Errorf("revoke MoviePilot credential: %w", err)
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	var exists bool
	if err := repository.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM identity.moviepilot_credentials WHERE user_id = $1
)`, userID).Scan(&exists); err != nil {
		return fmt.Errorf("classify MoviePilot credential revocation: %w", err)
	}
	if exists {
		return ErrCredentialConflict
	}
	return ErrCredentialNotFound
}

func (repository *PostgresRepository) Authenticate(ctx context.Context, tokenHash []byte, now time.Time) (AuthenticatedCredential, error) {
	var result AuthenticatedCredential
	var emailVerified pgtype.Timestamptz
	var lastUsed pgtype.Timestamptz
	err := repository.pool.QueryRow(ctx, `
SELECT
    credential.user_id,
    credential.token_prefix,
    credential.version,
    credential.created_at,
    credential.last_used_at,
    users.id,
    users.credential_ref,
    users.username,
    users.display_name,
    users.email_verified_at,
    users.numeric_id
FROM identity.moviepilot_credentials AS credential
JOIN identity.users AS users ON users.id = credential.user_id
WHERE credential.token_hash = $1
  AND users.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= $2
        AND restriction.expires_at > $2
  )`, tokenHash, now).Scan(
		&result.Credential.UserID,
		&result.Credential.KeyPrefix,
		&result.Credential.Version,
		&result.Credential.CreatedAt,
		&lastUsed,
		&result.User.ID,
		&result.User.CredentialRef,
		&result.User.Username,
		&result.User.DisplayName,
		&emailVerified,
		&result.NumericID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthenticatedCredential{}, ErrCredentialInvalid
	}
	if err != nil {
		return AuthenticatedCredential{}, fmt.Errorf("authenticate MoviePilot credential: %w", err)
	}
	result.Credential.CreatedAt = result.Credential.CreatedAt.UTC()
	result.Credential.LastUsedAt = optionalTime(lastUsed)
	result.User.EmailVerifiedAt = optionalTime(emailVerified)

	// Coalesce status activity so frequent searches produce at most one small
	// row update per key per six hours instead of a durable request log.
	if _, err := repository.pool.Exec(ctx, `
UPDATE identity.moviepilot_credentials
SET last_used_at = $2, updated_at = GREATEST(updated_at, $2)
WHERE user_id = $1
  AND version = $3
  AND (last_used_at IS NULL OR last_used_at <= $2 - interval '6 hours')`,
		result.User.ID, now, result.Credential.Version,
	); err != nil {
		return AuthenticatedCredential{}, fmt.Errorf("touch MoviePilot credential: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) ResolveCapabilityUser(ctx context.Context, userID uuid.UUID, version int64, now time.Time) (identity.User, error) {
	var user identity.User
	var emailVerified pgtype.Timestamptz
	err := repository.pool.QueryRow(ctx, `
SELECT users.id, users.credential_ref, users.username, users.display_name, users.email_verified_at
FROM identity.moviepilot_credentials AS credential
JOIN identity.users AS users ON users.id = credential.user_id
WHERE credential.user_id = $1
  AND credential.version = $2
  AND users.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= $3
        AND restriction.expires_at > $3
  )`, userID, version, now).Scan(
		&user.ID, &user.CredentialRef, &user.Username, &user.DisplayName, &emailVerified,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.User{}, ErrCapabilityInvalid
	}
	if err != nil {
		return identity.User{}, fmt.Errorf("resolve MoviePilot download capability: %w", err)
	}
	user.EmailVerifiedAt = optionalTime(emailVerified)
	return user, nil
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
		return Profile{}, ErrCredentialInvalid
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

func moviePilotCredentialWriteConflict(err error) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return false
	}
	switch postgresError.Code {
	case "23505", "40001", "40P01":
		return true
	default:
		return false
	}
}

var _ Repository = (*PostgresRepository)(nil)
