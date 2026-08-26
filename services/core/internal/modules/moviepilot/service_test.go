package moviepilot

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/peergo/peergo/services/core/internal/modules/catalog"
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
	if _, err := apiKeyDigest(raw + "x"); !errors.Is(err, ErrCredentialInvalid) {
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

func TestDownloadCapabilityIsShortLivedAndBoundToTorrent(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	service := &Service{
		signingKey: bytes.Repeat([]byte{0x3c}, 32),
		now:        func() time.Time { return now },
	}
	userID := uuid.MustParse("0198f20a-6da8-7e51-9c64-111111111111")
	raw, err := service.issueDownloadCapability(userID, 9830, 7, now)
	if err != nil {
		t.Fatalf("issueDownloadCapability() error = %v", err)
	}
	resolvedUserID, version, err := service.validateDownloadCapability(raw, 9830)
	if err != nil || resolvedUserID != userID || version != 7 {
		t.Fatalf("validateDownloadCapability() = %s, %d, %v", resolvedUserID, version, err)
	}
	if _, _, err := service.validateDownloadCapability(raw, 9831); !errors.Is(err, ErrCapabilityInvalid) {
		t.Fatalf("torrent-bound capability error = %v", err)
	}
	replacement := "A"
	if raw[len(raw)-1:] == replacement {
		replacement = "B"
	}
	tampered := raw[:len(raw)-1] + replacement
	if _, _, err := service.validateDownloadCapability(tampered, 9830); !errors.Is(err, ErrCapabilityInvalid) {
		t.Fatalf("tampered capability error = %v", err)
	}
	now = now.Add(downloadCapabilityTTL + time.Second)
	if _, _, err := service.validateDownloadCapability(raw, 9830); !errors.Is(err, ErrCapabilityInvalid) {
		t.Fatalf("expired capability error = %v", err)
	}
}

func TestMoviePilotCategoryAndPromotionCompatibility(t *testing.T) {
	if value, ok := peerGoCategory("movie"); !ok || value != "movies" {
		t.Fatalf("peerGoCategory(movie) = %q, %v", value, ok)
	}
	if value, ok := peerGoCategory("animation"); !ok || value != "anime" {
		t.Fatalf("peerGoCategory(animation) = %q, %v", value, ok)
	}
	if value, ok := peerGoCategory("ebook"); !ok || value != "ebooks" {
		t.Fatalf("peerGoCategory(ebook) = %q, %v", value, ok)
	}
	if value := moviePilotCategory("games"); value != "game" {
		t.Fatalf("moviePilotCategory(games) = %q", value)
	}
	if _, ok := peerGoCategory("not-a-category"); ok {
		t.Fatal("peerGoCategory() accepted an unknown category")
	}
	result := promotion(catalog.PromotionDoubleUploadHalfDownload, nil)
	if !result.Active || result.Type != 6 || result.TimeType != 1 || result.UploadFactor != 2 || result.DownloadFactor != 0.5 {
		t.Fatalf("promotion() = %+v", result)
	}
}

func TestMoviePilotCredentialWriteConflictMapping(t *testing.T) {
	for _, code := range []string{"23505", "40001", "40P01"} {
		if !moviePilotCredentialWriteConflict(&pgconn.PgError{Code: code}) {
			t.Fatalf("PostgreSQL error %s was not classified as a credential conflict", code)
		}
	}
	if moviePilotCredentialWriteConflict(&pgconn.PgError{Code: "23503"}) {
		t.Fatal("foreign-key violation was classified as a credential conflict")
	}
}
