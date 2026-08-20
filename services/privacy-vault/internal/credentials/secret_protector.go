package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
)

const vaultSecretAADDomain = "peergo:vault:protected-secret:v1\x00"

// ProtectedSecret is an AES-256-GCM envelope. KeyEpoch is persisted beside the
// ciphertext so key rotation fails closed instead of trying the wrong key.
type ProtectedSecret struct {
	Ciphertext []byte
	Nonce      []byte
	KeyEpoch   string
}

// SecretProtector is intentionally local to Privacy Vault. Core never receives
// this key and therefore cannot decrypt TOTP seeds or recovery-code bundles.
type SecretProtector struct {
	aead     cipher.AEAD
	keyEpoch string
	random   io.Reader
}

func NewSecretProtector(key []byte, keyEpoch string, random io.Reader) (*SecretProtector, error) {
	if len(key) != 32 {
		return nil, errors.New("vault secret encryption key must contain exactly 32 bytes")
	}
	if keyEpoch == "" || len(keyEpoch) > 80 {
		return nil, errors.New("vault secret encryption key epoch is invalid")
	}
	if random == nil {
		random = rand.Reader
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create vault secret cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create vault secret AEAD: %w", err)
	}
	return &SecretProtector{aead: aead, keyEpoch: keyEpoch, random: random}, nil
}

func (protector *SecretProtector) Protect(kind string, credentialRef, recordID uuid.UUID, plaintext []byte) (ProtectedSecret, error) {
	if protector == nil || kind == "" || credentialRef == uuid.Nil || recordID == uuid.Nil || len(plaintext) == 0 {
		return ProtectedSecret{}, errors.New("protected secret input is invalid")
	}
	nonce := make([]byte, protector.aead.NonceSize())
	if _, err := io.ReadFull(protector.random, nonce); err != nil {
		return ProtectedSecret{}, fmt.Errorf("generate protected secret nonce: %w", err)
	}
	ciphertext := protector.aead.Seal(nil, nonce, plaintext, protectedSecretAAD(kind, credentialRef, recordID))
	return ProtectedSecret{
		Ciphertext: ciphertext,
		Nonce:      nonce,
		KeyEpoch:   protector.keyEpoch,
	}, nil
}

func (protector *SecretProtector) Unprotect(kind string, credentialRef, recordID uuid.UUID, protected ProtectedSecret) ([]byte, error) {
	if protector == nil || kind == "" || credentialRef == uuid.Nil || recordID == uuid.Nil ||
		protected.KeyEpoch != protector.keyEpoch || len(protected.Nonce) != protector.aead.NonceSize() || len(protected.Ciphertext) == 0 {
		return nil, errors.New("protected secret envelope is invalid")
	}
	plaintext, err := protector.aead.Open(nil, protected.Nonce, protected.Ciphertext, protectedSecretAAD(kind, credentialRef, recordID))
	if err != nil {
		return nil, errors.New("protected secret authentication failed")
	}
	return plaintext, nil
}

func protectedSecretAAD(kind string, credentialRef, recordID uuid.UUID) []byte {
	aad := make([]byte, 0, len(vaultSecretAADDomain)+len(kind)+1+len(credentialRef)+len(recordID))
	aad = append(aad, vaultSecretAADDomain...)
	aad = append(aad, kind...)
	aad = append(aad, 0)
	aad = append(aad, credentialRef[:]...)
	aad = append(aad, recordID[:]...)
	return aad
}
