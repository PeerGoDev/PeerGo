package legacyusers

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/contracts/go/authzcontractv1"
	"github.com/peergo/peergo/contracts/go/schemaversionv1"
	"github.com/peergo/peergo/contracts/go/trackerpasskeyv1"
	"github.com/peergo/peergo/services/privacy-vault/internal/credentials"
)

const (
	ModeValidate = "validate"
	ModeImport   = "import"

	requiredCoreMigrationVersion  = schemaversionv1.Core
	requiredVaultMigrationVersion = schemaversionv1.PrivacyVault
)

var (
	memberMandateNamespace = uuid.MustParse("5d711f1b-f03c-5a89-b1e0-5ba0f3db4154")
	memberGrantNamespace   = uuid.MustParse("c5b95d58-b9a1-54cb-a4ac-afbc32296fe7")
)

type credentialWriter interface {
	Import(context.Context, credentials.LegacyCredentialImport) error
	EnsureEmailAddress(context.Context, uuid.UUID, string, time.Time) error
}

type Config struct {
	Mode             string
	RunID            uuid.UUID
	SnapshotSHA256   [sha256.Size]byte
	MappingVersion   string
	FingerprintKey   []byte
	IdentifierKey    []byte
	PasskeyLookupKey []byte
	OccurredAt       time.Time
	ProgressEvery    int
}

type Progress struct {
	Phase     string
	Processed int64
	Imported  int64
	Skipped   int64
}

type Result struct {
	RunID            uuid.UUID
	ExpectedUsers    int64
	ExpectedTorrents int64
	ValidatedUsers   int64
	ImportedUsers    int64
	SkippedUsers     int64
}

type Importer struct {
	source      *pgxpool.Pool
	core        *pgxpool.Pool
	vault       *pgxpool.Pool
	credentials credentialWriter
	config      Config
	progress    func(Progress)
}

func NewImporter(
	source *pgxpool.Pool,
	core *pgxpool.Pool,
	vault *pgxpool.Pool,
	credentialImporter credentialWriter,
	config Config,
	progress func(Progress),
) (*Importer, error) {
	config.OccurredAt = config.OccurredAt.UTC().Truncate(time.Microsecond)
	if source == nil || core == nil || vault == nil || credentialImporter == nil ||
		(config.Mode != ModeValidate && config.Mode != ModeImport) || config.RunID == uuid.Nil ||
		config.MappingVersion == "" || len(config.MappingVersion) > 64 ||
		len(config.FingerprintKey) < sha256.Size || len(config.IdentifierKey) < sha256.Size ||
		len(config.PasskeyLookupKey) < trackerpasskeyv1.LookupKeyMin || config.OccurredAt.IsZero() {
		return nil, errors.New("legacy user importer configuration is invalid")
	}
	if config.ProgressEvery < 1 {
		config.ProgressEvery = 250
	}
	if progress == nil {
		progress = func(Progress) {}
	}
	return &Importer{
		source: source, core: core, vault: vault, credentials: credentialImporter,
		config: config, progress: progress,
	}, nil
}

func (importer *Importer) Run(ctx context.Context) (Result, error) {
	if err := requireMigrationVersion(ctx, importer.core, "Core", requiredCoreMigrationVersion); err != nil {
		return Result{}, err
	}
	if err := requireMigrationVersion(ctx, importer.vault, "Vault", requiredVaultMigrationVersion); err != nil {
		return Result{}, err
	}
	expectedUsers, expectedTorrents, err := importer.sourceCounts(ctx)
	if err != nil {
		return Result{}, err
	}
	if expectedUsers < 1 || expectedTorrents < 1 {
		return Result{}, errors.New("PtYes source snapshot is missing users or torrents")
	}
	if err := importer.ensureRun(ctx, expectedUsers, expectedTorrents); err != nil {
		return Result{}, err
	}
	validated, err := importer.validateSource(ctx, expectedUsers)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		RunID: importer.config.RunID, ExpectedUsers: expectedUsers,
		ExpectedTorrents: expectedTorrents, ValidatedUsers: validated,
	}
	if importer.config.Mode == ModeValidate {
		return result, nil
	}
	if err := importer.beginImport(ctx); err != nil {
		return Result{}, err
	}
	imported, skipped, err := importer.importUsers(ctx)
	if err != nil {
		return Result{}, err
	}
	result.ImportedUsers = imported
	result.SkippedUsers = skipped
	return result, nil
}

