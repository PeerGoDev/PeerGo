package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const accountAccessAppealSelect = `
SELECT
    appeal.id,
    appeal.user_id,
    users.numeric_id,
    users.username,
    appeal.source_kind,
    appeal.source_reason_code,
    appeal.source_reason_summary,
    appeal.source_starts_at,
    appeal.source_expires_at,
    appeal.source_version,
    appeal.statement,
    appeal.created_at,
    COALESCE(resolution.outcome, 'pending'),
    COALESCE(resolution.response, ''),
    resolution.created_at,
    users.status,
    users.administration_version,
	    restriction.version,
	    restriction.starts_at,
	    restriction.expires_at,
	    restriction.revoked_at,
	    access.download_restricted,
	    access.version
	FROM identity.account_access_appeals AS appeal
JOIN identity.users AS users ON users.id = appeal.user_id
LEFT JOIN identity.account_access_appeal_resolutions AS resolution
  ON resolution.appeal_id = appeal.id
	LEFT JOIN identity.account_restrictions AS restriction
	  ON restriction.id = appeal.source_restriction_id
	LEFT JOIN identity.user_access_states AS access
	  ON access.user_id = appeal.user_id
	`

type currentAccountAccessSource struct {
	UserID        uuid.UUID
	CredentialRef uuid.UUID
	UserNumericID int64
	Username      string
	Kind          AccountAccessSourceKind
	RestrictionID *uuid.UUID
	Version       int64
	ReasonCode    string
	ReasonSummary string
	StartsAt      time.Time
	ExpiresAt     *time.Time
	Restricted    bool
}

type accountAccessRowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (repository *PostgresRepository) StatusByCredentialRef(ctx context.Context, credentialRef uuid.UUID, asOf time.Time) (AccountAccessStatus, uuid.UUID, error) {
	if credentialRef == uuid.Nil || asOf.IsZero() {
		return AccountAccessStatus{}, uuid.Nil, ErrInvalidInput
	}
	source, err := loadCurrentAccountAccessSource(ctx, repository.db, credentialRef, uuid.Nil, asOf, false)
	if err != nil {
		return AccountAccessStatus{}, uuid.Nil, err
	}
	status := AccountAccessStatus{Restricted: source.Restricted, CanAppeal: source.Restricted}
	if source.Restricted {
		restriction := source.publicRestriction()
		status.Restriction = &restriction
	}

	appeal, err := repository.appealForSourceOrLatest(ctx, repository.db, source, asOf)
	if err == nil {
		status.Appeal = &appeal
		status.CanAppeal = false
		if status.Restriction == nil {
			restriction := appeal.Restriction
			status.Restriction = &restriction
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return AccountAccessStatus{}, uuid.Nil, err
	}
	return status, source.UserID, nil
}

func (repository *PostgresRepository) DownloadRestrictionStatusByUserID(ctx context.Context, userID uuid.UUID, asOf time.Time) (DownloadRestrictionStatus, error) {
	if userID == uuid.Nil || asOf.IsZero() {
		return DownloadRestrictionStatus{}, ErrInvalidInput
	}
	source, err := loadCurrentManualDownloadRestrictionSource(ctx, repository.db, userID, false)
	if err != nil {
		return DownloadRestrictionStatus{}, err
	}
	status := DownloadRestrictionStatus{
		Sources:   DownloadRestrictionSources{ManualOrLegacy: source.Restricted},
		CanAppeal: source.Restricted,
	}
	if err := repository.db.QueryRow(ctx, `
SELECT
    EXISTS (
        SELECT 1 FROM ratio_watch.assessments AS assessment
        WHERE assessment.user_id = $1
          AND assessment.status = 'download_restricted'
          AND assessment.resolved_at IS NULL
    ),
    EXISTS (
        SELECT 1 FROM traffic.user_hnr_obligations AS obligation
        WHERE obligation.user_id = $1
          AND obligation.state = 'tracking'
          AND obligation.grace_ends_at <= $2
          AND NOT EXISTS (
              SELECT 1 FROM traffic.hnr_appeal_exemptions AS exemption
              WHERE exemption.obligation_id = obligation.obligation_id
          )
    )`, userID, asOf).Scan(&status.Sources.RatioWatch, &status.Sources.HitAndRun); err != nil {
		return DownloadRestrictionStatus{}, fmt.Errorf("read download restriction source breakdown: %w", err)
	}
	status.Restricted = status.Sources.ManualOrLegacy || status.Sources.RatioWatch || status.Sources.HitAndRun
	if source.Restricted {
		restriction := source.publicRestriction()
		status.Restriction = &restriction
	}
	appeal, err := repository.appealForSourceOrLatest(ctx, repository.db, source, asOf)
	if err == nil {
		status.Appeal = &appeal
		if source.Restricted && appeal.Restriction.SourceVersion == source.Version {
			status.CanAppeal = false
		}
		if status.Restriction == nil {
			restriction := appeal.Restriction
			status.Restriction = &restriction
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return DownloadRestrictionStatus{}, err
	}
	return status, nil
}

func (repository *PostgresRepository) SubmitAccountAccessAppeal(ctx context.Context, command SubmitAccountAccessAppealCommand) (AccountAccessAppeal, error) {
	return repository.submitAccountAccessAppeal(ctx, command, false)
}

func (repository *PostgresRepository) SubmitDownloadRestrictionAppeal(ctx context.Context, command SubmitAccountAccessAppealCommand) (AccountAccessAppeal, error) {
	return repository.submitAccountAccessAppeal(ctx, command, true)
}

func (repository *PostgresRepository) submitAccountAccessAppeal(ctx context.Context, command SubmitAccountAccessAppealCommand, manualDownload bool) (AccountAccessAppeal, error) {
	if command.AppealID == uuid.Nil || command.UserID == uuid.Nil || command.CreatedAt.IsZero() || strings.TrimSpace(command.Statement) == "" {
		return AccountAccessAppeal{}, ErrInvalidInput
	}
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return AccountAccessAppeal{}, fmt.Errorf("begin account access appeal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	existing, err := scanAccountAccessAppeal(tx.QueryRow(ctx, accountAccessAppealSelect+`
WHERE appeal.id = $1`, command.AppealID), command.CreatedAt)
	if err == nil {
		wrongSurface := (manualDownload && existing.Restriction.SourceKind != AccountAccessSourceManualDownload) ||
			(!manualDownload && existing.Restriction.SourceKind == AccountAccessSourceManualDownload)
		if existing.UserID != command.UserID || existing.Statement != command.Statement || wrongSurface {
			return AccountAccessAppeal{}, ErrAccountAccessAppealIdempotency
		}
		existing.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return AccountAccessAppeal{}, fmt.Errorf("commit account access appeal replay: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AccountAccessAppeal{}, err
	}

	var source currentAccountAccessSource
	if manualDownload {
		source, err = loadCurrentManualDownloadRestrictionSource(ctx, tx, command.UserID, true)
	} else {
		source, err = loadCurrentAccountAccessSource(ctx, tx, uuid.Nil, command.UserID, command.CreatedAt, true)
	}
	if err != nil {
		return AccountAccessAppeal{}, err
	}
	if !source.Restricted {
		return AccountAccessAppeal{}, ErrAccountAccessNotRestricted
	}
	var duplicate bool
	switch source.Kind {
	case AccountAccessSourceTemporaryRestriction:
		err = tx.QueryRow(ctx, `SELECT EXISTS (
	            SELECT 1 FROM identity.account_access_appeals
	            WHERE source_restriction_id = $1
	        )`, *source.RestrictionID).Scan(&duplicate)
	case AccountAccessSourceDisabledAccount:
		err = tx.QueryRow(ctx, `SELECT EXISTS (
	            SELECT 1 FROM identity.account_access_appeals
	            WHERE user_id = $1 AND source_kind = 'disabled_account' AND source_version = $2
	        )`, source.UserID, source.Version).Scan(&duplicate)
	case AccountAccessSourceManualDownload:
		err = tx.QueryRow(ctx, `SELECT EXISTS (
	            SELECT 1 FROM identity.account_access_appeals
	            WHERE user_id = $1 AND source_kind = 'manual_download_restriction' AND source_version = $2
	        )`, source.UserID, source.Version).Scan(&duplicate)
	default:
		return AccountAccessAppeal{}, ErrAccountAccessNotRestricted
	}
	if err != nil {
		return AccountAccessAppeal{}, fmt.Errorf("check existing account access appeal: %w", err)
	}
	if duplicate {
		return AccountAccessAppeal{}, ErrAccountAccessAppealExists
	}

	var restrictionID any
	if source.RestrictionID != nil {
		restrictionID = *source.RestrictionID
	}
	var credentialVerifiedAt any
	if source.Kind != AccountAccessSourceManualDownload {
		credentialVerifiedAt = command.CreatedAt
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.account_access_appeals (
    id, user_id, source_kind, source_restriction_id, source_version,
    source_reason_code, source_reason_summary, source_starts_at,
    source_expires_at, statement, credential_verified_at, created_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		command.AppealID, source.UserID, source.Kind, restrictionID, source.Version,
		source.ReasonCode, source.ReasonSummary, source.StartsAt, source.ExpiresAt,
		command.Statement, credentialVerifiedAt, command.CreatedAt,
	); err != nil {
		return AccountAccessAppeal{}, classifyAccountAccessAppealWrite("insert account access appeal", err)
	}
	result, err := scanAccountAccessAppeal(tx.QueryRow(ctx, accountAccessAppealSelect+`
WHERE appeal.id = $1`, command.AppealID), command.CreatedAt)
	if err != nil {
		return AccountAccessAppeal{}, fmt.Errorf("read submitted account access appeal: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AccountAccessAppeal{}, classifyAccountAccessAppealWrite("commit account access appeal", err)
	}
	return result, nil
}

func (repository *PostgresRepository) ListAccountAccessAppeals(ctx context.Context, query AccountAccessAppealQuery, asOf time.Time) (AccountAccessAppealPage, error) {
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return AccountAccessAppealPage{}, fmt.Errorf("begin account access appeal list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	search := "%" + strings.ToLower(query.Query) + "%"
	filter := accountAccessAppealFilterSQL(query.Filter)
	active := accountAccessAppealActiveSQL("$1")
	var total int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)
FROM identity.account_access_appeals AS appeal
JOIN identity.users AS users ON users.id = appeal.user_id
LEFT JOIN identity.account_access_appeal_resolutions AS resolution
  ON resolution.appeal_id = appeal.id
	LEFT JOIN identity.account_restrictions AS restriction
	  ON restriction.id = appeal.source_restriction_id
	LEFT JOIN identity.user_access_states AS access
	  ON access.user_id = appeal.user_id
	WHERE $1::timestamptz IS NOT NULL
  AND ($2 = '%%' OR lower(users.username) LIKE $2 OR users.numeric_id::text = btrim($2, '%'))
  AND `+filter, asOf, search).Scan(&total); err != nil {
		return AccountAccessAppealPage{}, fmt.Errorf("count account access appeals: %w", err)
	}
	rows, err := tx.Query(ctx, accountAccessAppealSelect+`
WHERE ($2 = '%%' OR lower(users.username) LIKE $2 OR users.numeric_id::text = btrim($2, '%'))
  AND `+filter+`
ORDER BY (resolution.id IS NULL AND `+active+`) DESC,
         appeal.created_at ASC, appeal.id ASC
LIMIT $3 OFFSET $4`, asOf, search, query.Limit, query.Offset)
	if err != nil {
		return AccountAccessAppealPage{}, fmt.Errorf("list account access appeals: %w", err)
	}
	items := make([]AccountAccessAppeal, 0, query.Limit)
	for rows.Next() {
		item, scanErr := scanAccountAccessAppeal(rows, asOf)
		if scanErr != nil {
			rows.Close()
			return AccountAccessAppealPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AccountAccessAppealPage{}, fmt.Errorf("iterate account access appeals: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AccountAccessAppealPage{}, fmt.Errorf("commit account access appeal list: %w", err)
	}
	return AccountAccessAppealPage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func (repository *PostgresRepository) AccountAccessAppealDecisionPreflight(ctx context.Context, appealID uuid.UUID, asOf time.Time) (AccountAccessAppealDecisionPreflight, error) {
	if appealID == uuid.Nil || asOf.IsZero() {
		return AccountAccessAppealDecisionPreflight{}, ErrInvalidInput
	}
	appeal, err := scanAccountAccessAppeal(repository.db.QueryRow(ctx, accountAccessAppealSelect+`
WHERE appeal.id = $1`, appealID), asOf)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountAccessAppealDecisionPreflight{}, ErrAccountAccessAppealMissing
	}
	if err != nil {
		return AccountAccessAppealDecisionPreflight{}, err
	}
	if appeal.Status != AccountAccessAppealPending || !appeal.SourceActive {
		return AccountAccessAppealDecisionPreflight{}, ErrAccountAccessAppealConflict
	}
	var credentialRef uuid.UUID
	if err := repository.db.QueryRow(ctx, `SELECT credential_ref FROM identity.users WHERE id = $1`, appeal.UserID).Scan(&credentialRef); err != nil {
		return AccountAccessAppealDecisionPreflight{}, fmt.Errorf("read appeal credential reference: %w", err)
	}
	return AccountAccessAppealDecisionPreflight{
		UserID: appeal.UserID, CredentialRef: credentialRef,
		SourceKind: appeal.Restriction.SourceKind, SourceVersion: appeal.Restriction.SourceVersion,
	}, nil
}

func (repository *PostgresRepository) DecideAccountAccessAppeal(ctx context.Context, command DecideAccountAccessAppealCommand) (AccountAccessAppeal, error) {
	if command.AppealID == uuid.Nil || command.ActorID == uuid.Nil || command.DecidedAt.IsZero() ||
		command.Authorization.ID == uuid.Nil || !command.Authorization.Allow {
		return AccountAccessAppeal{}, ErrInvalidInput
	}
	tx, err := repository.db.Begin(ctx)
	if err != nil {
		return AccountAccessAppeal{}, fmt.Errorf("begin account access appeal decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		userID        uuid.UUID
		sourceKind    AccountAccessSourceKind
		restrictionID pgtype.UUID
		sourceVersion int64
		userStatus    string
		userVersion   int64
	)
	err = tx.QueryRow(ctx, `
SELECT appeal.user_id, appeal.source_kind, appeal.source_restriction_id,
       appeal.source_version, users.status, users.administration_version
FROM identity.account_access_appeals AS appeal
JOIN identity.users AS users ON users.id = appeal.user_id
WHERE appeal.id = $1
FOR UPDATE OF appeal, users`, command.AppealID).Scan(
		&userID, &sourceKind, &restrictionID, &sourceVersion, &userStatus, &userVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccountAccessAppeal{}, ErrAccountAccessAppealMissing
	}
	if err != nil {
		return AccountAccessAppeal{}, fmt.Errorf("lock account access appeal: %w", err)
	}
	if userID == command.ActorID {
		return AccountAccessAppeal{}, ErrAccountAccessAppealSelfTarget
	}
	if sourceVersion != command.ExpectedSourceVersion {
		return AccountAccessAppeal{}, ErrAccountAccessAppealConflict
	}
	var resolved bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM identity.account_access_appeal_resolutions WHERE appeal_id = $1
    )`, command.AppealID).Scan(&resolved); err != nil {
		return AccountAccessAppeal{}, fmt.Errorf("check account access appeal resolution: %w", err)
	}
	if resolved {
		return AccountAccessAppeal{}, ErrAccountAccessAppealConflict
	}

	switch sourceKind {
	case AccountAccessSourceDisabledAccount:
		if userStatus != string(AccountStatusDisabled) || userVersion != sourceVersion {
			return AccountAccessAppeal{}, ErrAccountAccessAppealConflict
		}
		if command.Decision == AccountAccessAppealDecisionApprove {
			rows, err := tx.Exec(ctx, `
UPDATE identity.users
SET status = 'active', administration_version = administration_version + 1,
    updated_at = GREATEST(updated_at, $2)
WHERE id = $1 AND status = 'disabled' AND administration_version = $3`,
				userID, command.DecidedAt, sourceVersion)
			if err != nil {
				return AccountAccessAppeal{}, fmt.Errorf("reactivate approved account: %w", err)
			}
			if rows.RowsAffected() != 1 {
				return AccountAccessAppeal{}, ErrAccountAccessAppealConflict
			}
		}
	case AccountAccessSourceTemporaryRestriction:
		if !restrictionID.Valid {
			return AccountAccessAppeal{}, ErrAccountAccessAppealConflict
		}
		var currentVersion int64
		var startsAt, expiresAt time.Time
		var revokedAt pgtype.Timestamptz
		if err := tx.QueryRow(ctx, `
SELECT version, starts_at, expires_at, revoked_at
FROM identity.account_restrictions
WHERE id = $1 AND user_id = $2
FOR UPDATE`, uuid.UUID(restrictionID.Bytes), userID).Scan(&currentVersion, &startsAt, &expiresAt, &revokedAt); err != nil {
			return AccountAccessAppeal{}, ErrAccountAccessAppealConflict
		}
		if currentVersion != sourceVersion || revokedAt.Valid || startsAt.After(command.DecidedAt) || !expiresAt.After(command.DecidedAt) {
			return AccountAccessAppeal{}, ErrAccountAccessAppealConflict
		}
		if command.Decision == AccountAccessAppealDecisionApprove {
			rows, err := tx.Exec(ctx, `
UPDATE identity.account_restrictions
SET revoked_at = $2, revoked_by = $3,
    revocation_reason_code = 'review_completed', revocation_reason = $4,
    version = version + 1, updated_at = $2
WHERE id = $1 AND version = $5 AND revoked_at IS NULL`,
				uuid.UUID(restrictionID.Bytes), command.DecidedAt, command.ActorID, command.Response, sourceVersion)
			if err != nil {
				return AccountAccessAppeal{}, fmt.Errorf("revoke approved account restriction: %w", err)
			}
			if rows.RowsAffected() != 1 {
				return AccountAccessAppeal{}, ErrAccountAccessAppealConflict
			}
			if _, err := tx.Exec(ctx, `
UPDATE identity.users
SET administration_version = administration_version + 1,
    updated_at = GREATEST(updated_at, $2)
WHERE id = $1`, userID, command.DecidedAt); err != nil {
				return AccountAccessAppeal{}, fmt.Errorf("advance account version after appeal approval: %w", err)
			}
		}
	case AccountAccessSourceManualDownload:
		if restrictionID.Valid {
			return AccountAccessAppeal{}, ErrAccountAccessAppealConflict
		}
		var currentRestricted bool
		var currentVersion int64
		if err := tx.QueryRow(ctx, `
SELECT download_restricted, version
FROM identity.user_access_states
WHERE user_id = $1
FOR UPDATE`, userID).Scan(&currentRestricted, &currentVersion); err != nil {
			return AccountAccessAppeal{}, ErrAccountAccessAppealConflict
		}
		if !currentRestricted || currentVersion != sourceVersion {
			return AccountAccessAppeal{}, ErrAccountAccessAppealConflict
		}
		if command.Decision == AccountAccessAppealDecisionApprove {
			rows, err := tx.Exec(ctx, `
UPDATE identity.user_access_states
SET download_restricted = false,
    download_restriction_origin = NULL,
    download_restriction_reason_code = NULL,
    download_restriction_reason = NULL,
    download_restriction_started_at = NULL,
    download_restriction_created_by = NULL,
    version = version + 1,
    updated_at = $2
WHERE user_id = $1 AND download_restricted AND version = $3`,
				userID, command.DecidedAt, sourceVersion)
			if err != nil {
				return AccountAccessAppeal{}, fmt.Errorf("clear approved manual download restriction: %w", err)
			}
			if rows.RowsAffected() != 1 {
				return AccountAccessAppeal{}, ErrAccountAccessAppealConflict
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO identity.manual_download_restriction_transitions (
    id, user_id, transition, origin, reason_code, reason,
    actor_id, appeal_id, from_restricted, to_restricted,
    from_state_version, state_version, occurred_at
) VALUES ($1, $2, 'revoked', 'appeal', 'appeal_approved', $3,
          $4, $5, true, false, $6, $6 + 1, $7)`,
				uuid.New(), userID, command.Response, command.ActorID,
				command.AppealID, sourceVersion, command.DecidedAt,
			); err != nil {
				return AccountAccessAppeal{}, fmt.Errorf("record approved manual download appeal transition: %w", err)
			}
			if _, err := tx.Exec(ctx, `
UPDATE identity.users
SET administration_version = administration_version + 1,
    updated_at = GREATEST(updated_at, $2)
WHERE id = $1`, userID, command.DecidedAt); err != nil {
				return AccountAccessAppeal{}, fmt.Errorf("advance account version after download appeal approval: %w", err)
			}
		}
	default:
		return AccountAccessAppeal{}, ErrAccountAccessAppealConflict
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO identity.account_access_appeal_resolutions (
    appeal_id, outcome, response, actor_id,
    authorization_decision_id, source_version, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		command.AppealID, command.Decision, command.Response, command.ActorID,
		command.Authorization.ID, sourceVersion, command.DecidedAt,
	); err != nil {
		return AccountAccessAppeal{}, classifyAccountAccessAppealWrite("insert account access appeal resolution", err)
	}
	result, err := scanAccountAccessAppeal(tx.QueryRow(ctx, accountAccessAppealSelect+`
WHERE appeal.id = $1`, command.AppealID), command.DecidedAt)
	if err != nil {
		return AccountAccessAppeal{}, fmt.Errorf("read decided account access appeal: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AccountAccessAppeal{}, classifyAccountAccessAppealWrite("commit account access appeal decision", err)
	}
	return result, nil
}

func loadCurrentAccountAccessSource(ctx context.Context, query accountAccessRowQuerier, credentialRef, userID uuid.UUID, asOf time.Time, lock bool) (currentAccountAccessSource, error) {
	where := "users.credential_ref = $1"
	argument := any(credentialRef)
	if userID != uuid.Nil {
		where = "users.id = $1"
		argument = userID
	}
	lockSQL := ""
	if lock {
		lockSQL = " FOR UPDATE"
	}
	var status string
	var updatedAt time.Time
	var source currentAccountAccessSource
	err := query.QueryRow(ctx, `
SELECT users.id, users.credential_ref, users.numeric_id, users.username,
       users.status, users.administration_version, users.updated_at
FROM identity.users AS users
WHERE `+where+lockSQL, argument).Scan(
		&source.UserID, &source.CredentialRef, &source.UserNumericID,
		&source.Username, &status, &source.Version, &updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return currentAccountAccessSource{}, ErrInvalidCredentials
	}
	if err != nil {
		return currentAccountAccessSource{}, fmt.Errorf("load account access source user: %w", err)
	}
	if status == string(AccountStatusDisabled) {
		source.Kind = AccountAccessSourceDisabledAccount
		source.Restricted = true
		source.ReasonCode = "account_disabled"
		source.ReasonSummary = "账户已被停用，请提交复核申请。"
		source.StartsAt = updatedAt.UTC().Round(0)
		var legacyReason pgtype.Text
		var bannedAt, bannedUntil pgtype.Timestamptz
		legacyErr := query.QueryRow(ctx, `
SELECT source_ban_reason, source_banned_at, source_banned_until
FROM migration.user_status_openings
WHERE user_id = $1 AND source_banned`, source.UserID).Scan(&legacyReason, &bannedAt, &bannedUntil)
		if legacyErr != nil && !errors.Is(legacyErr, pgx.ErrNoRows) {
			return currentAccountAccessSource{}, fmt.Errorf("load legacy ban opening: %w", legacyErr)
		}
		if legacyReason.Valid {
			value := strings.TrimSpace(legacyReason.String)
			if utf8RuneCount(value) >= 2 {
				source.ReasonCode = "legacy_ban"
				source.ReasonSummary = truncateRunes(value, 500)
			}
		}
		if bannedAt.Valid {
			source.StartsAt = bannedAt.Time.UTC().Round(0)
		}
		if bannedUntil.Valid {
			value := bannedUntil.Time.UTC().Round(0)
			source.ExpiresAt = &value
		}
		return source, nil
	}

	restrictionQuery := `
SELECT id, version, reason_code, reason_summary, starts_at, expires_at
FROM identity.account_restrictions
WHERE user_id = $1 AND kind = 'account_access'
  AND revoked_at IS NULL AND starts_at <= $2 AND expires_at > $2
ORDER BY starts_at DESC, id DESC
LIMIT 1`
	if lock {
		restrictionQuery += " FOR UPDATE"
	}
	var restrictionID uuid.UUID
	var expiresAt time.Time
	err = query.QueryRow(ctx, restrictionQuery, source.UserID, asOf).Scan(
		&restrictionID, &source.Version, &source.ReasonCode,
		&source.ReasonSummary, &source.StartsAt, &expiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		source.Restricted = false
		return source, nil
	}
	if err != nil {
		return currentAccountAccessSource{}, fmt.Errorf("load current temporary account restriction: %w", err)
	}
	source.Kind = AccountAccessSourceTemporaryRestriction
	source.RestrictionID = &restrictionID
	source.Restricted = true
	source.StartsAt = source.StartsAt.UTC().Round(0)
	value := expiresAt.UTC().Round(0)
	source.ExpiresAt = &value
	return source, nil
}

func loadCurrentManualDownloadRestrictionSource(ctx context.Context, query accountAccessRowQuerier, userID uuid.UUID, lock bool) (currentAccountAccessSource, error) {
	if userID == uuid.Nil {
		return currentAccountAccessSource{}, ErrInvalidInput
	}
	lockSQL := ""
	if lock {
		lockSQL = " FOR UPDATE"
	}
	var userStatus string
	var userUpdatedAt time.Time
	source := currentAccountAccessSource{Kind: AccountAccessSourceManualDownload}
	if err := query.QueryRow(ctx, `
SELECT id, credential_ref, numeric_id, username, status, updated_at
FROM identity.users
WHERE id = $1`+lockSQL, userID).Scan(
		&source.UserID, &source.CredentialRef, &source.UserNumericID,
		&source.Username, &userStatus, &userUpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return currentAccountAccessSource{}, ErrSessionNotFound
		}
		return currentAccountAccessSource{}, fmt.Errorf("load manual download restriction user: %w", err)
	}
	source.StartsAt = userUpdatedAt.UTC().Round(0)
	stateQuery := `
SELECT download_restricted, version,
       download_restriction_started_at,
       download_restriction_reason_code,
       download_restriction_reason
FROM identity.user_access_states
WHERE user_id = $1`
	if lock {
		stateQuery += " FOR UPDATE"
	}
	var startedAt pgtype.Timestamptz
	var reasonCode, reason pgtype.Text
	err := query.QueryRow(ctx, stateQuery, userID).Scan(
		&source.Restricted, &source.Version, &startedAt, &reasonCode, &reason,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Native accounts have no opening state until an explicit access-state
		// command creates one. Absence therefore means this source is inactive.
		source.Restricted = false
		source.Version = 0
		return source, nil
	}
	if err != nil {
		return currentAccountAccessSource{}, fmt.Errorf("load manual download restriction state: %w", err)
	}
	if source.Restricted {
		if !startedAt.Valid || !reasonCode.Valid || !reason.Valid {
			return currentAccountAccessSource{}, errors.New("manual download restriction is missing current metadata")
		}
		source.StartsAt = startedAt.Time.UTC().Round(0)
		source.ReasonCode = reasonCode.String
		source.ReasonSummary = reason.String
	}
	return source, nil
}

func (repository *PostgresRepository) appealForSourceOrLatest(ctx context.Context, query accountAccessRowQuerier, source currentAccountAccessSource, asOf time.Time) (AccountAccessAppeal, error) {
	where := "appeal.user_id = $1 AND appeal.source_kind IN ('temporary_restriction', 'disabled_account')"
	args := []any{source.UserID}
	if source.Restricted && source.Kind == AccountAccessSourceTemporaryRestriction {
		where = "appeal.source_restriction_id = $1"
		args[0] = *source.RestrictionID
	} else if source.Restricted && source.Kind == AccountAccessSourceDisabledAccount {
		where = "appeal.user_id = $1 AND appeal.source_kind = 'disabled_account' AND appeal.source_version = $2"
		args = append(args, source.Version)
	} else if source.Kind == AccountAccessSourceManualDownload {
		where = "appeal.user_id = $1 AND appeal.source_kind = 'manual_download_restriction'"
		if source.Restricted {
			where += " AND appeal.source_version = $2"
			args = append(args, source.Version)
		}
	}
	return scanAccountAccessAppeal(query.QueryRow(ctx, accountAccessAppealSelect+`
WHERE `+where+`
ORDER BY appeal.created_at DESC, appeal.id DESC
LIMIT 1`, args...), asOf)
}

func (source currentAccountAccessSource) publicRestriction() AccountAccessRestriction {
	return AccountAccessRestriction{
		SourceKind: source.Kind, ReasonCode: source.ReasonCode,
		ReasonSummary: source.ReasonSummary, StartsAt: source.StartsAt,
		ExpiresAt: source.ExpiresAt, SourceVersion: source.Version,
	}
}

func scanAccountAccessAppeal(scanner interface{ Scan(...any) error }, asOf time.Time) (AccountAccessAppeal, error) {
	var (
		appeal                                                                         AccountAccessAppeal
		sourceKind, status, userStatus                                                 string
		sourceExpiresAt, resolvedAt                                                    pgtype.Timestamptz
		currentRestrictionVersion                                                      pgtype.Int8
		currentRestrictionStarts, currentRestrictionExpires, currentRestrictionRevoked pgtype.Timestamptz
		currentDownloadRestricted                                                      pgtype.Bool
		currentAccessVersion                                                           pgtype.Int8
		currentUserVersion                                                             int64
	)
	if err := scanner.Scan(
		&appeal.ID, &appeal.UserID, &appeal.UserNumericID, &appeal.Username,
		&sourceKind, &appeal.Restriction.ReasonCode, &appeal.Restriction.ReasonSummary,
		&appeal.Restriction.StartsAt, &sourceExpiresAt, &appeal.Restriction.SourceVersion,
		&appeal.Statement, &appeal.CreatedAt, &status, &appeal.Response, &resolvedAt,
		&userStatus, &currentUserVersion, &currentRestrictionVersion,
		&currentRestrictionStarts, &currentRestrictionExpires, &currentRestrictionRevoked,
		&currentDownloadRestricted, &currentAccessVersion,
	); err != nil {
		return AccountAccessAppeal{}, err
	}
	appeal.Restriction.SourceKind = AccountAccessSourceKind(sourceKind)
	appeal.Status = AccountAccessAppealStatus(status)
	appeal.CreatedAt = appeal.CreatedAt.UTC().Round(0)
	appeal.Restriction.StartsAt = appeal.Restriction.StartsAt.UTC().Round(0)
	if sourceExpiresAt.Valid {
		value := sourceExpiresAt.Time.UTC().Round(0)
		appeal.Restriction.ExpiresAt = &value
	}
	if resolvedAt.Valid {
		value := resolvedAt.Time.UTC().Round(0)
		appeal.ResolvedAt = &value
	}
	switch appeal.Restriction.SourceKind {
	case AccountAccessSourceDisabledAccount:
		appeal.SourceActive = userStatus == string(AccountStatusDisabled) && currentUserVersion == appeal.Restriction.SourceVersion
	case AccountAccessSourceTemporaryRestriction:
		appeal.SourceActive = currentRestrictionVersion.Valid && currentRestrictionVersion.Int64 == appeal.Restriction.SourceVersion &&
			currentRestrictionStarts.Valid && !currentRestrictionStarts.Time.After(asOf) &&
			currentRestrictionExpires.Valid && currentRestrictionExpires.Time.After(asOf) &&
			!currentRestrictionRevoked.Valid
	case AccountAccessSourceManualDownload:
		appeal.SourceActive = currentDownloadRestricted.Valid && currentDownloadRestricted.Bool &&
			currentAccessVersion.Valid && currentAccessVersion.Int64 == appeal.Restriction.SourceVersion
	default:
		return AccountAccessAppeal{}, errors.New("account access appeal contains an invalid source kind")
	}
	if appeal.Status == AccountAccessAppealPending && !appeal.SourceActive {
		appeal.Status = AccountAccessAppealSourceResolved
	}
	if appeal.ID == uuid.Nil || appeal.UserID == uuid.Nil || appeal.UserNumericID < 1 || strings.TrimSpace(appeal.Username) == "" ||
		appeal.Restriction.SourceVersion < 1 || strings.TrimSpace(appeal.Restriction.ReasonCode) == "" || strings.TrimSpace(appeal.Restriction.ReasonSummary) == "" ||
		!validAccountAccessAppealStatus(appeal.Status) {
		return AccountAccessAppeal{}, errors.New("account access appeal projection is invalid")
	}
	return appeal, nil
}

func validAccountAccessAppealStatus(status AccountAccessAppealStatus) bool {
	switch status {
	case AccountAccessAppealPending, AccountAccessAppealApproved, AccountAccessAppealRejected, AccountAccessAppealSourceResolved:
		return true
	default:
		return false
	}
}

func accountAccessAppealActiveSQL(asOf string) string {
	return `(appeal.source_kind = 'disabled_account'
        AND users.status = 'disabled'
        AND users.administration_version = appeal.source_version)
    OR
	    (appeal.source_kind = 'temporary_restriction'
        AND restriction.version = appeal.source_version
        AND restriction.revoked_at IS NULL
        AND restriction.starts_at <= ` + asOf + `
	        AND restriction.expires_at > ` + asOf + `)
	    OR
	    (appeal.source_kind = 'manual_download_restriction'
	        AND COALESCE(access.download_restricted, false)
	        AND access.version = appeal.source_version)`
}

func accountAccessAppealFilterSQL(filter AccountAccessAppealFilter) string {
	active := accountAccessAppealActiveSQL("$1")
	switch filter {
	case AccountAccessAppealFilterPending:
		return "resolution.id IS NULL AND (" + active + ")"
	case AccountAccessAppealFilterResolved:
		return "(resolution.id IS NOT NULL OR NOT (" + active + "))"
	default:
		return "TRUE"
	}
}

func classifyAccountAccessAppealWrite(operation string, err error) error {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
		return ErrAccountAccessAppealExists
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func utf8RuneCount(value string) int {
	return len([]rune(value))
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}

var _ AccountAccessAppealRepository = (*PostgresRepository)(nil)
var _ DownloadRestrictionAppealRepository = (*PostgresRepository)(nil)
