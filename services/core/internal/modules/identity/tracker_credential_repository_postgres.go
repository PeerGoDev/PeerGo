package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/peergo/peergo/services/core/internal/generated/identitydb"
)

func (repository *PostgresRepository) BindTrackerCredential(
	ctx context.Context,
	userID uuid.UUID,
	credentialRef uuid.UUID,
	credential TrackerCredential,
	boundAt time.Time,
) (TrackerCredentialProjection, error) {
	if userID == uuid.Nil || credentialRef == uuid.Nil || credential.Version < 1 || boundAt.IsZero() {
		return TrackerCredentialProjection{}, ErrTrackerCredentialStateConflict
	}
	row, err := repository.queries.UpsertTrackerPasskeyProjection(ctx, identitydb.UpsertTrackerPasskeyProjectionParams{
		UserID:        userID,
		CredentialRef: credentialRef,
		LookupHmac:    credential.LookupHMAC[:],
		VaultVersion:  credential.Version,
		BoundAt:       timestamp(boundAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackerCredentialProjection{}, ErrTrackerCredentialStateConflict
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.Code == "23505" {
		return TrackerCredentialProjection{}, ErrTrackerCredentialStateConflict
	}
	if err != nil {
		return TrackerCredentialProjection{}, fmt.Errorf("bind Tracker credential projection: %w", err)
	}
	if row.UserID == uuid.Nil || row.CredentialRef == uuid.Nil || len(row.LookupHmac) != 32 ||
		row.VaultVersion < 1 || !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
		return TrackerCredentialProjection{}, ErrTrackerCredentialStateConflict
	}
	var lookup [32]byte
	copy(lookup[:], row.LookupHmac)
	return TrackerCredentialProjection{
		UserID: row.UserID, CredentialRef: row.CredentialRef,
		LookupHMAC: lookup, VaultVersion: row.VaultVersion,
		CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC(),
	}, nil
}

var _ TrackerCredentialProjectionRepository = (*PostgresRepository)(nil)
