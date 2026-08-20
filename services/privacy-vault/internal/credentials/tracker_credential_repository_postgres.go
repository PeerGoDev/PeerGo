package credentials

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/contracts/go/trackerpasskeyv1"
	"github.com/peergo/peergo/services/privacy-vault/internal/generated/vaultdb"
)

// GetOrCreateTrackerPasskey inserts only for an active credential. ON CONFLICT
// serializes concurrent first downloads; the losing request then reads the
// winner's immutable envelope and returns the same passkey.
func (repository *PostgresRepository) GetOrCreateTrackerPasskey(ctx context.Context, candidate TrackerCredentialRecord) (TrackerCredentialRecord, error) {
	if candidate.CredentialRef == uuid.Nil || len(candidate.Protected.Ciphertext) == 0 || len(candidate.Protected.Nonce) == 0 ||
		candidate.Protected.KeyEpoch == "" || len(candidate.LookupHMAC) != 32 ||
		candidate.FormatProfile != trackerpasskeyv1.ProfileCanonicalHexV1 || candidate.Version != 1 || candidate.CreatedAt.IsZero() {
		return TrackerCredentialRecord{}, ErrTrackerCredentialInput
	}
	row, err := repository.queries.InsertTrackerPasskeyIfAbsent(ctx, vaultdb.InsertTrackerPasskeyIfAbsentParams{
		CredentialRef:      candidate.CredentialRef,
		Ciphertext:         candidate.Protected.Ciphertext,
		Nonce:              candidate.Protected.Nonce,
		EncryptionKeyEpoch: candidate.Protected.KeyEpoch,
		LookupHmac:         candidate.LookupHMAC,
		FormatProfile:      candidate.FormatProfile,
		CreatedAt:          vaultTimestamp(candidate.CreatedAt),
	})
	if err == nil {
		return trackerCredentialRecord(
			row.CredentialRef, row.Ciphertext, row.Nonce, row.EncryptionKeyEpoch,
			row.LookupHmac, row.FormatProfile, row.Version, row.CreatedAt,
		)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return TrackerCredentialRecord{}, fmt.Errorf("insert Tracker passkey: %w", err)
	}

	existing, err := repository.queries.GetTrackerPasskey(ctx, candidate.CredentialRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrackerCredentialRecord{}, ErrTrackerCredentialNotFound
	}
	if err != nil {
		return TrackerCredentialRecord{}, fmt.Errorf("get Tracker passkey: %w", err)
	}
	return trackerCredentialRecord(
		existing.CredentialRef, existing.Ciphertext, existing.Nonce,
		existing.EncryptionKeyEpoch, existing.LookupHmac, existing.FormatProfile, existing.Version,
		existing.CreatedAt,
	)
}

func trackerCredentialRecord(
	credentialRef uuid.UUID,
	ciphertext, nonce []byte,
	keyEpoch string,
	lookupHMAC []byte,
	formatProfile string,
	version int64,
	createdAt pgtype.Timestamptz,
) (TrackerCredentialRecord, error) {
	if credentialRef == uuid.Nil || len(ciphertext) == 0 || len(nonce) == 0 || keyEpoch == "" ||
		len(lookupHMAC) != 32 || (formatProfile != trackerpasskeyv1.ProfileCanonicalHexV1 &&
		formatProfile != trackerpasskeyv1.ProfilePtYesAlnum32V1) || version < 1 || !createdAt.Valid || createdAt.Time.IsZero() {
		return TrackerCredentialRecord{}, errors.New("stored Tracker passkey row is invalid")
	}
	return TrackerCredentialRecord{
		CredentialRef: credentialRef,
		Protected: ProtectedSecret{
			Ciphertext: append([]byte(nil), ciphertext...),
			Nonce:      append([]byte(nil), nonce...),
			KeyEpoch:   keyEpoch,
		},
		LookupHMAC:    append([]byte(nil), lookupHMAC...),
		FormatProfile: formatProfile,
		Version:       version,
		CreatedAt:     createdAt.Time.UTC(),
	}, nil
}

var _ TrackerCredentialRepository = (*PostgresRepository)(nil)
