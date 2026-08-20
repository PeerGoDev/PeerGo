package credentials

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/peergo/peergo/contracts/go/trackerpasskeyv1"
)

var (
	ErrLegacyCredentialInput    = errors.New("legacy credential input is invalid")
	ErrLegacyCredentialConflict = errors.New("legacy credential conflicts with persisted Vault state")
)

// LegacyCredentialImport is the only P0 migration DTO. It intentionally lives
// inside Privacy Vault instead of a shared migration package: Core and generic
// tools must never gain helpers that encrypt or persist password/passkey data.
type LegacyCredentialImport struct {
	CredentialRef     uuid.UUID
	PasswordHash      string
	UsernameLookup    []byte
	UsernameMasked    string
	EmailLookup       []byte
	EmailMasked       string
	EmailAddress      string
	EmailVerifiedAt   *time.Time
	Passkey           string
	PasskeyProfile    string
	DisabledAt        *time.Time
	PasswordUpdatedAt time.Time
	CreatedAt         time.Time
	ImportedAt        time.Time
}

// LegacyCredentialImporter atomically creates one credential, its two direct
// identifiers, and its stable Tracker passkey. Retrying the exact source row is
// a no-op; any fork in credential material fails closed.
type LegacyCredentialImporter struct {
	db        vaultDB
	protector *SecretProtector
	lookupKey []byte
}

func NewLegacyCredentialImporter(
	db vaultDB,
	protector *SecretProtector,
	lookupKey []byte,
) (*LegacyCredentialImporter, error) {
	if db == nil || protector == nil || len(lookupKey) < trackerpasskeyv1.LookupKeyMin {
		return nil, ErrLegacyCredentialInput
	}
	return &LegacyCredentialImporter{
		db:        db,
		protector: protector,
		lookupKey: append([]byte(nil), lookupKey...),
	}, nil
}

func (importer *LegacyCredentialImporter) Import(ctx context.Context, record LegacyCredentialImport) error {
	record = normalizeLegacyCredentialImport(record)
	if !validLegacyCredentialImport(record) {
		return ErrLegacyCredentialInput
	}
	lookup, err := trackerpasskeyv1.LookupHMACForProfile(
		importer.lookupKey,
		record.PasskeyProfile,
		record.Passkey,
	)
	if err != nil {
		return ErrLegacyCredentialInput
	}
	protected, err := importer.protector.Protect(
		trackerPasskeyRecordKind,
		record.CredentialRef,
		record.CredentialRef,
		[]byte(record.Passkey),
	)
	if err != nil {
		return fmt.Errorf("protect legacy Tracker passkey: %w", err)
	}

	tx, err := importer.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin legacy credential import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := importLegacyCredentialRow(ctx, tx, record); err != nil {
		return err
	}
	for _, identifier := range []legacyIdentifierImport{
		{Kind: "username", Lookup: record.UsernameLookup, Masked: record.UsernameMasked, VerifiedAt: &record.CreatedAt},
		{Kind: "email", Lookup: record.EmailLookup, Masked: record.EmailMasked, VerifiedAt: record.EmailVerifiedAt},
	} {
		if err := importLegacyIdentifierRow(ctx, tx, record.CredentialRef, record.CreatedAt, identifier); err != nil {
			return err
		}
	}
	if err := ensureEmailAddressRow(
		ctx,
		tx,
		record.CredentialRef,
		record.EmailAddress,
		record.CreatedAt,
	); err != nil {
		return fmt.Errorf("import legacy email address: %w", err)
	}
	if err := importer.importLegacyTrackerPasskeyRow(ctx, tx, record, protected, lookup); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit legacy credential import: %w", err)
	}
	return nil
}

func normalizeLegacyCredentialImport(record LegacyCredentialImport) LegacyCredentialImport {
	record.CreatedAt = record.CreatedAt.UTC().Truncate(time.Microsecond)
	record.ImportedAt = record.ImportedAt.UTC().Truncate(time.Microsecond)
	record.PasswordUpdatedAt = record.PasswordUpdatedAt.UTC().Truncate(time.Microsecond)
	if record.EmailVerifiedAt != nil {
		value := record.EmailVerifiedAt.UTC().Truncate(time.Microsecond)
		record.EmailVerifiedAt = &value
	}
	if record.DisabledAt != nil {
		value := record.DisabledAt.UTC().Truncate(time.Microsecond)
		record.DisabledAt = &value
	}
	return record
}

