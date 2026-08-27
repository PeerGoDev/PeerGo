package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
	"github.com/peergo/peergo/services/core/internal/generated/identitydb"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

// PostgresRepository persists Core-owned user projections and token digests.
type PostgresRepository struct {
	db      postgresDB
	queries *identitydb.Queries
}

type postgresDB interface {
	identitydb.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

// NewPostgresRepository creates the production identity persistence adapter.
func NewPostgresRepository(db postgresDB) *PostgresRepository {
	return &PostgresRepository{db: db, queries: identitydb.New(db)}
}

// UserByCredentialRef implements Repository. The database checks account
// access restrictions in the same read that resolves the verified credential,
// so a restriction cannot race between an application-side preflight and login.
func (r *PostgresRepository) UserByCredentialRef(ctx context.Context, credentialRef uuid.UUID, asOf time.Time) (User, error) {
	row, err := r.queries.GetUserByCredentialRef(ctx, identitydb.GetUserByCredentialRefParams{
		CredentialRef: credentialRef,
		AsOf:          timestamp(asOf),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, fmt.Errorf("get user by credential reference: %w", err)
	}
	if row.Status != "active" {
		return User{}, ErrInvalidCredentials
	}

	return User{
		ID:              row.ID,
		CredentialRef:   row.CredentialRef,
		Username:        row.Username,
		DisplayName:     row.DisplayName,
		EmailVerifiedAt: nullableTimestamp(row.EmailVerifiedAt),
	}, nil
}

// WebSessionPolicy implements Repository. Durations are deliberately fetched
// for each successful credential verification so an administrator edit takes
// effect without restarting Core; already-issued sessions are never extended.
func (r *PostgresRepository) WebSessionPolicy(ctx context.Context) (WebSessionPolicy, error) {
	row, err := r.queries.GetWebSessionPolicy(ctx)
	if err != nil {
		return WebSessionPolicy{}, fmt.Errorf("get web session policy: %w", err)
	}
	policy := WebSessionPolicy{
		SessionDuration:         time.Duration(row.SessionValidHours) * time.Hour,
		RememberSessionDuration: time.Duration(row.RememberSessionValidHours) * time.Hour,
	}
	if !validWebSessionPolicy(policy) {
		return WebSessionPolicy{}, errors.New("web session policy contains invalid durations")
	}
	return policy, nil
}

// PublicProfileByUsername uses two bounded reads: one aggregate profile row and
// at most ten recent public, non-anonymous publications.
// Anonymous torrents are intentionally excluded: their uploader association
// is operational metadata and must not be re-identified by a member page.
func (r *PostgresRepository) PublicProfileByUsername(ctx context.Context, username string, asOf time.Time) (PublicUserProfile, error) {
	row, err := r.queries.GetPublicUserProfileByUsername(ctx, identitydb.GetPublicUserProfileByUsernameParams{
		Username: username,
		AsOf:     timestamp(asOf),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicUserProfile{}, ErrPublicUserNotFound
	}
	if err != nil {
		return PublicUserProfile{}, fmt.Errorf("get public user profile: %w", err)
	}
	if !row.JoinedAt.Valid {
		return PublicUserProfile{}, errors.New("public user profile contains an invalid join timestamp")
	}
	publishedRows, err := r.queries.ListPublicUserPublishedTorrents(ctx, identitydb.ListPublicUserPublishedTorrentsParams{
		UserID: row.ID, ResultLimit: 10,
	})
	if err != nil {
		return PublicUserProfile{}, fmt.Errorf("list public user published torrents: %w", err)
	}
	published := make([]PublicUserPublishedTorrent, 0, len(publishedRows))
	for _, item := range publishedRows {
		if item.ID < 1 || strings.TrimSpace(item.Title) == "" || strings.TrimSpace(item.CategoryID) == "" ||
			strings.TrimSpace(item.CategoryName) == "" || item.TotalSizeBytes < 1 || !item.PublishedAt.Valid {
			return PublicUserProfile{}, errors.New("public user published torrent projection is invalid")
		}
		published = append(published, PublicUserPublishedTorrent{
			ID: item.ID, Title: item.Title, Subtitle: item.Subtitle,
			CategoryID: item.CategoryID, CategoryName: item.CategoryName,
			TotalSizeBytes: item.TotalSizeBytes, PublishedAt: item.PublishedAt.Time.UTC(),
		})
	}
	return PublicUserProfile{
		NumericID:             row.NumericID,
		Username:              row.Username,
		DisplayName:           row.DisplayName,
		JoinedAt:              row.JoinedAt.Time.UTC(),
		PublishedTorrentCount: row.PublishedTorrentCount,
		PublishedTorrents:     published,
	}, nil
}

// UpdateMyDisplayName implements AccountProfileRepository. The active account
// and access-restriction checks are repeated in the write statement so a
// restriction taking effect after session authentication still fails closed.
func (r *PostgresRepository) UpdateMyDisplayName(ctx context.Context, userID uuid.UUID, displayName string, updatedAt time.Time) (User, error) {
	row, err := r.queries.UpdateMyDisplayName(ctx, identitydb.UpdateMyDisplayNameParams{
		UserID: userID, DisplayName: displayName, UpdatedAt: timestamp(updatedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrSessionNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("update own display name: %w", err)
	}
	return User{
		ID: row.ID, CredentialRef: row.CredentialRef, Username: row.Username,
		DisplayName: row.DisplayName, EmailVerifiedAt: nullableTimestamp(row.EmailVerifiedAt),
	}, nil
}

// SaveUserAvatar commits the immutable logical object, its verified physical
// location and the user's current pointer in one PostgreSQL transaction. The
// object bytes were already written and fully read back by AvatarService;
// failures here may leave a harmless content-addressed orphan, never a partial
// current avatar.
func (r *PostgresRepository) SaveUserAvatar(ctx context.Context, avatar StoredAvatar) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin avatar transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := r.queries.WithTx(tx)
	objectID, err := queries.ResolveUserAvatarObject(ctx, identitydb.ResolveUserAvatarObjectParams{
		ObjectID: avatar.ObjectID, ContentSha256: avatar.Descriptor.SHA256[:], ByteLength: avatar.Descriptor.ByteLength,
		ContentType: avatar.ContentType, Extension: avatar.Extension, Width: avatar.Width, Height: avatar.Height,
		CreatedAt: timestamp(avatar.UpdatedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAvatarConflict
	}
	if err != nil {
		return fmt.Errorf("resolve avatar object: %w", err)
	}
	versionID := pgtype.Text{String: avatar.VersionID, Valid: avatar.VersionID != ""}
	rows, err := queries.InsertUserAvatarLocation(ctx, identitydb.InsertUserAvatarLocationParams{
		ObjectID: objectID, BackendID: string(avatar.BackendID), ObjectKey: string(avatar.ObjectKey), VersionID: versionID,
		ObservedByteLength: avatar.Descriptor.ByteLength, ObservedSha256: avatar.Descriptor.SHA256[:], VerifiedAt: timestamp(avatar.UpdatedAt),
	})
	if err != nil {
		return fmt.Errorf("insert avatar object location: %w", err)
	}
	if rows != 1 {
		return ErrAvatarConflict
	}
	rows, err = queries.SetCurrentUserAvatar(ctx, identitydb.SetCurrentUserAvatarParams{
		ObjectID: objectID, UpdatedAt: timestamp(avatar.UpdatedAt), UserID: avatar.UserID,
	})
	if err != nil {
		return fmt.Errorf("set current user avatar: %w", err)
	}
	if rows != 1 {
		return ErrSessionNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit avatar transaction: %w", err)
	}
	return nil
}

func (r *PostgresRepository) PublicUserAvatar(ctx context.Context, username string, asOf time.Time) (AvatarSource, error) {
	row, err := r.queries.GetPublicUserAvatar(ctx, identitydb.GetPublicUserAvatarParams{Username: username, AsOf: timestamp(asOf)})
	if errors.Is(err, pgx.ErrNoRows) {
		return AvatarSource{}, ErrAvatarNotFound
	}
	if err != nil {
		return AvatarSource{}, fmt.Errorf("get public user avatar: %w", err)
	}
	if len(row.ContentSha256) != 32 || !row.UpdatedAt.Valid || row.ByteLength < 1 || row.Width < 1 || row.Height < 1 {
		return AvatarSource{}, ErrAvatarConflict
	}
	backendID, err := objectstorage.ParseBackendID(row.BackendID)
	if err != nil {
		return AvatarSource{}, ErrAvatarConflict
	}
	objectKey, err := objectstorage.ParseKey(row.ObjectKey)
	if err != nil {
		return AvatarSource{}, ErrAvatarConflict
	}
	var digest objectstorage.SHA256
	copy(digest[:], row.ContentSha256)
	return AvatarSource{
		ObjectID:    row.ObjectID,
		Descriptor:  objectstorage.Descriptor{SHA256: digest, ByteLength: row.ByteLength},
		ContentType: row.ContentType, Extension: row.Extension, Width: row.Width, Height: row.Height,
		BackendID: backendID, ObjectKey: objectKey, VersionID: row.VersionID.String, UpdatedAt: row.UpdatedAt.Time.UTC(),
	}, nil
}

func nullableTimestamp(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

// CreateSession implements Repository.
func (r *PostgresRepository) CreateSession(ctx context.Context, session SessionRecord) error {
	return r.queries.CreateWebSession(ctx, identitydb.CreateWebSessionParams{
		TokenHash: session.TokenHash,
		UserID:    session.User.ID,
		CreatedAt: timestamp(session.CreatedAt),
		ExpiresAt: timestamp(session.ExpiresAt),
	})
}

// ActiveSession implements Repository.
func (r *PostgresRepository) ActiveSession(ctx context.Context, tokenHash []byte, asOf time.Time) (SessionRecord, error) {
	row, err := r.queries.GetActiveWebSession(ctx, identitydb.GetActiveWebSessionParams{
		TokenHash: tokenHash,
		AsOf:      timestamp(asOf),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionRecord{}, ErrSessionNotFound
	}
	if err != nil {
		return SessionRecord{}, fmt.Errorf("get active web session: %w", err)
	}
	if !row.CreatedAt.Valid || !row.ExpiresAt.Valid {
		return SessionRecord{}, errors.New("active web session contains an invalid timestamp")
	}
	// last_seen_at is deliberately coalesced in SQL to five-minute buckets. A
	// useful session-management timestamp is retained without creating a
	// request-by-request activity trail.
	if err := r.queries.TouchActiveWebSession(ctx, identitydb.TouchActiveWebSessionParams{
		SeenAt: timestamp(asOf), TokenHash: tokenHash,
	}); err != nil {
		return SessionRecord{}, fmt.Errorf("touch active web session: %w", err)
	}

	return SessionRecord{
		TokenHash: append([]byte(nil), row.TokenHash...),
		User: User{
			ID:              row.UserID,
			CredentialRef:   row.CredentialRef,
			Username:        row.Username,
			DisplayName:     row.DisplayName,
			EmailVerifiedAt: nullableTimestamp(row.EmailVerifiedAt),
		},
		CreatedAt: row.CreatedAt.Time,
		ExpiresAt: row.ExpiresAt.Time,
	}, nil
}

// RevokeSession implements Repository.
func (r *PostgresRepository) RevokeSession(ctx context.Context, tokenHash []byte, revokedAt time.Time) error {
	rows, err := r.queries.RevokeWebSession(ctx, identitydb.RevokeWebSessionParams{
		RevokedAt: timestamp(revokedAt),
		TokenHash: tokenHash,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// ListActiveStaffWebAuthnCredentials implements StaffRepository. The complete
// encrypted record is returned untouched; decryption belongs to StaffService.
func (r *PostgresRepository) ListActiveStaffWebAuthnCredentials(ctx context.Context, userID uuid.UUID) ([]StaffWebAuthnCredential, error) {
	rows, err := r.queries.ListActiveStaffWebAuthnCredentials(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list active staff WebAuthn credentials: %w", err)
	}
	result := make([]StaffWebAuthnCredential, 0, len(rows))
	for _, row := range rows {
		result = append(result, StaffWebAuthnCredential{
			ID:     append([]byte(nil), row.CredentialID...),
			UserID: row.UserID,
			Protected: ProtectedRecord{
				Ciphertext: append([]byte(nil), row.RecordCiphertext...),
				Nonce:      append([]byte(nil), row.RecordNonce...),
				KeyEpoch:   row.KeyEpoch,
			},
		})
	}
	return result, nil
}

// CreateStaffWebAuthnChallenge implements StaffRepository. The SQL statement
// invalidates any older live challenge for the same parent session first.
func (r *PostgresRepository) CreateStaffWebAuthnChallenge(ctx context.Context, challenge StaffWebAuthnChallenge) error {
	params := identitydb.CreateStaffWebAuthnChallengeParams{
		ID:                challenge.ID,
		UserID:            challenge.UserID,
		ParentTokenHash:   challenge.ParentTokenHash,
		SessionCiphertext: challenge.Protected.Ciphertext,
		SessionNonce:      challenge.Protected.Nonce,
		KeyEpoch:          challenge.Protected.KeyEpoch,
		CreatedAt:         timestamp(challenge.CreatedAt),
		ExpiresAt:         timestamp(challenge.ExpiresAt),
	}
	// The partial unique index is the final concurrency guard. Two begins can
	// take the same statement snapshot; the losing insert then retries after the
	// winner commits, consumes that winner, and installs the newest challenge.
	// This keeps the one-live-challenge invariant without exposing an incidental
	// PostgreSQL uniqueness race as a 500 response.
	for attempt := 0; attempt < 3; attempt++ {
		err := r.queries.CreateStaffWebAuthnChallenge(ctx, params)
		if err == nil {
			return nil
		}
		if !isActiveStaffChallengeConflict(err) {
			return fmt.Errorf("create staff WebAuthn challenge: %w", err)
		}
	}
	return errors.New("create staff WebAuthn challenge: concurrent replacement did not settle")
}

// ConsumeStaffWebAuthnChallenge implements StaffRepository. PostgreSQL claims
// the row and returns it in one statement, making concurrent replays lose.
func (r *PostgresRepository) ConsumeStaffWebAuthnChallenge(ctx context.Context, challengeID, userID uuid.UUID, parentTokenHash []byte, consumedAt time.Time) (StaffWebAuthnChallenge, error) {
	row, err := r.queries.ConsumeStaffWebAuthnChallenge(ctx, identitydb.ConsumeStaffWebAuthnChallengeParams{
		ConsumedAt:      timestamp(consumedAt),
		ID:              challengeID,
		UserID:          userID,
		ParentTokenHash: parentTokenHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StaffWebAuthnChallenge{}, ErrStaffChallengeNotFound
	}
	if err != nil {
		return StaffWebAuthnChallenge{}, fmt.Errorf("consume staff WebAuthn challenge: %w", err)
	}
	if !row.CreatedAt.Valid || !row.ExpiresAt.Valid {
		return StaffWebAuthnChallenge{}, errors.New("staff WebAuthn challenge contains an invalid timestamp")
	}
	return StaffWebAuthnChallenge{
		ID:              row.ID,
		UserID:          row.UserID,
		ParentTokenHash: append([]byte(nil), row.ParentTokenHash...),
		Protected: ProtectedRecord{
			Ciphertext: append([]byte(nil), row.SessionCiphertext...),
			Nonce:      append([]byte(nil), row.SessionNonce...),
			KeyEpoch:   row.KeyEpoch,
		},
		CreatedAt: row.CreatedAt.Time.UTC(),
		ExpiresAt: row.ExpiresAt.Time.UTC(),
	}, nil
}

// CreateStaffSession updates the credential counter and creates the new token
// inside one transaction. Locking the parent session closes the race with Web
// logout: logout either wins first, or waits and then revokes the new child.
func (r *PostgresRepository) CreateStaffSession(ctx context.Context, creation StaffSessionCreation) (time.Time, error) {
	if !creation.Authority.IsValid() {
		return time.Time{}, errors.New("staff session creation contains an invalid authority binding")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("begin staff session transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := r.queries.WithTx(tx)

	parentExpiry, err := queries.LockActiveWebSessionForStaff(ctx, identitydb.LockActiveWebSessionForStaffParams{
		ParentTokenHash: creation.ParentTokenHash,
		UserID:          creation.UserID,
		AsOf:            timestamp(creation.CreatedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrStaffSessionNotFound
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("lock parent Web session: %w", err)
	}
	if !parentExpiry.Valid {
		return time.Time{}, errors.New("parent Web session contains an invalid expiry")
	}
	expiresAt := creation.ExpiresAt.UTC()
	if parentExpiry.Time.Before(expiresAt) {
		expiresAt = parentExpiry.Time.UTC()
	}
	if !expiresAt.After(creation.CreatedAt) {
		return time.Time{}, ErrStaffSessionNotFound
	}

	if err := queries.RevokeExistingStaffSessions(ctx, identitydb.RevokeExistingStaffSessionsParams{
		RevokedAt: timestamp(creation.CreatedAt),
		UserID:    creation.UserID,
	}); err != nil {
		return time.Time{}, fmt.Errorf("revoke existing staff sessions: %w", err)
	}
	rows, err := queries.UpdateStaffWebAuthnCredential(ctx, identitydb.UpdateStaffWebAuthnCredentialParams{
		RecordCiphertext: creation.CredentialRecord.Ciphertext,
		RecordNonce:      creation.CredentialRecord.Nonce,
		KeyEpoch:         creation.CredentialRecord.KeyEpoch,
		LastUsedAt:       timestamp(creation.WebAuthnAuthenticatedAt),
		UpdatedAt:        timestamp(creation.CreatedAt),
		CredentialID:     creation.StaffCredentialID,
		UserID:           creation.UserID,
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("update staff WebAuthn credential: %w", err)
	}
	if rows != 1 {
		return time.Time{}, ErrStaffCredentialRequired
	}
	persistedExpiry, err := queries.InsertStaffSession(ctx, identitydb.InsertStaffSessionParams{
		TokenHash:               creation.TokenHash,
		UserID:                  creation.UserID,
		ParentTokenHash:         creation.ParentTokenHash,
		StaffCredentialID:       creation.StaffCredentialID,
		WebauthnAuthenticatedAt: timestamp(creation.WebAuthnAuthenticatedAt),
		AuthorityGrantID:        pgtype.UUID{Bytes: creation.Authority.GrantID, Valid: true},
		AuthorityGrantVersion:   pgtype.Int8{Int64: creation.Authority.GrantVersion, Valid: true},
		AuthorityMandateID:      pgtype.UUID{Bytes: creation.Authority.MandateID, Valid: true},
		CreatedAt:               timestamp(creation.CreatedAt),
		ExpiresAt:               timestamp(expiresAt),
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("insert staff session: %w", err)
	}
	if !persistedExpiry.Valid {
		return time.Time{}, errors.New("staff session insert returned an invalid expiry")
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, fmt.Errorf("commit staff session: %w", err)
	}
	return persistedExpiry.Time.UTC(), nil
}

// ActiveStaffSession implements StaffRepository. Both the parent Web session
// and the staff credential must still be active in the same query.
func (r *PostgresRepository) ActiveStaffSession(ctx context.Context, tokenHash []byte, asOf time.Time) (StaffSessionRecord, error) {
	row, err := r.queries.GetActiveStaffSession(ctx, identitydb.GetActiveStaffSessionParams{
		TokenHash: tokenHash,
		AsOf:      timestamp(asOf),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StaffSessionRecord{}, ErrStaffSessionNotFound
	}
	if err != nil {
		return StaffSessionRecord{}, fmt.Errorf("get active staff session: %w", err)
	}
	if !row.CreatedAt.Valid || !row.ExpiresAt.Valid || !row.WebauthnAuthenticatedAt.Valid ||
		!row.AuthorityGrantID.Valid || !row.AuthorityGrantVersion.Valid || !row.AuthorityMandateID.Valid {
		return StaffSessionRecord{}, errors.New("active staff session contains invalid required fields")
	}
	authority := authz.AuthorityBinding{
		GrantID:      uuid.UUID(row.AuthorityGrantID.Bytes),
		GrantVersion: row.AuthorityGrantVersion.Int64,
		MandateID:    uuid.UUID(row.AuthorityMandateID.Bytes),
	}
	if !authority.IsValid() {
		return StaffSessionRecord{}, errors.New("active staff session contains an invalid authority binding")
	}
	return StaffSessionRecord{
		TokenHash:         append([]byte(nil), row.TokenHash...),
		ParentTokenHash:   append([]byte(nil), row.ParentTokenHash...),
		StaffCredentialID: append([]byte(nil), row.StaffCredentialID...),
		Authority:         authority,
		User: User{
			ID:              row.UserID,
			CredentialRef:   row.CredentialRef,
			Username:        row.Username,
			DisplayName:     row.DisplayName,
			EmailVerifiedAt: nullableTimestamp(row.EmailVerifiedAt),
		},
		CreatedAt:               row.CreatedAt.Time.UTC(),
		ExpiresAt:               row.ExpiresAt.Time.UTC(),
		WebAuthnAuthenticatedAt: row.WebauthnAuthenticatedAt.Time.UTC(),
	}, nil
}

// RevokeStaffSession implements StaffRepository.
func (r *PostgresRepository) RevokeStaffSession(ctx context.Context, tokenHash []byte, revokedAt time.Time) error {
	rows, err := r.queries.RevokeStaffSession(ctx, identitydb.RevokeStaffSessionParams{
		RevokedAt: timestamp(revokedAt),
		TokenHash: tokenHash,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrStaffSessionNotFound
	}
	return nil
}

// ListManagedUsers implements UserAdministrationRepository. SQL selects only
// Core-owned operational state plus the opaque credential reference needed by
// the authorized use case; Vault contact data is enriched after this adapter.
func (r *PostgresRepository) ListManagedUsers(ctx context.Context, query ManagedUserListQuery) (ManagedUserPage, error) {
	total, err := r.queries.CountManagedUsers(ctx, identitydb.CountManagedUsersParams{
		SearchQuery: query.Query, DirectoryFilter: string(query.Filter), AsOf: timestamp(query.AsOf),
	})
	if err != nil {
		return ManagedUserPage{}, fmt.Errorf("count managed users: %w", err)
	}
	summaryRow, err := r.queries.GetManagedUserDirectorySummary(ctx, timestamp(query.AsOf))
	if err != nil {
		return ManagedUserPage{}, fmt.Errorf("summarize managed users: %w", err)
	}
	rows, err := r.queries.ListManagedUsers(ctx, identitydb.ListManagedUsersParams{
		AsOf: timestamp(query.AsOf), SearchQuery: query.Query,
		DirectoryFilter: string(query.Filter), PageOffset: int32(query.Offset), PageSize: int32(query.PageSize),
	})
	if err != nil {
		return ManagedUserPage{}, fmt.Errorf("list managed users: %w", err)
	}
	items := make([]ManagedUserSummary, 0, len(rows))
	for _, row := range rows {
		item, err := managedUserSummaryFromValues(
			row.ID, row.NumericID, row.CredentialRef, row.Username, row.DisplayName, row.Status,
			row.EmailVerified, row.Banned, row.DownloadRestricted,
			row.VipEnabled, row.VipActive, row.VipUntil,
			row.AdministrationVersion, row.ActiveRestrictionCount,
			row.UploadedBytes, row.DownloadedBytes, row.MagicBalance, row.Level,
			row.RoleNames, row.LastActiveAt, row.CreatedAt, row.UpdatedAt,
		)
		if err != nil {
			return ManagedUserPage{}, err
		}
		items = append(items, item)
	}
	return ManagedUserPage{
		Items: items, Total: total, Page: query.Page, PageSize: query.PageSize,
		Summary: ManagedUserDirectorySummary{
			Total: summaryRow.Total, Active: summaryRow.Active, Banned: summaryRow.Banned,
			VIP: summaryRow.Vip, DownloadRestricted: summaryRow.DownloadRestricted,
			Unverified: summaryRow.Unverified,
		},
	}, nil
}

// GetManagedUser implements UserAdministrationRepository. The summary and
// restrictions use the same as-of instant so the detail cannot contradict its
// own active-restriction count around a start or expiry boundary.
func (r *PostgresRepository) GetManagedUser(ctx context.Context, userID uuid.UUID, asOf time.Time) (ManagedUserDetail, error) {
	return managedUserDetailWithQueries(ctx, r.queries, userID, asOf)
}

func managedUserDetailWithQueries(ctx context.Context, queries *identitydb.Queries, userID uuid.UUID, asOf time.Time) (ManagedUserDetail, error) {
	row, err := queries.GetManagedUser(ctx, identitydb.GetManagedUserParams{
		AsOf:   timestamp(asOf),
		UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, ErrManagedUserNotFound
	}
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("get managed user: %w", err)
	}
	summary, err := managedUserSummaryFromValues(
		row.ID, row.NumericID, row.CredentialRef, row.Username, row.DisplayName, row.Status,
		row.EmailVerified, row.Banned, row.DownloadRestricted,
		row.VipEnabled, row.VipActive, row.VipUntil,
		row.AdministrationVersion, row.ActiveRestrictionCount,
		row.UploadedBytes, row.DownloadedBytes, row.MagicBalance, row.Level,
		row.RoleNames, row.LastActiveAt, row.CreatedAt, row.UpdatedAt,
	)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	operations, err := queries.GetManagedUserOperations(ctx, userID)
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("get managed user operations: %w", err)
	}
	if operations.Experience == "" || operations.RemainingInvites < 0 ||
		operations.SubmittedTorrentCount < 0 || operations.PublishedTorrentCount < 0 ||
		operations.PendingReviewTorrentCount < 0 || operations.DirectInviteCount < 0 {
		return ManagedUserDetail{}, errors.New("managed user operations contain invalid required fields")
	}
	var inviterNumericID *int64
	if operations.InviterNumericID.Valid {
		value := operations.InviterNumericID.Int64
		inviterNumericID = &value
	}
	var inviterUsername *string
	if operations.InviterUsername.Valid {
		value := operations.InviterUsername.String
		inviterUsername = &value
	}
	if (inviterNumericID == nil) != (inviterUsername == nil) {
		return ManagedUserDetail{}, errors.New("managed user inviter projection is inconsistent")
	}
	var registrationMode *RegistrationMode
	var registrationState *RegistrationState
	if operations.RegistrationMode.Valid || operations.RegistrationState.Valid {
		if !operations.RegistrationMode.Valid || !operations.RegistrationState.Valid {
			return ManagedUserDetail{}, errors.New("managed user registration projection is inconsistent")
		}
		mode := RegistrationMode(operations.RegistrationMode.String)
		state := RegistrationState(operations.RegistrationState.String)
		if (mode != RegistrationModeOpen && mode != RegistrationModeInvite) ||
			(state != RegistrationStateReserved && state != RegistrationStateCredentialProvisioned && state != RegistrationStateCompleted) {
			return ManagedUserDetail{}, errors.New("managed user registration projection contains invalid state")
		}
		registrationMode = &mode
		registrationState = &state
	}
	rows, err := queries.ListCurrentAccountRestrictions(ctx, identitydb.ListCurrentAccountRestrictionsParams{
		UserID: userID,
		AsOf:   timestamp(asOf),
	})
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("list current account restrictions: %w", err)
	}
	restrictions := make([]CurrentAccountRestriction, 0, len(rows))
	for _, restrictionRow := range rows {
		if restrictionRow.Kind != string(AccountRestrictionAccountAccess) ||
			!restrictionRow.StartsAt.Valid || !restrictionRow.ExpiresAt.Valid ||
			restrictionRow.ReasonCode == "" || restrictionRow.ReasonSummary == "" ||
			restrictionRow.Version < 1 ||
			!restrictionRow.ExpiresAt.Time.After(restrictionRow.StartsAt.Time) {
			return ManagedUserDetail{}, errors.New("managed user restriction contains invalid required fields")
		}
		restrictions = append(restrictions, CurrentAccountRestriction{
			ID: restrictionRow.ID, Kind: AccountRestrictionKind(restrictionRow.Kind),
			ReasonCode: restrictionRow.ReasonCode, ReasonSummary: restrictionRow.ReasonSummary,
			StartsAt: restrictionRow.StartsAt.Time.UTC(), ExpiresAt: restrictionRow.ExpiresAt.Time.UTC(),
			Version: restrictionRow.Version,
		})
	}
	if int64(len(restrictions)) != summary.ActiveRestrictionCount {
		return ManagedUserDetail{}, errors.New("managed user restriction projection is inconsistent")
	}
	manualRestriction, manualHistory, err := loadManualDownloadRestrictionDetail(ctx, queries, userID)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	vipState, vipHistory, err := loadVIPDetail(ctx, queries, userID, asOf)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	if vipState.Enabled != summary.VIPEnabled || vipState.Active != summary.VIPActive ||
		!sameOptionalTime(vipState.Until, summary.VIPUntil) {
		return ManagedUserDetail{}, errors.New("managed user VIP projection is inconsistent")
	}
	return ManagedUserDetail{
		ManagedUserSummary: summary,
		Experience:         operations.Experience, RemainingInvites: operations.RemainingInvites,
		SubmittedTorrentCount:     operations.SubmittedTorrentCount,
		PublishedTorrentCount:     operations.PublishedTorrentCount,
		PendingReviewTorrentCount: operations.PendingReviewTorrentCount,
		DirectInviteCount:         operations.DirectInviteCount,
		InviterNumericID:          inviterNumericID, InviterUsername: inviterUsername,
		RegistrationMode: registrationMode, RegistrationState: registrationState,
		ActiveRestrictions:               restrictions,
		ManualDownloadRestriction:        manualRestriction,
		ManualDownloadRestrictionHistory: manualHistory,
		VIPState:                         vipState,
		VIPHistory:                       vipHistory,
	}, nil
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func managedUserSummaryFromValues(
	id uuid.UUID,
	numericID int64,
	credentialRef uuid.UUID,
	username string,
	displayName string,
	status string,
	emailVerified bool,
	banned bool,
	downloadRestricted bool,
	vipEnabled bool,
	vipActive bool,
	vipUntilValue pgtype.Timestamptz,
	version int64,
	activeRestrictionCount int64,
	uploadedBytes int64,
	downloadedBytes int64,
	magicBalance int64,
	level int32,
	roleNames []string,
	lastActiveAt pgtype.Timestamptz,
	createdAt pgtype.Timestamptz,
	updatedAt pgtype.Timestamptz,
) (ManagedUserSummary, error) {
	accountStatus := AccountStatus(status)
	if id == uuid.Nil || numericID < 1 || credentialRef == uuid.Nil || username == "" || displayName == "" ||
		(accountStatus != AccountStatusActive && accountStatus != AccountStatusDisabled && accountStatus != AccountStatusPending) ||
		banned != (accountStatus == AccountStatusDisabled) || (vipActive && !vipEnabled) ||
		version < 1 || activeRestrictionCount < 0 || uploadedBytes < 0 || downloadedBytes < 0 ||
		level < 1 || !createdAt.Valid || !updatedAt.Valid {
		return ManagedUserSummary{}, errors.New("managed user contains invalid required fields")
	}
	var lastActive *time.Time
	if lastActiveAt.Valid {
		value := lastActiveAt.Time.UTC()
		lastActive = &value
	}
	var vipUntil *time.Time
	if vipUntilValue.Valid {
		value := vipUntilValue.Time.UTC()
		vipUntil = &value
	}
	return ManagedUserSummary{
		ID: id, NumericID: numericID, credentialRef: credentialRef, Username: username, DisplayName: displayName,
		Status: accountStatus, EmailVerified: emailVerified, Banned: banned,
		DownloadRestricted: downloadRestricted, VIPEnabled: vipEnabled, VIPActive: vipActive, VIPUntil: vipUntil,
		Version:                version,
		ActiveRestrictionCount: activeRestrictionCount,
		UploadedBytes:          uploadedBytes, DownloadedBytes: downloadedBytes,
		MagicBalance: magicBalance, Level: level, RoleNames: append([]string(nil), roleNames...),
		LastActiveAt: lastActive,
		CreatedAt:    createdAt.Time.UTC(), UpdatedAt: updatedAt.Time.UTC(),
	}, nil
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func isActiveStaffChallengeConflict(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) &&
		postgresError.Code == "23505" &&
		postgresError.ConstraintName == "staff_webauthn_challenges_active_parent_idx"
}

var _ Repository = (*PostgresRepository)(nil)
var _ AvatarRepository = (*PostgresRepository)(nil)
var _ StaffRepository = (*PostgresRepository)(nil)
var _ UserAdministrationRepository = (*PostgresRepository)(nil)