func requireMigrationVersion(ctx context.Context, db *pgxpool.Pool, name string, expected int64) error {
	var actual int64
	if err := db.QueryRow(ctx, `
SELECT COALESCE(MAX(version_id), 0)
FROM goose_db_version
WHERE is_applied = true`).Scan(&actual); err != nil {
		return fmt.Errorf("read %s migration version: %w", name, err)
	}
	if actual != expected {
		return fmt.Errorf("%s migration version is %d, want %d", name, actual, expected)
	}
	return nil
}

func (importer *Importer) sourceCounts(ctx context.Context) (int64, int64, error) {
	var users, torrents int64
	if err := importer.source.QueryRow(ctx, `
SELECT
    (SELECT count(*)::bigint FROM users),
    (SELECT count(*)::bigint FROM torrents)`).Scan(&users, &torrents); err != nil {
		return 0, 0, fmt.Errorf("count PtYes source entities: %w", err)
	}
	return users, torrents, nil
}

func (importer *Importer) ensureRun(ctx context.Context, users, torrents int64) error {
	tx, err := importer.core.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration run reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.runs (
    id,
    source_system,
    source_snapshot_sha256,
    mapping_version,
    expected_user_rows,
    expected_torrent_rows,
    created_at,
    state_changed_at
) VALUES ($1, 'ptyes', $2, $3, $4, $5, $6, $6)
ON CONFLICT (source_system, source_snapshot_sha256, mapping_version) DO NOTHING`,
		importer.config.RunID,
		importer.config.SnapshotSHA256[:],
		importer.config.MappingVersion,
		users,
		torrents,
		importer.config.OccurredAt,
	); err != nil {
		return fmt.Errorf("reserve migration run: %w", err)
	}
	var id uuid.UUID
	var expectedUsers, expectedTorrents int64
	var state string
	if err := tx.QueryRow(ctx, `
SELECT id, expected_user_rows, expected_torrent_rows, state
FROM migration.runs
WHERE source_system = 'ptyes'
  AND source_snapshot_sha256 = $1
  AND mapping_version = $2`,
		importer.config.SnapshotSHA256[:], importer.config.MappingVersion,
	).Scan(&id, &expectedUsers, &expectedTorrents, &state); err != nil {
		return fmt.Errorf("read migration run reservation: %w", err)
	}
	if id != importer.config.RunID || expectedUsers != users || expectedTorrents != torrents || state == "failed" {
		return errors.New("migration run conflicts with the requested source snapshot")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration run reservation: %w", err)
	}
	return nil
}

func (importer *Importer) validateSource(ctx context.Context, expectedUsers int64) (int64, error) {
	rows, err := querySourceUsers(ctx, importer.source)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	tx, err := importer.core.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin source validation checkpoint transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var processed int64
	for rows.Next() {
		user, scanErr := scanSourceUser(rows)
		if scanErr != nil {
			return 0, fmt.Errorf("scan PtYes source user %d: %w", processed+1, scanErr)
		}
		processed++
		if user.LegacyID != processed {
			return 0, sourceUserError(user.LegacyID, "non_contiguous_id")
		}
		fingerprint, fingerprintErr := user.fingerprint(importer.config.FingerprintKey)
		if fingerprintErr != nil {
			return 0, sourceUserError(user.LegacyID, "invalid_fields")
		}
		state, version, attempts, checkpointErr := ensureSourceCheckpoint(
			ctx,
			tx,
			importer.config.RunID,
			user.LegacyID,
			fingerprint,
			importer.config.OccurredAt,
		)
		if checkpointErr != nil {
			return 0, checkpointErr
		}
		if state == "discovered" {
			result, updateErr := tx.Exec(ctx, `
UPDATE migration.source_rows
SET state = 'validated',
    attempt_count = $1,
    error_code = NULL,
    version = $2,
    updated_at = $3
WHERE run_id = $4 AND entity_kind = 'user' AND legacy_id = $5
  AND state = 'discovered' AND version = $6`,
				attempts+1,
				version+1,
				importer.config.OccurredAt,
				importer.config.RunID,
				user.LegacyID,
				version,
			)
			if updateErr != nil {
				return 0, fmt.Errorf("validate legacy source checkpoint: %w", updateErr)
			}
			if result.RowsAffected() != 1 {
				return 0, errors.New("legacy source checkpoint changed concurrently")
			}
		} else if state != "validated" && state != "imported" {
			return 0, fmt.Errorf("legacy user %d checkpoint is %s", user.LegacyID, state)
		}
		if processed%int64(importer.config.ProgressEvery) == 0 {
			importer.progress(Progress{Phase: ModeValidate, Processed: processed})
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read PtYes source users: %w", err)
	}
	if processed != expectedUsers {
		return 0, fmt.Errorf("validated %d PtYes users, expected %d", processed, expectedUsers)
	}
	if err := advanceRunToValidated(ctx, tx, importer.config.RunID, importer.config.OccurredAt); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit source validation checkpoints: %w", err)
	}
	importer.progress(Progress{Phase: ModeValidate, Processed: processed})
	return processed, nil
}

func ensureSourceCheckpoint(
	ctx context.Context,
	tx pgx.Tx,
	runID uuid.UUID,
	legacyID int64,
	fingerprint [sha256.Size]byte,
	occurredAt time.Time,
) (string, int64, int32, error) {
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.source_rows (
    run_id,
    entity_kind,
    legacy_id,
    source_fingerprint,
    fingerprint_scheme,
    created_at,
    updated_at
) VALUES ($1, 'user', $2, $3, 'hmac-sha256-v1', $4, $4)
ON CONFLICT (run_id, entity_kind, legacy_id) DO NOTHING`,
		runID, legacyID, fingerprint[:], occurredAt,
	); err != nil {
		return "", 0, 0, fmt.Errorf("checkpoint legacy user %d: %w", legacyID, err)
	}
	var state string
	var version int64
	var attempts int32
	var storedFingerprint []byte
	err := tx.QueryRow(ctx, `
SELECT source_fingerprint, state, version, attempt_count
FROM migration.source_rows
WHERE run_id = $1 AND entity_kind = 'user' AND legacy_id = $2`,
		runID, legacyID,
	).Scan(&storedFingerprint, &state, &version, &attempts)
	if err != nil {
		return "", 0, 0, fmt.Errorf("checkpoint legacy user %d: %w", legacyID, err)
	}
	if subtle.ConstantTimeCompare(storedFingerprint, fingerprint[:]) != 1 {
		return "", 0, 0, fmt.Errorf("legacy user %d source fingerprint changed", legacyID)
	}
	return state, version, attempts, nil
}

