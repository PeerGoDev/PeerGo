package moviepilot

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/personalapikey"
)

func TestMoviePilotAdapterEnforcesSharedPersonalAPIKeyScopes(t *testing.T) {
	service := &Service{}
	credential := personalapikey.AuthenticatedCredential{
		Credential: personalapikey.Credential{Scopes: []personalapikey.Scope{personalapikey.ScopeTorrentRead}},
	}
	if _, err := service.Profile(context.Background(), credential); !errors.Is(err, personalapikey.ErrScopeDenied) {
		t.Fatalf("Profile() scope error = %v", err)
	}
	if _, err := service.SeedingReward(context.Background(), credential); !errors.Is(err, personalapikey.ErrScopeDenied) {
		t.Fatalf("SeedingReward() scope error = %v", err)
	}
	if _, err := service.Torrent(context.Background(), credential, 9830); !errors.Is(err, personalapikey.ErrScopeDenied) {
		t.Fatalf("Torrent() download scope error = %v", err)
	}
	if _, err := service.DownloadWithCredential(context.Background(), credential, 9830); !errors.Is(err, personalapikey.ErrScopeDenied) {
		t.Fatalf("DownloadWithCredential() scope error = %v", err)
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
