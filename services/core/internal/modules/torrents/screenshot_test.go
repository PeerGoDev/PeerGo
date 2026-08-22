package torrents

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/google/uuid"
)

func TestPrepareTorrentScreenshotsUsesDecodedContentAndStableOrder(t *testing.T) {
	t.Parallel()

	first := screenshotPNG(t, 4, 3, color.RGBA{R: 255, A: 255})
	second := screenshotPNG(t, 2, 5, color.RGBA{B: 255, A: 255})
	prepared, err := prepareTorrentScreenshots([]TorrentScreenshotInput{
		{Raw: first},
		{Raw: second},
	}, uuid.New)
	if err != nil {
		t.Fatalf("prepareTorrentScreenshots() error = %v", err)
	}
	if len(prepared) != 2 || prepared[0].Position != 0 || prepared[1].Position != 1 ||
		prepared[0].ContentType != "image/png" || prepared[0].Extension != ".png" ||
		prepared[0].Width != 4 || prepared[0].Height != 3 || prepared[0].ContentSHA256 == prepared[1].ContentSHA256 {
		t.Fatalf("prepared screenshots = %+v", prepared)
	}
}

func TestPrepareTorrentScreenshotsRejectsClaimsWithoutValidImageBytes(t *testing.T) {
	t.Parallel()

	_, err := prepareTorrentScreenshots([]TorrentScreenshotInput{{Raw: []byte("not an image")}}, uuid.New)
	if code, ok := ValidationCodeOf(err); !ok || code != CodeInvalidScreenshot {
		t.Fatalf("prepareTorrentScreenshots() error = %v, code=%q", err, code)
	}
}

func TestPrepareTorrentScreenshotsRejectsSourceAboveTwoMiB(t *testing.T) {
	t.Parallel()

	_, err := prepareTorrentScreenshots([]TorrentScreenshotInput{{
		Raw: make([]byte, MaxTorrentScreenshotBytes+1),
	}}, uuid.New)
	if code, ok := ValidationCodeOf(err); !ok || code != CodeObjectTooLarge {
		t.Fatalf("prepareTorrentScreenshots() error = %v, code=%q", err, code)
	}
}

func TestStoredScreenshotMetadataKeepsLegacyEnvelopeSeparateFromLiveAdmission(t *testing.T) {
	t.Parallel()

	// 4096x6144 is present in the audited Rousi archive. It is just above the
	// 25 MP live-upload limit, but remains within the migration-only 100 MP
	// envelope and must be readable after object verification.
	if !validStoredScreenshotMetadata("image/jpeg", 2_075_681, 4096, 6144) {
		t.Fatal("verified legacy screenshot should remain readable")
	}
	if validStoredScreenshotMetadata("image/jpeg", 2_075_681, 10_001, 10_000) {
		t.Fatal("stored screenshot above the migration pixel envelope was accepted")
	}
	if validStoredScreenshotMetadata("image/jpeg", MaxStoredTorrentScreenshotBytes+1, 4096, 6144) {
		t.Fatal("stored screenshot above the migration byte envelope was accepted")
	}
	if validStoredScreenshotMetadata("image/svg+xml", 1024, 4096, 6144) {
		t.Fatal("unsupported stored screenshot type was accepted")
	}
}

func TestStoreAndVerifyTorrentScreenshotsUsesContentAddressedObjectStore(t *testing.T) {
	t.Parallel()

	prepared, err := prepareTorrentScreenshots([]TorrentScreenshotInput{{
		Raw: screenshotPNG(t, 3, 2, color.RGBA{G: 255, A: 255}),
	}}, uuid.New)
	if err != nil {
		t.Fatal(err)
	}
	store := newMemoryObjectStore("local-primary")
	registry, err := NewStoreRegistry(store)
	if err != nil {
		t.Fatal(err)
	}
	service := &TorrentUploadService{stores: registry, activeBackendID: store.BackendID()}
	stored, err := service.storeAndVerifyScreenshots(context.Background(), prepared)
	if err != nil {
		t.Fatalf("storeAndVerifyScreenshots() error = %v", err)
	}
	wantKey := TorrentScreenshotObjectKey(prepared[0].ContentSHA256, ".png")
	if len(stored) != 1 || stored[0].ObjectKey != wantKey || stored[0].BackendID != store.BackendID() || len(store.objects) != 1 {
		t.Fatalf("stored=%+v objects=%d want key=%q", stored, len(store.objects), wantKey)
	}
}

func screenshotPNG(t *testing.T, width, height int, fill color.Color) []byte {
	t.Helper()
	imageValue := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			imageValue.Set(x, y, fill)
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, imageValue); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