func advanceRunToValidated(ctx context.Context, tx pgx.Tx, runID uuid.UUID, occurredAt time.Time) error {
	result, err := tx.Exec(ctx, `
UPDATE migration.runs
SET state = 'validated', version = version + 1, state_changed_at = $1
WHERE id = $2 AND state = 'planned'`, occurredAt, runID)
	if err != nil {
		return fmt.Errorf("advance migration run to validated: %w", err)
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM migration.runs WHERE id = $1`, runID).Scan(&state); err != nil {
		return fmt.Errorf("read migration run after validation: %w", err)
	}
	if state != "validated" && state != "importing" && state != "imported" && state != "reconciled" {
		return fmt.Errorf("migration run cannot validate from state %s", state)
	}
	return nil
}

func (importer *Importer) beginImport(ctx context.Context) error {
	result, err := importer.core.Exec(ctx, `
UPDATE migration.runs
SET state = 'importing', version = version + 1, state_changed_at = $1
WHERE id = $2 AND state = 'validated'`, importer.config.OccurredAt, importer.config.RunID)
	if err != nil {
		return fmt.Errorf("advance migration run to importing: %w", err)
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	var state string
	if err := importer.core.QueryRow(ctx, `SELECT state FROM migration.runs WHERE id = $1`, importer.config.RunID).Scan(&state); err != nil {
		return fmt.Errorf("read migration run before import: %w", err)
	}
	if state != "importing" && state != "imported" && state != "reconciled" {
		return fmt.Errorf("migration run cannot import from state %s", state)
	}
	return nil
}

func (importer *Importer) importUsers(ctx context.Context) (int64, int64, error) {
	rows, err := querySourceUsers(ctx, importer.source)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var processed, imported, skipped int64
	for rows.Next() {
		user, scanErr := scanSourceUser(rows)
		if scanErr != nil {
			return 0, 0, fmt.Errorf("scan PtYes user during import: %w", scanErr)
		}
		processed++
		state, stateErr := importer.sourceCheckpointState(ctx, user.LegacyID)
		if stateErr != nil {
			return 0, 0, stateErr
		}
		if state == "imported" {
			mapping, mapErr := importer.ensureUserMapping(ctx, user.LegacyID)
			if mapErr != nil {
				return 0, 0, mapErr
			}
			if emailErr := importer.credentials.EnsureEmailAddress(
				ctx,
				mapping.CredentialRef,
				user.emailAddress(),
				user.CreatedAt,
			); emailErr != nil {
				return 0, 0, fmt.Errorf("ensure legacy user %d Vault email: %w", user.LegacyID, emailErr)
			}
			skipped++
			continue
		}
		if state != "validated" {
			return 0, 0, fmt.Errorf("legacy user %d is not validated", user.LegacyID)
		}
		mapping, mapErr := importer.ensureUserMapping(ctx, user.LegacyID)
		if mapErr != nil {
			return 0, 0, mapErr
		}
		if err := importer.importVaultCredential(ctx, user, mapping); err != nil {
			return 0, 0, fmt.Errorf("import legacy user %d Vault credential: %w", user.LegacyID, err)
		}
		if err := importer.finalizeCoreUser(ctx, user, mapping); err != nil {
			return 0, 0, fmt.Errorf("finalize legacy user %d Core projection: %w", user.LegacyID, err)
		}
		imported++
		if processed%int64(importer.config.ProgressEvery) == 0 {
			importer.progress(Progress{Phase: ModeImport, Processed: processed, Imported: imported, Skipped: skipped})
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, fmt.Errorf("read PtYes users during import: %w", err)
	}
	importer.progress(Progress{Phase: ModeImport, Processed: processed, Imported: imported, Skipped: skipped})
	return imported, skipped, nil
}

func querySourceUsers(ctx context.Context, source *pgxpool.Pool) (pgx.Rows, error) {
	rows, err := source.Query(ctx, `
SELECT
    id::bigint,
    username,
    nickname,
    avatar,
    email,
    password,
    passkey,
    email_verified,
    banned,
    totp_enabled,
    created_at,
    updated_at,
    deleted_at
FROM users
ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes source users: %w", err)
	}
	return rows, nil
}