func validLegacyCredentialImport(record LegacyCredentialImport) bool {
	return record.CredentialRef != uuid.Nil &&
		ValidateLegacyPtYesPasswordHash(record.PasswordHash) == nil &&
		len(record.UsernameLookup) == sha256.Size && len(record.EmailLookup) == sha256.Size &&
		validLegacyMaskedValue(record.UsernameMasked) && validLegacyMaskedValue(record.EmailMasked) &&
		validNormalizedEmail(record.EmailAddress) &&
		trackerpasskeyv1.ValidateForProfile(record.PasskeyProfile, record.Passkey) == nil &&
		!record.CreatedAt.IsZero() && !record.PasswordUpdatedAt.Before(record.CreatedAt) &&
		!record.ImportedAt.Before(record.CreatedAt) &&
		(record.EmailVerifiedAt == nil || !record.EmailVerifiedAt.Before(record.CreatedAt)) &&
		(record.DisabledAt == nil || !record.DisabledAt.Before(record.CreatedAt))
}

func validNormalizedEmail(value string) bool {
	normalized, err := normalizeEmailAddress(value)
	return err == nil && normalized == value
}

// EnsureEmailAddress lets an already-terminal legacy account checkpoint gain
// the newly required Vault-only email projection without replaying credentials.
func (importer *LegacyCredentialImporter) EnsureEmailAddress(
	ctx context.Context,
	credentialRef uuid.UUID,
	emailAddress string,
	createdAt time.Time,
) error {
	createdAt = createdAt.UTC().Truncate(time.Microsecond)
	if importer == nil || credentialRef == uuid.Nil || !validNormalizedEmail(emailAddress) || createdAt.IsZero() {
		return ErrLegacyCredentialInput
	}
	tx, err := importer.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin legacy email address import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ensureEmailAddressRow(ctx, tx, credentialRef, emailAddress, createdAt); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit legacy email address import: %w", err)
	}
	return nil
}

func ensureEmailAddressRow(
	ctx context.Context,
	tx pgx.Tx,
	credentialRef uuid.UUID,
	emailAddress string,
	createdAt time.Time,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO vault.email_addresses (
    credential_ref, email_address, created_at, updated_at
) VALUES ($1, $2, $3, $3)
ON CONFLICT (credential_ref) DO NOTHING`, credentialRef, emailAddress, createdAt); err != nil {
		return fmt.Errorf("insert Vault email address: %w", err)
	}
	var stored string
	var storedCreatedAt time.Time
	if err := tx.QueryRow(ctx, `
SELECT email_address, created_at
FROM vault.email_addresses
WHERE credential_ref = $1`, credentialRef).Scan(&stored, &storedCreatedAt); err != nil {
		return fmt.Errorf("read Vault email address after insert: %w", err)
	}
	if !constantTimeStringEqual(stored, emailAddress) || !storedCreatedAt.Equal(createdAt) {
		return ErrLegacyCredentialConflict
	}
	return nil
}

func validLegacyMaskedValue(value string) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && length >= 1 && length <= 254
}

func importLegacyCredentialRow(ctx context.Context, tx pgx.Tx, record LegacyCredentialImport) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO vault.credentials (
    credential_ref,
    password_hash,
    password_algorithm,
    password_updated_at,
    disabled_at,
    created_at,
    updated_at
) VALUES ($1, $2, 'bcrypt_ptyes_cost10', $3, $4, $5, $6)
ON CONFLICT (credential_ref) DO NOTHING`,
		record.CredentialRef,
		record.PasswordHash,
		record.PasswordUpdatedAt,
		record.DisabledAt,
		record.CreatedAt,
		record.ImportedAt,
	); err != nil {
		return fmt.Errorf("insert legacy credential: %w", err)
	}

	var passwordHash, algorithm string
	var passwordUpdatedAt, createdAt time.Time
	var disabledAt *time.Time
	if err := tx.QueryRow(ctx, `
SELECT password_hash, password_algorithm, password_updated_at, disabled_at, created_at
FROM vault.credentials
WHERE credential_ref = $1
FOR UPDATE`, record.CredentialRef).Scan(
		&passwordHash,
		&algorithm,
		&passwordUpdatedAt,
		&disabledAt,
		&createdAt,
	); err != nil {
		return fmt.Errorf("read legacy credential after insert: %w", err)
	}
	if !constantTimeStringEqual(passwordHash, record.PasswordHash) ||
		algorithm != "bcrypt_ptyes_cost10" ||
		!passwordUpdatedAt.Equal(record.PasswordUpdatedAt) ||
		!createdAt.Equal(record.CreatedAt) ||
		!optionalTimeEqual(disabledAt, record.DisabledAt) {
		return ErrLegacyCredentialConflict
	}
	return nil
}

