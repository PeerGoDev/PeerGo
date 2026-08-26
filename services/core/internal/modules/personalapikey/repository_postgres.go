package personalapikey

import (
	"context"
	"errors"
	"fmt"
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
	RotateCredential(context.Context, uuid.UUID, *int64, []byte, string, []Scope, time.Time) (Credential, error)
	RevokeCredential(context.Context, uuid.UUID, int64) error
	Authenticate(context.Context, []byte, time.Time) (AuthenticatedCredential, error)
	ResolveActiveUser(context.Context, uuid.UUID, int64, Scope, time.Time) (identity.User, error)
}

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("personal API key PostgreSQL pool is required")
	}
	return &PostgresRepository{pool: pool}, nil
}

func (repository *PostgresRepository) Credential(ctx context.Context, userID uuid.UUID) (Credential, error) {
	var result Credential
	var scopes []string
	var lastUsed pgtype.Timestamptz
	err := repository.pool.QueryRow(ctx, `
SELECT user_id, token_prefix, version, scopes, created_at, last_used_at
FROM identity.personal_api_keys
WHERE user_id = $1`, userID).Scan(
		&result.UserID, &result.KeyPrefix, &result.Version, &scopes, &result.CreatedAt, &lastUsed,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credential{}, ErrNotFound
	}
	if err != nil {
		return Credential{}, fmt.Errorf("read personal API key: %w", err)
	}
	result.Scopes, err = scopesFromStrings(scopes)
	if err != nil {
		return Credential{}, fmt.Errorf("read personal API key scopes: %w", err)
	}
	result.CreatedAt = result.CreatedAt.UTC()
	result.LastUsedAt = optionalTime(lastUsed)
	return result, nil
}

