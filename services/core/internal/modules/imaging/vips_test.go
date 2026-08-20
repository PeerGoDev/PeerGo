package imaging

import (
	"bytes"
	"context"
	"image"
	"image/color"
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

func TestVipsTransformerRejectsUnboundedConcurrency(t *testing.T) {
	t.Parallel()
	if _, err := NewVipsTransformer(VipsConfig{TempDir: t.TempDir(), Concurrency: 65}); err == nil {
		t.Fatal("expected unbounded libvips concurrency to be rejected")
	}
}