type legacyIdentifierImport struct {
	Kind       string
	Lookup     []byte
	Masked     string
	VerifiedAt *time.Time
}

func importLegacyIdentifierRow(
	ctx context.Context,
	tx pgx.Tx,
	credentialRef uuid.UUID,
	createdAt time.Time,
	record legacyIdentifierImport,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO vault.direct_identifiers (
    credential_ref, kind, lookup_hmac, masked_value, verified_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (credential_ref, kind) DO NOTHING`,
		credentialRef,
		record.Kind,
		record.Lookup,
		record.Masked,
		record.VerifiedAt,
		createdAt,
	); err != nil {
		return fmt.Errorf("insert legacy %s identifier: %w", record.Kind, err)
	}

	var lookup []byte
	var masked string
	var verifiedAt *time.Time
	var storedCreatedAt time.Time
	if err := tx.QueryRow(ctx, `
SELECT lookup_hmac, masked_value, verified_at, created_at
FROM vault.direct_identifiers
WHERE credential_ref = $1 AND kind = $2`, credentialRef, record.Kind).Scan(
		&lookup,
		&masked,
		&verifiedAt,
		&storedCreatedAt,
	); err != nil {
		return fmt.Errorf("read legacy %s identifier after insert: %w", record.Kind, err)
	}
	if subtle.ConstantTimeCompare(lookup, record.Lookup) != 1 || masked != record.Masked ||
		!optionalTimeEqual(verifiedAt, record.VerifiedAt) || !storedCreatedAt.Equal(createdAt) {
		return ErrLegacyCredentialConflict
	}
	return nil
}

func (importer *LegacyCredentialImporter) importLegacyTrackerPasskeyRow(
	ctx context.Context,
	tx pgx.Tx,
	record LegacyCredentialImport,
	protected ProtectedSecret,
	lookup [sha256.Size]byte,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO vault.tracker_passkeys (
    credential_ref,
    ciphertext,
    nonce,
    encryption_key_epoch,
    lookup_hmac,
    format_profile,
    version,
    created_at,
    updated_at
) VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $7)
ON CONFLICT (credential_ref) DO NOTHING`,
		record.CredentialRef,
		protected.Ciphertext,
		protected.Nonce,
		protected.KeyEpoch,
		lookup[:],
		record.PasskeyProfile,
		record.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert legacy Tracker passkey: %w", err)
	}

	var stored ProtectedSecret
	var storedLookup []byte
	var profile string
	var version int64
	var createdAt time.Time
	if err := tx.QueryRow(ctx, `
SELECT
    ciphertext,
    nonce,
    encryption_key_epoch,
    lookup_hmac,
    format_profile,
    version,
    created_at
FROM vault.tracker_passkeys
WHERE credential_ref = $1`, record.CredentialRef).Scan(
		&stored.Ciphertext,
		&stored.Nonce,
		&stored.KeyEpoch,
		&storedLookup,
		&profile,
		&version,
		&createdAt,
	); err != nil {
		return fmt.Errorf("read legacy Tracker passkey after insert: %w", err)
	}
	plaintext, err := importer.protector.Unprotect(
		trackerPasskeyRecordKind,
		record.CredentialRef,
		record.CredentialRef,
		stored,
	)
	if err != nil {
		return fmt.Errorf("verify legacy Tracker passkey envelope: %w", err)
	}
	defer clear(plaintext)
	if subtle.ConstantTimeCompare(plaintext, []byte(record.Passkey)) != 1 ||
		subtle.ConstantTimeCompare(storedLookup, lookup[:]) != 1 ||
		profile != record.PasskeyProfile || version != 1 || !createdAt.Equal(record.CreatedAt) {
		return ErrLegacyCredentialConflict
	}
	return nil
}

func constantTimeStringEqual(left, right string) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func optionalTimeEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