func (repository *PostgresRepository) RotateCredential(ctx context.Context, userID uuid.UUID, expectedVersion *int64, tokenHash []byte, prefix string, scopes []Scope, now time.Time) (Credential, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Credential{}, fmt.Errorf("begin personal API key rotation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentVersion int64
	err = tx.QueryRow(ctx, `
SELECT version
FROM identity.personal_api_keys
WHERE user_id = $1
FOR UPDATE`, userID).Scan(&currentVersion)
	switch {
	case errors.Is(err, pgx.ErrNoRows) && expectedVersion != nil:
		return Credential{}, ErrConflict
	case errors.Is(err, pgx.ErrNoRows):
		currentVersion = 0
	case err != nil:
		return Credential{}, fmt.Errorf("lock personal API key: %w", err)
	case expectedVersion == nil || *expectedVersion != currentVersion:
		return Credential{}, ErrConflict
	}

	version := currentVersion + 1
	_, err = tx.Exec(ctx, `
INSERT INTO identity.personal_api_keys (
    user_id, token_hash, token_prefix, version, scopes, created_at, last_used_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, NULL, $6)
ON CONFLICT (user_id) DO UPDATE SET
    token_hash = EXCLUDED.token_hash,
    token_prefix = EXCLUDED.token_prefix,
    version = EXCLUDED.version,
    scopes = EXCLUDED.scopes,
    created_at = EXCLUDED.created_at,
    last_used_at = NULL,
    updated_at = EXCLUDED.updated_at`,
		userID, tokenHash, prefix, version, scopeStrings(scopes), now,
	)
	if err != nil {
		if credentialWriteConflict(err) {
			return Credential{}, ErrConflict
		}
		return Credential{}, fmt.Errorf("write personal API key: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		if credentialWriteConflict(err) {
			return Credential{}, ErrConflict
		}
		return Credential{}, fmt.Errorf("commit personal API key rotation: %w", err)
	}
	return Credential{
		UserID: userID, KeyPrefix: prefix, Version: version,
		Scopes: append([]Scope(nil), scopes...), CreatedAt: now,
	}, nil
}

func (repository *PostgresRepository) RevokeCredential(ctx context.Context, userID uuid.UUID, expectedVersion int64) error {
	result, err := repository.pool.Exec(ctx, `
DELETE FROM identity.personal_api_keys
WHERE user_id = $1 AND version = $2`, userID, expectedVersion)
	if err != nil {
		return fmt.Errorf("revoke personal API key: %w", err)
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	var exists bool
	if err := repository.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM identity.personal_api_keys WHERE user_id = $1
)`, userID).Scan(&exists); err != nil {
		return fmt.Errorf("classify personal API key revocation: %w", err)
	}
	if exists {
		return ErrConflict
	}
	return ErrNotFound
}

func (repository *PostgresRepository) Authenticate(ctx context.Context, tokenHash []byte, now time.Time) (AuthenticatedCredential, error) {
	var result AuthenticatedCredential
	var scopes []string
	var emailVerified, lastUsed pgtype.Timestamptz
	err := repository.pool.QueryRow(ctx, `
SELECT
    credential.user_id,
    credential.token_prefix,
    credential.version,
    credential.scopes,
    credential.created_at,
    credential.last_used_at,
    users.id,
    users.credential_ref,
    users.username,
    users.display_name,
    users.email_verified_at,
    users.numeric_id
FROM identity.personal_api_keys AS credential
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
		&scopes,
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
		return AuthenticatedCredential{}, ErrInvalid
	}
	if err != nil {
		return AuthenticatedCredential{}, fmt.Errorf("authenticate personal API key: %w", err)
	}
	result.Credential.Scopes, err = scopesFromStrings(scopes)
	if err != nil {
		return AuthenticatedCredential{}, fmt.Errorf("authenticate personal API key scopes: %w", err)
	}
	result.Credential.CreatedAt = result.Credential.CreatedAt.UTC()
	result.Credential.LastUsedAt = optionalTime(lastUsed)
	result.User.EmailVerifiedAt = optionalTime(emailVerified)

	// Frequent tool traffic produces at most one small status write per key per
	// six hours. PeerGo does not persist an API request log here.
	if _, err := repository.pool.Exec(ctx, `
UPDATE identity.personal_api_keys
SET last_used_at = $2, updated_at = GREATEST(updated_at, $2)
WHERE user_id = $1
  AND version = $3
  AND (last_used_at IS NULL OR last_used_at <= $2 - interval '6 hours')`,
		result.User.ID, now, result.Credential.Version,
	); err != nil {
		return AuthenticatedCredential{}, fmt.Errorf("touch personal API key: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) ResolveActiveUser(ctx context.Context, userID uuid.UUID, version int64, requiredScope Scope, now time.Time) (identity.User, error) {
	var user identity.User
	var emailVerified pgtype.Timestamptz
	err := repository.pool.QueryRow(ctx, `
SELECT users.id, users.credential_ref, users.username, users.display_name, users.email_verified_at
FROM identity.personal_api_keys AS credential
JOIN identity.users AS users ON users.id = credential.user_id
WHERE credential.user_id = $1
  AND credential.version = $2
  AND $3 = ANY(credential.scopes)
  AND users.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM identity.account_restrictions AS restriction
      WHERE restriction.user_id = users.id
        AND restriction.kind = 'account_access'
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= $4
        AND restriction.expires_at > $4
  )`, userID, version, string(requiredScope), now).Scan(
		&user.ID, &user.CredentialRef, &user.Username, &user.DisplayName, &emailVerified,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.User{}, ErrInvalid
	}
	if err != nil {
		return identity.User{}, fmt.Errorf("resolve personal API key user: %w", err)
	}
	user.EmailVerifiedAt = optionalTime(emailVerified)
	return user, nil
}

func scopesFromStrings(values []string) ([]Scope, error) {
	result := make([]Scope, 0, len(values))
	for _, value := range values {
		result = append(result, Scope(value))
	}
	normalized, err := NormalizeScopes(result)
	if err != nil {
		return nil, ErrInvalid
	}
	return normalized, nil
}

func scopeStrings(values []Scope) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func credentialWriteConflict(err error) bool {
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