func scanSourceUser(rows pgx.Rows) (sourceUser, error) {
	var user sourceUser
	if err := rows.Scan(
		&user.LegacyID,
		&user.Username,
		&user.Nickname,
		&user.Avatar,
		&user.Email,
		&user.PasswordHash,
		&user.Passkey,
		&user.EmailVerified,
		&user.Banned,
		&user.TOTPEnabled,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.DeletedAt,
	); err != nil {
		return sourceUser{}, err
	}
	user.CreatedAt = user.CreatedAt.UTC().Truncate(time.Microsecond)
	user.UpdatedAt = user.UpdatedAt.UTC().Truncate(time.Microsecond)
	if user.DeletedAt != nil {
		value := user.DeletedAt.UTC().Truncate(time.Microsecond)
		user.DeletedAt = &value
	}
	return user, nil
}

func (importer *Importer) sourceCheckpointState(ctx context.Context, legacyID int64) (string, error) {
	var state string
	if err := importer.core.QueryRow(ctx, `
SELECT state
FROM migration.source_rows
WHERE run_id = $1 AND entity_kind = 'user' AND legacy_id = $2`,
		importer.config.RunID, legacyID,
	).Scan(&state); err != nil {
		return "", fmt.Errorf("read legacy user %d checkpoint: %w", legacyID, err)
	}
	return state, nil
}

type userMapping struct {
	UserID        uuid.UUID
	CredentialRef uuid.UUID
}

