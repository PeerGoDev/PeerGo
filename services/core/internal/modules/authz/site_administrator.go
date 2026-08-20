package authz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/contracts/go/authzcontractv1"
)

var (
	ErrSiteAdministratorInput = errors.New("site administrator input is invalid")
	ErrSiteAdministratorUser  = errors.New("site administrator user was not found")
	ErrSiteAdministratorState = errors.New("site administrator user is not active")
)

// SiteAdministratorChange is the operator-facing result. Mandate and grant
// identifiers deliberately stay internal: a site owner only needs to know
// which account changed and whether the command actually mutated authority.
type SiteAdministratorChange struct {
	UserID   uuid.UUID
	Username string
	Enabled  bool
	Changed  bool
}

// SiteAdministratorRepository implements the intentionally small operator
// workflow behind `make admin`. Stable UUIDs make retries idempotent, while a
// transaction-level advisory lock prevents concurrent grant/revoke commands
// from racing each other for the same account.
type SiteAdministratorRepository struct {
	db *pgxpool.Pool
}

func NewSiteAdministratorRepository(db *pgxpool.Pool) (*SiteAdministratorRepository, error) {
	if db == nil {
		return nil, errors.New("site administrator database is required")
	}
	return &SiteAdministratorRepository{db: db}, nil
}

