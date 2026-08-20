package identity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
)

const (
	staffCredentialRecordKind          = "staff-credential-v1"
	staffChallengeRecordKind           = "staff-challenge-v1"
	staffEnrollmentChallengeRecordKind = "staff-enrollment-challenge-v1"
	maxProtectedPlaintext              = 64 * 1024
)

// RecordProtector authenticates and encrypts WebAuthn credential and ceremony
// records before PostgreSQL sees them. The AAD binds ciphertext to its record
// kind, user and identifier, so copying a valid envelope into another row is
// rejected even when the database constraints would otherwise accept it.
type RecordProtector struct {
	aead   cipher.AEAD
	epoch  string
	random io.Reader
}

// NewRecordProtector constructs an AES-256-GCM protector. The key is exact-size
// by design; accepting arbitrary passphrases here would silently turn this
// security boundary into an unspecified password KDF.
func NewRecordProtector(key []byte, epoch string, random io.Reader) (*RecordProtector, error) {
	if len(key) != 32 {
		return nil, errors.New("WebAuthn record key must contain exactly 32 bytes")
	}
	epoch = strings.TrimSpace(epoch)
	if epoch == "" || len(epoch) > 64 {
		return nil, errors.New("WebAuthn record key epoch must contain 1 to 64 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create WebAuthn record cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create WebAuthn record AEAD: %w", err)
	}
	if random == nil {
		random = rand.Reader
	}
	return &RecordProtector{aead: aead, epoch: epoch, random: random}, nil
}

func (protector *RecordProtector) Seal(kind string, userID uuid.UUID, recordID, plaintext []byte) (ProtectedRecord, error) {
	if err := validateProtectedRecordIdentity(kind, userID, recordID); err != nil {
		return ProtectedRecord{}, err
	}
	if len(plaintext) == 0 || len(plaintext) > maxProtectedPlaintext-protector.aead.Overhead() {
		return ProtectedRecord{}, errors.New("protected WebAuthn plaintext is outside supported bounds")
	}
	nonce := make([]byte, protector.aead.NonceSize())
	if _, err := io.ReadFull(protector.random, nonce); err != nil {
		return ProtectedRecord{}, fmt.Errorf("generate WebAuthn record nonce: %w", err)
	}
	ciphertext := protector.aead.Seal(nil, nonce, plaintext, protectedRecordAAD(kind, userID, recordID))
	return ProtectedRecord{
		Ciphertext: ciphertext,
		Nonce:      nonce,
		KeyEpoch:   protector.epoch,
	}, nil
}

func (protector *RecordProtector) Open(kind string, userID uuid.UUID, recordID []byte, record ProtectedRecord) ([]byte, error) {
	if err := validateProtectedRecordIdentity(kind, userID, recordID); err != nil {
		return nil, err
	}
	if record.KeyEpoch != protector.epoch || len(record.Nonce) != protector.aead.NonceSize() || len(record.Ciphertext) <= protector.aead.Overhead() || len(record.Ciphertext) > maxProtectedPlaintext {
		return nil, errors.New("protected WebAuthn record envelope is invalid")
	}
	plaintext, err := protector.aead.Open(nil, record.Nonce, record.Ciphertext, protectedRecordAAD(kind, userID, recordID))
	if err != nil {
		return nil, errors.New("protected WebAuthn record authentication failed")
	}
	return plaintext, nil
}

func validateProtectedRecordIdentity(kind string, userID uuid.UUID, recordID []byte) error {
	if kind == "" || len(kind) > 64 || userID == uuid.Nil || len(recordID) == 0 || len(recordID) > 1024 {
		return errors.New("protected WebAuthn record identity is invalid")
	}
	return nil
}

func protectedRecordAAD(kind string, userID uuid.UUID, recordID []byte) []byte {
	// Explicit lengths avoid ambiguous concatenation if future record kinds use
	// binary identifiers containing separator bytes.
	aad := make([]byte, 0, 2+len(kind)+len(userID)+2+len(recordID))
	aad = binary.BigEndian.AppendUint16(aad, uint16(len(kind)))
	aad = append(aad, kind...)
	aad = append(aad, userID[:]...)
	aad = binary.BigEndian.AppendUint16(aad, uint16(len(recordID)))
	aad = append(aad, recordID...)
	return aad
}
