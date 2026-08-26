package personalapikey

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestNewAPIKeyReturnsOnlyHighEntropyRawValueAndDigest(t *testing.T) {
	random := bytes.Repeat([]byte{0x5a}, rawAPIKeyBytes)
	service := &Service{readRandom: func(target []byte) (int, error) {
		return copy(target, random), nil
	}}

	raw, digest, prefix, err := service.newAPIKey()
	if err != nil {
		t.Fatalf("newAPIKey() error = %v", err)
	}
	if len(raw) != 47 || raw[:4] != apiKeyPrefix || prefix != raw[:12] {
		t.Fatalf("unexpected API key shape: len=%d prefix=%q", len(raw), prefix)
	}
	expectedDigest := sha256.Sum256([]byte(raw))
	if !bytes.Equal(digest, expectedDigest[:]) {
		t.Fatal("newAPIKey() did not return the SHA-256 digest of the raw key")
	}
	parsed, err := apiKeyDigest(raw)
	if err != nil || !bytes.Equal(parsed, digest) {
		t.Fatalf("apiKeyDigest() = %x, %v", parsed, err)
	}
	if _, err := apiKeyDigest(raw + "x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("apiKeyDigest(invalid) error = %v", err)
	}
}

func TestNewAPIKeyRejectsShortRandomRead(t *testing.T) {
	service := &Service{readRandom: func(target []byte) (int, error) {
		return copy(target, []byte("short")), nil
	}}
	if _, _, _, err := service.newAPIKey(); err == nil {
		t.Fatal("newAPIKey() accepted a short random read")
	}
}

func TestNormalizeScopesRejectsUnknownAndDuplicateScopes(t *testing.T) {
	scopes, err := NormalizeScopes([]Scope{ScopeTorrentRead, ScopeProfileRead})
	if err != nil || len(scopes) != 2 || scopes[0] != ScopeProfileRead || scopes[1] != ScopeTorrentRead {
		t.Fatalf("NormalizeScopes() = %#v, %v", scopes, err)
	}
	for _, input := range [][]Scope{
		{},
		{ScopeProfileRead, ScopeProfileRead},
		{"admin:write"},
	} {
		if _, err := NormalizeScopes(input); !errors.Is(err, ErrInput) {
			t.Fatalf("NormalizeScopes(%#v) error = %v", input, err)
		}
	}
}

func TestCredentialWriteConflictMapping(t *testing.T) {
	for _, code := range []string{"23505", "40001", "40P01"} {
		if !credentialWriteConflict(&pgconn.PgError{Code: code}) {
			t.Fatalf("PostgreSQL error %s was not classified as a credential conflict", code)
		}
	}
	if credentialWriteConflict(&pgconn.PgError{Code: "23503"}) {
		t.Fatal("foreign-key violation was classified as a credential conflict")
	}
}