func (repository *SiteAdministratorRepository) Set(ctx context.Context, username string, enabled bool, changedAt time.Time) (SiteAdministratorChange, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(username) > 64 || changedAt.IsZero() {
		return SiteAdministratorChange{}, ErrSiteAdministratorInput
	}
	changedAt = changedAt.UTC()

	tx, err := repository.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return SiteAdministratorChange{}, fmt.Errorf("begin site administrator change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// This lock has no user-controlled key material and only serializes the rare
	// local operator command; normal HTTP authorization never waits on it.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('peergo.site-administrator', 0))`); err != nil {
		return SiteAdministratorChange{}, fmt.Errorf("lock site administrator changes: %w", err)
	}

	var userID uuid.UUID
	var canonicalUsername, status string
	err = tx.QueryRow(ctx, `
SELECT id, username, status
FROM identity.users
WHERE lower(username) = lower($1)
FOR UPDATE`, username).Scan(&userID, &canonicalUsername, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return SiteAdministratorChange{}, ErrSiteAdministratorUser
	}
	if err != nil {
		return SiteAdministratorChange{}, fmt.Errorf("load site administrator user: %w", err)
	}
	if enabled && status != "active" {
		return SiteAdministratorChange{}, ErrSiteAdministratorState
	}

	mandateID, grantID := siteAdministratorIDs(userID)
	currentlyEnabled, err := currentSiteAdministratorState(ctx, tx, userID, mandateID, grantID, changedAt)
	if err != nil {
		return SiteAdministratorChange{}, err
	}
	result := SiteAdministratorChange{
		UserID: userID, Username: canonicalUsername, Enabled: enabled,
		Changed: currentlyEnabled != enabled,
	}
	if !result.Changed {
		if err := tx.Commit(ctx); err != nil {
			return SiteAdministratorChange{}, fmt.Errorf("commit unchanged site administrator state: %w", err)
		}
		return result, nil
	}

	if enabled {
		if err := enableSiteAdministrator(ctx, tx, userID, mandateID, grantID, changedAt); err != nil {
			return SiteAdministratorChange{}, err
		}
	} else if err := disableSiteAdministrator(ctx, tx, userID, mandateID, grantID, changedAt); err != nil {
		return SiteAdministratorChange{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return SiteAdministratorChange{}, fmt.Errorf("commit site administrator change: %w", err)
	}
	return result, nil
}

func currentSiteAdministratorState(ctx context.Context, tx pgx.Tx, userID, mandateID, grantID uuid.UUID, asOf time.Time) (bool, error) {
	var enabled bool
	err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM authz.grants AS grant_record
    JOIN governance.mandates AS mandate
      ON mandate.id = grant_record.mandate_id
     AND mandate.subject_id = grant_record.subject_id
    WHERE grant_record.id = $1
      AND grant_record.subject_id = $2
      AND grant_record.role_id = 'site_admin'
      AND grant_record.mandate_id = $3
      AND grant_record.scope_type = $4
      AND grant_record.scope_id = $5
      AND grant_record.revoked_at IS NULL
      AND grant_record.valid_from <= $6
      AND $6 < grant_record.valid_until
      AND mandate.status = 'active'
      AND mandate.starts_at <= $6
      AND $6 < mandate.ends_at
)`, grantID, userID, mandateID, authzcontractv1.SiteScopeType, authzcontractv1.SiteScopeID, asOf).Scan(&enabled)
	if err != nil {
		return false, fmt.Errorf("read site administrator state: %w", err)
	}
	return enabled, nil
}

func enableSiteAdministrator(ctx context.Context, tx pgx.Tx, userID, mandateID, grantID uuid.UUID, changedAt time.Time) error {
	// PostgreSQL requires a finite end time in the legacy grant schema. A
	// century-long internal horizon behaves as a non-expiring administrator to
	// operators without weakening the existing time-subset invariants.
	validUntil := changedAt.AddDate(100, 0, 0)
	commandStart := changedAt.Add(-time.Second)
	commandEnd := validUntil.Add(time.Second)

	commandTag, err := tx.Exec(ctx, `
INSERT INTO governance.mandates (
    id, subject_id, source_type, source_reference, scope_type, scope_id,
    starts_at, ends_at, status, approved_by, created_at, updated_at
) VALUES ($1, $2, 'bootstrap', 'site-admin-cli', $3, $4, $5, $6, 'active', NULL, $7, $7)
ON CONFLICT (id) DO UPDATE SET
    starts_at = EXCLUDED.starts_at,
    ends_at = EXCLUDED.ends_at,
    status = 'active',
    approved_by = NULL,
    updated_at = EXCLUDED.updated_at
WHERE governance.mandates.subject_id = EXCLUDED.subject_id
  AND governance.mandates.source_type = 'bootstrap'
  AND governance.mandates.source_reference = 'site-admin-cli'
  AND governance.mandates.scope_type = EXCLUDED.scope_type
  AND governance.mandates.scope_id = EXCLUDED.scope_id`,
		mandateID, userID, authzcontractv1.SiteScopeType, authzcontractv1.SiteScopeID,
		commandStart, commandEnd, changedAt)
	if err != nil {
		return fmt.Errorf("upsert site administrator authority: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return errors.New("site administrator mandate ID conflicts with another authority")
	}

	commandTag, err = tx.Exec(ctx, `
INSERT INTO authz.grants (
    id, subject_id, role_id, mandate_id, scope_type, scope_id,
    valid_from, valid_until, constraints, version, revoked_at, created_at, updated_at
) VALUES ($1, $2, 'site_admin', $3, $4, $5, $6, $7, '{}'::jsonb, 1, NULL, $6, $6)
ON CONFLICT (id) DO UPDATE SET
    valid_from = EXCLUDED.valid_from,
    valid_until = EXCLUDED.valid_until,
    constraints = '{}'::jsonb,
    version = authz.grants.version + 1,
    revoked_at = NULL,
    updated_at = EXCLUDED.updated_at
WHERE authz.grants.subject_id = EXCLUDED.subject_id
  AND authz.grants.role_id = 'site_admin'
  AND authz.grants.mandate_id = EXCLUDED.mandate_id
  AND authz.grants.scope_type = EXCLUDED.scope_type
  AND authz.grants.scope_id = EXCLUDED.scope_id`,
		grantID, userID, mandateID, authzcontractv1.SiteScopeType,
		authzcontractv1.SiteScopeID, changedAt, validUntil)
	if err != nil {
		return fmt.Errorf("upsert site administrator grant: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return errors.New("site administrator grant ID conflicts with another authority")
	}
	return nil
}

func disableSiteAdministrator(ctx context.Context, tx pgx.Tx, userID, mandateID, grantID uuid.UUID, changedAt time.Time) error {
	if _, err := tx.Exec(ctx, `
UPDATE authz.grants
SET revoked_at = $1, version = version + 1, updated_at = $1
WHERE id = $2 AND subject_id = $3 AND role_id = 'site_admin' AND mandate_id = $4`,
		changedAt, grantID, userID, mandateID); err != nil {
		return fmt.Errorf("revoke site administrator grant: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE governance.mandates
SET status = 'revoked', updated_at = $1
WHERE id = $2 AND subject_id = $3 AND source_reference = 'site-admin-cli'`,
		changedAt, mandateID, userID); err != nil {
		return fmt.Errorf("revoke site administrator authority: %w", err)
	}
	// Existing passkey staff sessions must not survive a CLI revocation. The
	// ordinary account session may remain logged in but loses admin access on
	// its next request because every request re-evaluates the grant.
	if _, err := tx.Exec(ctx, `
UPDATE identity.sessions
SET revoked_at = $1
WHERE user_id = $2 AND audience = 'staff' AND revoked_at IS NULL`, changedAt, userID); err != nil {
		return fmt.Errorf("revoke site administrator staff sessions: %w", err)
	}
	return nil
}

func siteAdministratorIDs(userID uuid.UUID) (uuid.UUID, uuid.UUID) {
	mandateID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("peergo:site-admin:mandate:"+userID.String()))
	grantID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("peergo:site-admin:grant:"+userID.String()))
	return mandateID, grantID
}