func (importer *Importer) ensureUserMapping(ctx context.Context, legacyID int64) (userMapping, error) {
	tx, err := importer.core.Begin(ctx)
	if err != nil {
		return userMapping{}, fmt.Errorf("begin legacy user ID allocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.user_id_map (
    source_system,
    legacy_user_id,
    user_id,
    credential_ref,
    first_run_id,
    created_at
) VALUES ('ptyes', $1, $2, $3, $4, $5)
ON CONFLICT (source_system, legacy_user_id) DO NOTHING`,
		legacyID,
		uuid.New(),
		uuid.New(),
		importer.config.RunID,
		importer.config.OccurredAt,
	); err != nil {
		return userMapping{}, fmt.Errorf("allocate legacy user %d IDs: %w", legacyID, err)
	}
	var mapping userMapping
	if err := tx.QueryRow(ctx, `
SELECT user_id, credential_ref
FROM migration.user_id_map
WHERE source_system = 'ptyes' AND legacy_user_id = $1`, legacyID).Scan(
		&mapping.UserID, &mapping.CredentialRef,
	); err != nil {
		return userMapping{}, fmt.Errorf("read legacy user %d ID allocation: %w", legacyID, err)
	}
	if mapping.UserID == uuid.Nil || mapping.CredentialRef == uuid.Nil {
		return userMapping{}, errors.New("legacy user ID allocation is invalid")
	}
	if err := tx.Commit(ctx); err != nil {
		return userMapping{}, fmt.Errorf("commit legacy user ID allocation: %w", err)
	}
	return mapping, nil
}

func (importer *Importer) importVaultCredential(ctx context.Context, user sourceUser, mapping userMapping) error {
	usernameLookup, err := credentials.LookupHMAC(importer.config.IdentifierKey, user.username())
	if err != nil {
		return credentials.ErrLegacyCredentialInput
	}
	emailLookup, err := credentials.LookupHMAC(importer.config.IdentifierKey, user.emailAddress())
	if err != nil {
		return credentials.ErrLegacyCredentialInput
	}
	profile, err := user.passkeyProfile()
	if err != nil {
		return credentials.ErrLegacyCredentialInput
	}
	var verifiedAt *time.Time
	if user.EmailVerified {
		value := user.CreatedAt
		verifiedAt = &value
	}
	var disabledAt *time.Time
	if user.Banned || user.DeletedAt != nil {
		value := importer.config.OccurredAt
		disabledAt = &value
	}
	return importer.credentials.Import(ctx, credentials.LegacyCredentialImport{
		CredentialRef:     mapping.CredentialRef,
		PasswordHash:      user.PasswordHash,
		UsernameLookup:    usernameLookup,
		UsernameMasked:    credentials.MaskUsername(user.username()),
		EmailLookup:       emailLookup,
		EmailMasked:       credentials.MaskEmail(user.emailAddress()),
		EmailAddress:      user.emailAddress(),
		EmailVerifiedAt:   verifiedAt,
		Passkey:           user.Passkey,
		PasskeyProfile:    profile,
		DisabledAt:        disabledAt,
		PasswordUpdatedAt: user.CreatedAt,
		CreatedAt:         user.CreatedAt,
		ImportedAt:        importer.config.OccurredAt,
	})
}

func (importer *Importer) finalizeCoreUser(ctx context.Context, user sourceUser, mapping userMapping) error {
	profile, err := user.passkeyProfile()
	if err != nil {
		return err
	}
	passkeyLookup, err := trackerpasskeyv1.LookupHMACForProfile(
		importer.config.PasskeyLookupKey,
		profile,
		user.Passkey,
	)
	if err != nil {
		return err
	}
	status := "active"
	if user.Banned || user.DeletedAt != nil {
		status = "disabled"
	}
	var emailVerifiedAt *time.Time
	if user.EmailVerified {
		value := user.CreatedAt
		emailVerifiedAt = &value
	}
	tx, err := importer.core.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin Core legacy user projection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.users (
    id,
    credential_ref,
    username,
    display_name,
    status,
    created_at,
    updated_at,
    email_verified_at,
    password_changed_at,
    two_factor_reenrollment_required,
    numeric_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $6, $9, $10)
ON CONFLICT (id) DO NOTHING`,
		mapping.UserID,
		mapping.CredentialRef,
		user.username(),
		user.displayName(),
		status,
		user.CreatedAt,
		importer.config.OccurredAt,
		emailVerifiedAt,
		user.TOTPEnabled,
		user.LegacyID,
	); err != nil {
		return fmt.Errorf("insert Core legacy user: %w", err)
	}
	// Explicit PtYes IDs do not advance a PostgreSQL sequence automatically.
	// Keep the allocator above every imported ID so the first native PeerGo
	// registration cannot collide after cutover.
	if _, err := tx.Exec(ctx, `
SELECT setval(
    'identity.user_numeric_id_seq',
    GREATEST((SELECT last_value FROM identity.user_numeric_id_seq), $1),
    true
)`, user.LegacyID); err != nil {
		return fmt.Errorf("advance Core user numeric ID sequence: %w", err)
	}
	if err := verifyCoreUser(ctx, tx, user, mapping, status, emailVerifiedAt); err != nil {
		return err
	}
	mandateID := uuid.NewSHA1(memberMandateNamespace, mapping.UserID[:])
	grantID := uuid.NewSHA1(memberGrantNamespace, mapping.UserID[:])
	validUntil := importer.config.OccurredAt.AddDate(100, 0, 0)
	if err := ensureMemberMandateAndGrant(
		ctx,
		tx,
		mapping.UserID,
		mandateID,
		grantID,
		user.CreatedAt,
		validUntil,
		importer.config.OccurredAt,
	); err != nil {
		return err
	}
	if err := ensureTrackerProjection(
		ctx,
		tx,
		mapping,
		passkeyLookup,
		user.CreatedAt,
	); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `
UPDATE migration.source_rows
SET state = 'imported',
    attempt_count = attempt_count + 1,
    error_code = NULL,
    version = version + 1,
    updated_at = $1
WHERE run_id = $2 AND entity_kind = 'user' AND legacy_id = $3
  AND state = 'validated'`,
		importer.config.OccurredAt,
		importer.config.RunID,
		user.LegacyID,
	)
	if err != nil {
		return fmt.Errorf("complete legacy user checkpoint: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("legacy user checkpoint changed before Core finalization")
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Core legacy user projection: %w", err)
	}
	return nil
}

