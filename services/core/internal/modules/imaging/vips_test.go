package imaging

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os/exec"
	"testing"
)

func TestVipsTransformerProducesVerifiedWebPWithoutUpscaling(t *testing.T) {
	t.Parallel()
	binary, err := exec.LookPath("vips")
	if err != nil {
		t.Skip("libvips is not installed in this test environment")
	}
	var source bytes.Buffer
	input := image.NewRGBA(image.Rect(0, 0, 48, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 48; x++ {
			input.SetRGBA(x, y, color.RGBA{R: uint8(x * 5), G: uint8(y * 7), B: 96, A: 255})
		}
	}
	if err := png.Encode(&source, input); err != nil {
		t.Fatal(err)
	}
	transformer, err := NewVipsTransformer(VipsConfig{Binary: binary, TempDir: t.TempDir(), Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := transformer.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	output, err := transformer.Transform(
		context.Background(), SourceTorrentScreenshot, VariantDisplay,
		source.Bytes(), ".png",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !output.Descriptor.Valid() || output.Descriptor.ByteLength != int64(len(output.Bytes)) {
		t.Fatalf("invalid output descriptor: %+v", output.Descriptor)
	}
	if output.Width != 48 || output.Height != 32 {
		t.Fatalf("output dimensions = %dx%d, want 48x32", output.Width, output.Height)
	}
	configuration, format, err := image.DecodeConfig(bytes.NewReader(output.Bytes))
	if err != nil || format != "webp" || configuration.Width != 48 || configuration.Height != 32 {
		t.Fatalf("decoded output = %+v, %q, %v", configuration, format, err)
	}
}

func TestImageProfilesAreBoundedBySourceKind(t *testing.T) {
	t.Parallel()
	screenshot, err := Profile(SourceTorrentScreenshot, VariantThumbnail)
	if err != nil {
		t.Fatal(err)
	}
	avatar, err := Profile(SourceAvatar, VariantThumbnail)
	if err != nil {
		t.Fatal(err)
	}
	if screenshot.MaxWidth != 320 || screenshot.MaxHeight != 480 || avatar.MaxWidth != 64 || avatar.MaxHeight != 64 {
		t.Fatalf("unexpected profiles: screenshot=%+v avatar=%+v", screenshot, avatar)
	}
}

func TestVipsTransformerKeepsAnimationOnlyInLargeScreenshot(t *testing.T) {
	t.Parallel()
	binary, err := exec.LookPath("vips")
	if err != nil {
		t.Skip("libvips is not installed in this test environment")
	}
	palette := color.Palette{color.Black, color.White}
	bounds := image.Rect(0, 0, 1000, 12)
	black := image.NewPaletted(bounds, palette)
	white := image.NewPaletted(bounds, palette)
	for i := range white.Pix {
		white.Pix[i] = 1
	}
	var source bytes.Buffer
	if err := gif.EncodeAll(&source, &gif.GIF{
		Image: []*image.Paletted{black, white},
		Delay: []int{8, 16},
	}); err != nil {
		t.Fatal(err)
	}
	transformer, err := NewVipsTransformer(VipsConfig{Binary: binary, TempDir: t.TempDir(), Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	preview, err := transformer.Transform(ctx, SourceTorrentScreenshot, VariantThumbnail, source.Bytes(), ".gif")
	if err != nil || IsAnimatedWebP(preview.Bytes) {
		t.Fatalf("preview must contain only the first frame: %v", err)
	}
	playback, err := transformer.Transform(ctx, SourceTorrentScreenshot, VariantLarge, source.Bytes(), ".gif")
	if err != nil {
		t.Fatal(err)
	}
	transcoded, err := transformer.Transform(ctx, SourceTorrentScreenshot, VariantLarge, playback.Bytes, ".webp")
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []Output{playback, transcoded} {
		frames, err := ScreenshotFrameCount(output.Bytes, "webp", output.Width, output.Height)
		if err != nil || frames != 2 || output.Width != 960 || output.Height > 12 {
			t.Fatalf("playback: %d frames at %dx%d (%v)", frames, output.Width, output.Height, err)
		}
	}
}

func TestVipsTransformerRejectsUnboundedConcurrency(t *testing.T) {
	t.Parallel()
	if _, err := NewVipsTransformer(VipsConfig{TempDir: t.TempDir(), Concurrency: 65}); err == nil {
		t.Fatal("expected unbounded libvips concurrency to be rejected")
	}
}
