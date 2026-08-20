package credentials

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/contracts/go/trackerpasskeyv1"
)

const (
	trackerPasskeyRecordKind = "tracker-passkey"
)

var (
	ErrTrackerCredentialInput    = errors.New("Tracker credential input is invalid")
	ErrTrackerCredentialNotFound = errors.New("Tracker credential owner was not found")
)

// TrackerCredentialRecord is the complete Vault-owned record. Callers outside
// Privacy Vault never receive Protected or the encryption key epoch.
type TrackerCredentialRecord struct {
	CredentialRef uuid.UUID
	Protected     ProtectedSecret
	LookupHMAC    []byte
	FormatProfile string
	Version       int64
	CreatedAt     time.Time
}

// TrackerCredential is the narrow result allowed to cross the internal service
// boundary. Passkey must exist only long enough for Core to build one metainfo
// response; it must never be logged, cached, or persisted outside Vault.
type TrackerCredential struct {
	CredentialRef uuid.UUID
	Passkey       string
	LookupHMAC    [sha256.Size]byte
	FormatProfile string
	Version       int64
	CreatedAt     time.Time
}

type TrackerCredentialRepository interface {
	GetOrCreateTrackerPasskey(context.Context, TrackerCredentialRecord) (TrackerCredentialRecord, error)
}

type TrackerCredentialServiceConfig struct {
	Random io.Reader
	Now    func() time.Time
}

type TrackerCredentialService struct {
	repository TrackerCredentialRepository
	protector  *SecretProtector
	lookupKey  []byte
	random     io.Reader
	now        func() time.Time
}

func NewTrackerCredentialService(
	repository TrackerCredentialRepository,
	protector *SecretProtector,
	lookupKey []byte,
	config TrackerCredentialServiceConfig,
) (*TrackerCredentialService, error) {
	if repository == nil || protector == nil {
		return nil, errors.New("Tracker credential service dependencies are required")
	}
	if len(lookupKey) < 32 {
		return nil, errors.New("Tracker passkey lookup key must contain at least 32 bytes")
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &TrackerCredentialService{
		repository: repository,
		protector:  protector,
		lookupKey:  append([]byte(nil), lookupKey...),
		random:     config.Random,
		now:        config.Now,
	}, nil
}

// GetOrCreate returns one stable passkey per credential reference. A random
// candidate is encrypted before the insert attempt; if another request already
// won, the repository returns that committed envelope and this candidate is
// discarded. This makes the cross-service operation safe to retry after either
// side commits and the other side times out.
func (service *TrackerCredentialService) GetOrCreate(ctx context.Context, credentialRef uuid.UUID) (TrackerCredential, error) {
	if credentialRef == uuid.Nil {
		return TrackerCredential{}, ErrTrackerCredentialInput
	}
	raw := make([]byte, trackerpasskeyv1.RawBytes)
	if _, err := io.ReadFull(service.random, raw); err != nil {
		return TrackerCredential{}, fmt.Errorf("generate Tracker passkey: %w", err)
	}
	passkey := hex.EncodeToString(raw)
	lookup, err := trackerpasskeyv1.LookupHMAC(service.lookupKey, passkey)
	if err != nil {
		return TrackerCredential{}, ErrTrackerCredentialInput
	}
	protected, err := service.protector.Protect(
		trackerPasskeyRecordKind,
		credentialRef,
		credentialRef,
		[]byte(passkey),
	)
	if err != nil {
		return TrackerCredential{}, fmt.Errorf("protect Tracker passkey: %w", err)
	}
	createdAt := service.now().UTC()
	if createdAt.IsZero() {
		return TrackerCredential{}, ErrTrackerCredentialInput
	}
	record, err := service.repository.GetOrCreateTrackerPasskey(ctx, TrackerCredentialRecord{
		CredentialRef: credentialRef,
		Protected:     protected,
		LookupHMAC:    lookup[:],
		FormatProfile: trackerpasskeyv1.ProfileCanonicalHexV1,
		Version:       1,
		CreatedAt:     createdAt,
	})
	if err != nil {
		return TrackerCredential{}, err
	}
	plaintext, err := service.protector.Unprotect(
		trackerPasskeyRecordKind,
		record.CredentialRef,
		record.CredentialRef,
		record.Protected,
	)
	if err != nil {
		return TrackerCredential{}, fmt.Errorf("unprotect Tracker passkey: %w", err)
	}
	defer clear(plaintext)
	persistedPasskey := string(plaintext)
	if trackerpasskeyv1.ValidateForProfile(record.FormatProfile, persistedPasskey) != nil ||
		len(record.LookupHMAC) != sha256.Size || record.Version < 1 || record.CreatedAt.IsZero() {
		return TrackerCredential{}, errors.New("persisted Tracker credential is invalid")
	}
	persistedLookup, err := trackerpasskeyv1.LookupHMACForProfile(
		service.lookupKey,
		record.FormatProfile,
		persistedPasskey,
	)
	if err != nil {
		return TrackerCredential{}, errors.New("persisted Tracker credential lookup is invalid")
	}
	if subtle.ConstantTimeCompare(persistedLookup[:], record.LookupHMAC) != 1 {
		return TrackerCredential{}, errors.New("persisted Tracker credential lookup is inconsistent")
	}
	return TrackerCredential{
		CredentialRef: record.CredentialRef,
		Passkey:       persistedPasskey,
		LookupHMAC:    persistedLookup,
		FormatProfile: record.FormatProfile,
		Version:       record.Version,
		CreatedAt:     record.CreatedAt.UTC(),
	}, nil
}

func validTrackerPasskey(passkey string) bool {
	_, err := trackerpasskeyv1.DetectProfile(passkey)
	return err == nil
}