func verifyCoreUser(
	ctx context.Context,
	tx pgx.Tx,
	user sourceUser,
	mapping userMapping,
	status string,
	emailVerifiedAt *time.Time,
) error {
	var userID, credentialRef uuid.UUID
	var numericID int64
	var username, displayName, storedStatus string
	var createdAt, passwordChangedAt time.Time
	var storedEmailVerifiedAt *time.Time
	var reenrollment bool
	if err := tx.QueryRow(ctx, `
SELECT
    id,
    numeric_id,
    credential_ref,
    username,
    display_name,
    status,
    created_at,
    email_verified_at,
    password_changed_at,
    two_factor_reenrollment_required
FROM identity.users
WHERE id = $1
FOR UPDATE`, mapping.UserID).Scan(
		&userID,
		&numericID,
		&credentialRef,
		&username,
		&displayName,
		&storedStatus,
		&createdAt,
		&storedEmailVerifiedAt,
		&passwordChangedAt,
		&reenrollment,
	); err != nil {
		return fmt.Errorf("read Core legacy user after insert: %w", err)
	}
	if userID != mapping.UserID || numericID != user.LegacyID || credentialRef != mapping.CredentialRef ||
		username != user.username() || displayName != user.displayName() || storedStatus != status ||
		!createdAt.Equal(user.CreatedAt) || !passwordChangedAt.Equal(user.CreatedAt) ||
		!optionalTimeEqual(storedEmailVerifiedAt, emailVerifiedAt) || reenrollment != user.TOTPEnabled {
		return errors.New("Core legacy user conflicts with stable migration mapping")
	}
	return nil
}

