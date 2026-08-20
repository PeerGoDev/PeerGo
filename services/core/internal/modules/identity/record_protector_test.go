package identity

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestRecordProtectorEncryptsAndBindsEveryRecordIdentityField(t *testing.T) {
	t.Parallel()

	key := []byte("0123456789abcdef0123456789abcdef")
	nonce := bytes.Repeat([]byte{0x31}, 12)
	protector, err := NewRecordProtector(key, "epoch-1", bytes.NewReader(nonce))
	if err != nil {
		t.Fatalf("NewRecordProtector() error = %v", err)
	}
	userID := uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	recordID := []byte("credential-one")
	plaintext := []byte(`{"publicKey":"server-only-record"}`)

	protected, err := protector.Seal(staffCredentialRecordKind, userID, recordID, plaintext)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if bytes.Equal(protected.Ciphertext, plaintext) || bytes.Contains(protected.Ciphertext, []byte("server-only-record")) {
		t.Fatal("Seal() exposed plaintext in the stored ciphertext")
	}
	if !bytes.Equal(protected.Nonce, nonce) || protected.KeyEpoch != "epoch-1" {
		t.Fatalf("Seal() envelope = %+v", protected)
	}
	opened, err := protector.Open(staffCredentialRecordKind, userID, recordID, protected)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("Open() = %q, want %q", opened, plaintext)
	}

	otherUser := uuid.MustParse("0198f20a-6da8-7e51-9c64-222222222222")
	tamperedCiphertext := cloneProtectedRecord(protected)
	tamperedCiphertext.Ciphertext[0] ^= 0xff
	tamperedNonce := cloneProtectedRecord(protected)
	tamperedNonce.Nonce[0] ^= 0xff
	tamperedEpoch := cloneProtectedRecord(protected)
	tamperedEpoch.KeyEpoch = "epoch-2"

	tests := []struct {
		name     string
		kind     string
		userID   uuid.UUID
		recordID []byte
		record   ProtectedRecord
	}{
		{name: "record kind", kind: staffChallengeRecordKind, userID: userID, recordID: recordID, record: protected},
		{name: "user", kind: staffCredentialRecordKind, userID: otherUser, recordID: recordID, record: protected},
		{name: "record ID", kind: staffCredentialRecordKind, userID: userID, recordID: []byte("credential-two"), record: protected},
		{name: "ciphertext", kind: staffCredentialRecordKind, userID: userID, recordID: recordID, record: tamperedCiphertext},
		{name: "nonce", kind: staffCredentialRecordKind, userID: userID, recordID: recordID, record: tamperedNonce},
		{name: "key epoch", kind: staffCredentialRecordKind, userID: userID, recordID: recordID, record: tamperedEpoch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := protector.Open(test.kind, test.userID, test.recordID, test.record); err == nil {
				t.Fatal("Open() accepted an envelope copied or modified outside its bound row")
			}
		})
	}
}

func TestRecordProtectorRejectsUnsafeConfigurationAndEntropyFailure(t *testing.T) {
	t.Parallel()

	if _, err := NewRecordProtector([]byte("short"), "epoch", nil); err == nil {
		t.Fatal("NewRecordProtector() accepted a non-AES-256 key")
	}
	protector, err := NewRecordProtector(
		[]byte("0123456789abcdef0123456789abcdef"),
		"epoch",
		errorReader{err: errors.New("entropy unavailable")},
	)
	if err != nil {
		t.Fatalf("NewRecordProtector() error = %v", err)
	}
	recordID := uuid.MustParse("0198f20a-6da8-7e51-9c64-333333333333")
	_, err = protector.Seal(
		staffChallengeRecordKind,
		uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111"),
		recordID[:],
		[]byte("session"),
	)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("entropy unavailable")) {
		t.Fatalf("Seal() error = %v, want entropy failure", err)
	}
}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

func cloneProtectedRecord(record ProtectedRecord) ProtectedRecord {
	return ProtectedRecord{
		Ciphertext: append([]byte(nil), record.Ciphertext...),
		Nonce:      append([]byte(nil), record.Nonce...),
		KeyEpoch:   record.KeyEpoch,
	}
}
