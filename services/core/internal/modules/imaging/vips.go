package imaging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/peergo/peergo/services/core/internal/contracts/objectstorage"
)

type Transformer interface {
	Probe(context.Context) error
	Transform(context.Context, SourceKind, Variant, []byte, string) (Output, error)
}

type VipsConfig struct {
	Binary      string
	TempDir     string
	Timeout     time.Duration
	Concurrency int
}

// VipsTransformer is the only native image boundary. Keeping libvips behind a
// subprocess avoids cgo in Core while still allowing deterministic fakes in
// domain tests. The saver strips metadata; thumbnail rotates EXIF orientation
// upright by default and never upscales the source.
type VipsTransformer struct {
	binary      string
	tempDir     string
	timeout     time.Duration
	concurrency int
}

func NewVipsTransformer(config VipsConfig) (*VipsTransformer, error) {
	config.Binary = strings.TrimSpace(config.Binary)
	if config.Binary == "" {
		config.Binary = "vips"
	}
	if config.TempDir == "" || !filepath.IsAbs(config.TempDir) {
		return nil, errors.New("image derivative temporary directory must be absolute")
	}
	info, err := os.Stat(config.TempDir)
	if err != nil || !info.IsDir() {
		return nil, errors.New("image derivative temporary directory is unavailable")
	}
	if config.Timeout == 0 {
		config.Timeout = 2 * time.Minute
	}
	if config.Timeout < time.Second || config.Timeout > 10*time.Minute {
		return nil, errors.New("image derivative libvips timeout is outside the safe range")
	}
	if config.Concurrency < 0 || config.Concurrency > 64 {
		return nil, errors.New("image derivative libvips concurrency is outside the safe range")
	}
	return &VipsTransformer{
		binary: config.Binary, tempDir: filepath.Clean(config.TempDir), timeout: config.Timeout,
		concurrency: config.Concurrency,
	}, nil
}

func (transformer *VipsTransformer) Probe(ctx context.Context) error {
	if transformer == nil || ctx == nil {
		return ErrInput
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, transformer.binary, "--version").CombinedOutput()
	if err != nil || !strings.Contains(strings.ToLower(string(output)), "vips") {
		return errors.New("libvips command is unavailable or incompatible")
	}
	return nil
}

func (transformer *VipsTransformer) Transform(ctx context.Context, kind SourceKind, variant Variant, raw []byte, extension string) (Output, error) {
	if transformer == nil || ctx == nil || len(raw) < 1 || len(raw) > 32<<20 {
		return Output{}, ErrInput
	}
	profile, err := Profile(kind, variant)
	if err != nil {
		return Output{}, err
	}
	extension = canonicalSourceExtension(extension)
	if extension == "" {
		return Output{}, ErrInput
	}
	input, err := os.CreateTemp(transformer.tempDir, "peergo-image-source-*"+extension)
	if err != nil {
		return Output{}, errors.New("create private image derivative input")
	}
	inputName := input.Name()
	outputName := inputName + ".webp"
	defer func() {
		_ = os.Remove(inputName)
		_ = os.Remove(outputName)
	}()
	if err := input.Chmod(0o600); err != nil {
		_ = input.Close()
		return Output{}, errors.New("protect private image derivative input")
	}
	if _, err := input.Write(raw); err != nil {
		_ = input.Close()
		return Output{}, errors.New("write private image derivative input")
	}
	if err := input.Close(); err != nil {
		return Output{}, errors.New("close private image derivative input")
	}
	transformCtx, cancel := context.WithTimeout(ctx, transformer.timeout)
	defer cancel()
	outputOption := fmt.Sprintf("%s[Q=%d,effort=4,keep=none]", outputName, profile.Quality)
	arguments := make([]string, 0, 10)
	if transformer.concurrency > 0 {
		arguments = append(arguments, fmt.Sprintf("--vips-concurrency=%d", transformer.concurrency))
	}
	arguments = append(arguments, "thumbnail", inputName, outputOption,
		fmt.Sprintf("%d", profile.MaxWidth), "--height", fmt.Sprintf("%d", profile.MaxHeight),
		"--size", "down", "--fail-on", "warning",
	)
	command := exec.CommandContext(transformCtx, transformer.binary, arguments...)
	var diagnostics limitedDiagnostics
	command.Stdout, command.Stderr = &diagnostics, &diagnostics
	if err := command.Run(); err != nil {
		if errors.Is(transformCtx.Err(), context.DeadlineExceeded) {
			return Output{}, errors.New("libvips image derivative transform timed out")
		}
		return Output{}, fmt.Errorf("libvips image derivative transform failed: %s", diagnostics.String())
	}
	file, err := os.Open(outputName)
	if err != nil {
		return Output{}, errors.New("open libvips image derivative output")
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, MaxOutputBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(contents) < 1 || len(contents) > MaxOutputBytes {
		return Output{}, errors.New("read libvips image derivative output")
	}
	configuration, format, err := image.DecodeConfig(bytes.NewReader(contents))
	if err != nil || format != "webp" || configuration.Width < 1 || configuration.Height < 1 ||
		configuration.Width > profile.MaxWidth || configuration.Height > profile.MaxHeight {
		return Output{}, errors.New("libvips produced an invalid WebP derivative")
	}
	digest := objectstorage.SHA256(sha256.Sum256(contents))
	return Output{
		Descriptor: objectstorage.Descriptor{SHA256: digest, ByteLength: int64(len(contents))},
		Width:      configuration.Width, Height: configuration.Height, Bytes: contents,
	}, nil
}

func canonicalSourceExtension(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ".jpg", ".jpeg":
		return ".jpg"
	case ".png":
		return ".png"
	case ".webp":
		return ".webp"
	case ".gif":
		// Runtime variants are intentionally static previews. The immutable GIF
		// remains available from the original endpoint and is never flattened in
		// place or deleted after this job succeeds.
		return ".gif"
	default:
		return ""
	}
}

type limitedDiagnostics struct{ buffer bytes.Buffer }

func (diagnostics *limitedDiagnostics) Write(contents []byte) (int, error) {
	original := len(contents)
	remaining := 1024 - diagnostics.buffer.Len()
	if remaining > 0 {
		if len(contents) > remaining {
			contents = contents[:remaining]
		}
		_, _ = diagnostics.buffer.Write(contents)
	}
	return original, nil
}

func (diagnostics *limitedDiagnostics) String() string {
	value := strings.TrimSpace(diagnostics.buffer.String())
	if value == "" {
		return "no diagnostics"
	}
	return value
}
