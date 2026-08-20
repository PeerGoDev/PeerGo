package credentials

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/contracts/go/trackerpasskeyv1"
)

type trackerCredentialRepositoryFixture struct {
	record *TrackerCredentialRecord
}

func TestTrackerCredentialServiceReadsIsolatedPtYesProfile(t *testing.T) {
	t.Parallel()

	credentialRef := uuid.New()
	legacyPasskey := "PtYesLegacyPasskey2026ABCDEF1234"
	lookupKey := []byte("tracker-passkey-lookup-key-test-2026")
	lookup, err := trackerpasskeyv1.LookupHMACForProfile(
		lookupKey,
		trackerpasskeyv1.ProfilePtYesAlnum32V1,
		legacyPasskey,
	)
	if err != nil {
		t.Fatal(err)
	}
	protector, err := NewSecretProtector(
		[]byte("0123456789abcdef0123456789abcdef"),
		"test-2026-08",
		bytes.NewReader(bytes.Repeat([]byte{0x6b}, 64)),
	)
	if err != nil {
		t.Fatal(err)
	}
	protected, err := protector.Protect(
		trackerPasskeyRecordKind,
		credentialRef,
		credentialRef,
		[]byte(legacyPasskey),
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 10, 15, 0, 0, 0, time.UTC)
	repository := &trackerCredentialRepositoryFixture{record: &TrackerCredentialRecord{
		CredentialRef: credentialRef,
		Protected:     protected,
		LookupHMAC:    lookup[:],
		FormatProfile: trackerpasskeyv1.ProfilePtYesAlnum32V1,
		Version:       1,
		CreatedAt:     now,
	}}
	service, err := NewTrackerCredentialService(
		repository,
		protector,
		lookupKey,
		TrackerCredentialServiceConfig{
			Random: bytes.NewReader(bytes.Repeat([]byte{0x7c}, 16)),
			Now:    func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := service.GetOrCreate(context.Background(), credentialRef)
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if credential.Passkey != legacyPasskey || credential.LookupHMAC != lookup ||
		credential.FormatProfile != trackerpasskeyv1.ProfilePtYesAlnum32V1 {
		t.Fatalf("legacy Tracker credential = %+v", credential)
	}
}

func (fixture *trackerCredentialRepositoryFixture) GetOrCreateTrackerPasskey(_ context.Context, candidate TrackerCredentialRecord) (TrackerCredentialRecord, error) {
	if fixture.record == nil {
		copyOfCandidate := candidate
		copyOfCandidate.Protected.Ciphertext = append([]byte(nil), candidate.Protected.Ciphertext...)
		copyOfCandidate.Protected.Nonce = append([]byte(nil), candidate.Protected.Nonce...)
		copyOfCandidate.LookupHMAC = append([]byte(nil), candidate.LookupHMAC...)
		fixture.record = &copyOfCandidate
	}
	return *fixture.record, nil
}

func TestTrackerCredentialServiceReturnsOneStableEncryptedPasskey(t *testing.T) {
	t.Parallel()

	credentialRef := uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222")
	now := time.Date(2026, time.August, 8, 19, 0, 0, 0, time.UTC)
	repository := &trackerCredentialRepositoryFixture{}
	protector, err := NewSecretProtector(
		[]byte("0123456789abcdef0123456789abcdef"),
		"test-2026-08",
		bytes.NewReader(bytes.Repeat([]byte{0x7a}, 64)),
	)
	if err != nil {
		t.Fatalf("NewSecretProtector() error = %v", err)
	}
	service, err := NewTrackerCredentialService(
		repository,
		protector,
		[]byte("tracker-passkey-lookup-key-test-2026"),
		TrackerCredentialServiceConfig{
			Random: bytes.NewReader(append(bytes.Repeat([]byte{0x11}, 16), bytes.Repeat([]byte{0x22}, 16)...)),
			Now:    func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatalf("NewTrackerCredentialService() error = %v", err)
	}

	first, err := service.GetOrCreate(context.Background(), credentialRef)
	if err != nil {
		t.Fatalf("first GetOrCreate() error = %v", err)
	}
	second, err := service.GetOrCreate(context.Background(), credentialRef)
	if err != nil {
		t.Fatalf("second GetOrCreate() error = %v", err)
	}
	if first.Passkey != "11111111111111111111111111111111" || second.Passkey != first.Passkey {
		t.Fatalf("passkeys = %q, %q", first.Passkey, second.Passkey)
	}
	if first.LookupHMAC != second.LookupHMAC || first.Version != 1 || first.CreatedAt != now {
		t.Fatalf("first = %+v second = %+v", first, second)
	}
	if bytes.Contains(repository.record.Protected.Ciphertext, []byte(first.Passkey)) {
		t.Fatal("persisted ciphertext contains the raw passkey")
	}
}

func TestTrackerCredentialServiceFailsClosedOnLookupFork(t *testing.T) {
	t.Parallel()

	credentialRef := uuid.New()
	protector, err := NewSecretProtector(
		[]byte("0123456789abcdef0123456789abcdef"),
		"test-2026-08",
		bytes.NewReader(bytes.Repeat([]byte{0x3a}, 32)),
	)
	if err != nil {
		t.Fatalf("NewSecretProtector() error = %v", err)
	}
	repository := &trackerCredentialRepositoryFixture{}
	service, err := NewTrackerCredentialService(
		repository,
		protector,
		[]byte("tracker-passkey-lookup-key-test-2026"),
		TrackerCredentialServiceConfig{Random: bytes.NewReader(bytes.Repeat([]byte{0x44}, 16))},
	)
	if err != nil {
		t.Fatalf("NewTrackerCredentialService() error = %v", err)
	}
	if _, err := service.GetOrCreate(context.Background(), credentialRef); err != nil {
		t.Fatalf("initial GetOrCreate() error = %v", err)
	}
	repository.record.LookupHMAC[0] ^= 0xff
	if _, err := service.GetOrCreate(context.Background(), credentialRef); err == nil {
		t.Fatal("GetOrCreate() accepted a forked lookup HMAC")
	}
}