func ensureMemberMandateAndGrant(
	ctx context.Context,
	tx pgx.Tx,
	userID, mandateID, grantID uuid.UUID,
	validFrom, validUntil, createdAt time.Time,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO governance.mandates (
    id,
    subject_id,
    source_type,
    source_reference,
    scope_type,
    scope_id,
    starts_at,
    ends_at,
    status,
    created_at,
    updated_at
) VALUES ($1, $2, 'legacy_import', 'ptyes-user-migration-v1', $3, $4, $5, $6, 'active', $7, $7)
ON CONFLICT (id) DO NOTHING`,
		mandateID, userID, authzcontractv1.SiteScopeType, authzcontractv1.SiteScopeID,
		validFrom, validUntil, createdAt,
	); err != nil {
		return fmt.Errorf("insert legacy member mandate: %w", err)
	}
	var storedSubject uuid.UUID
	var sourceType, scopeType, scopeID, status string
	var startsAt, endsAt time.Time
	if err := tx.QueryRow(ctx, `
SELECT subject_id, source_type, scope_type, scope_id, starts_at, ends_at, status
FROM governance.mandates
WHERE id = $1`, mandateID).Scan(
		&storedSubject, &sourceType, &scopeType, &scopeID, &startsAt, &endsAt, &status,
	); err != nil {
		return fmt.Errorf("read legacy member mandate: %w", err)
	}
	if storedSubject != userID || sourceType != "legacy_import" || scopeType != authzcontractv1.SiteScopeType ||
		scopeID != authzcontractv1.SiteScopeID || !startsAt.Equal(validFrom) || !endsAt.Equal(validUntil) || status != "active" {
		return errors.New("legacy member mandate conflicts with stable user mapping")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO authz.grants (
    id,
    subject_id,
    role_id,
    mandate_id,
    scope_type,
    scope_id,
    valid_from,
    valid_until,
    constraints,
    created_at,
    updated_at
) VALUES ($1, $2, 'member', $3, $4, $5, $6, $7, '{}'::jsonb, $8, $8)
ON CONFLICT (id) DO NOTHING`,
		grantID, userID, mandateID, authzcontractv1.SiteScopeType, authzcontractv1.SiteScopeID,
		validFrom, validUntil, createdAt,
	); err != nil {
		return fmt.Errorf("insert legacy member grant: %w", err)
	}
	var grantSubject, storedMandate uuid.UUID
	var roleID, grantScopeType, grantScopeID string
	var grantValidFrom, grantValidUntil time.Time
	var version int64
	var revokedAt *time.Time
	if err := tx.QueryRow(ctx, `
SELECT subject_id, role_id, mandate_id, scope_type, scope_id,
       valid_from, valid_until, version, revoked_at
FROM authz.grants
WHERE id = $1`, grantID).Scan(
		&grantSubject, &roleID, &storedMandate, &grantScopeType, &grantScopeID,
		&grantValidFrom, &grantValidUntil, &version, &revokedAt,
	); err != nil {
		return fmt.Errorf("read legacy member grant: %w", err)
	}
	if grantSubject != userID || roleID != "member" || storedMandate != mandateID ||
		grantScopeType != authzcontractv1.SiteScopeType || grantScopeID != authzcontractv1.SiteScopeID ||
		!grantValidFrom.Equal(validFrom) || !grantValidUntil.Equal(validUntil) ||
		(version != 1 && version != 2) || revokedAt != nil {
		return errors.New("legacy member grant conflicts with stable user mapping")
	}
	return nil
}

func ensureTrackerProjection(
	ctx context.Context,
	tx pgx.Tx,
	mapping userMapping,
	lookup [sha256.Size]byte,
	createdAt time.Time,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.tracker_passkey_hmac (
    user_id,
    credential_ref,
    lookup_hmac,
    vault_version,
    created_at,
    updated_at
) VALUES ($1, $2, $3, 1, $4, $4)
ON CONFLICT (user_id) DO NOTHING`,
		mapping.UserID, mapping.CredentialRef, lookup[:], createdAt,
	); err != nil {
		return fmt.Errorf("insert legacy Tracker credential projection: %w", err)
	}
	var userID, credentialRef uuid.UUID
	var storedLookup []byte
	var version int64
	var storedCreatedAt time.Time
	if err := tx.QueryRow(ctx, `
SELECT user_id, credential_ref, lookup_hmac, vault_version, created_at
FROM identity.tracker_passkey_hmac
WHERE user_id = $1`, mapping.UserID).Scan(
		&userID, &credentialRef, &storedLookup, &version, &storedCreatedAt,
	); err != nil {
		return fmt.Errorf("read legacy Tracker credential projection: %w", err)
	}
	if userID != mapping.UserID || credentialRef != mapping.CredentialRef ||
		subtle.ConstantTimeCompare(storedLookup, lookup[:]) != 1 || version != 1 ||
		!storedCreatedAt.Equal(createdAt) {
		return errors.New("legacy Tracker projection conflicts with Vault credential")
	}
	return nil
}

func optionalTimeEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
