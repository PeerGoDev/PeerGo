// Package legacymedia imports the finite PtYes image snapshot into the
// existing torrent screenshot object model. It deliberately excludes avatars,
// social posts and arbitrary uploads from the cutover boundary.
package legacymedia

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	maxArchiveEntries      = 1_000_000
	maxSourceImageBytes    = 32 << 20
	maxBundledTorrentBytes = 16 << 20
)

type ArchiveInspection struct {
	SHA256     [sha256.Size]byte
	ByteLength int64
	ImageCount int64
}

type sourceImageEntry struct {
	file      *zip.File
	extension string
}

// SourceArchive is a read-only indexed view over either uploads.zip or one
// combined assets.zip. Paths are canonical historical URL paths beginning
// with /uploads/images/; host paths never leave this adapter.
type SourceArchive struct {
	resolved   string
	reader     *zip.ReadCloser
	images     map[string]sourceImageEntry
	inspection ArchiveInspection
}

func OpenSourceArchive(value string) (*SourceArchive, error) {
	if value == "" || !filepath.IsAbs(value) || !strings.EqualFold(filepath.Ext(value), ".zip") {
		return nil, errors.New("PtYes image source must be an absolute ZIP file")
	}
	cleaned := filepath.Clean(value)
	before, err := os.Lstat(cleaned)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 || before.Size() < 1 {
		return nil, errors.New("PtYes image ZIP must be a non-symlink regular file")
	}
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return nil, errors.New("resolve PtYes image ZIP path")
	}
	reader, err := zip.OpenReader(resolved)
	if err != nil {
		return nil, errors.New("open PtYes image ZIP")
	}
	archive := &SourceArchive{
		resolved: resolved,
		reader:   reader,
		images:   make(map[string]sourceImageEntry),
	}
	if err := archive.index(); err != nil {
		_ = reader.Close()
		return nil, err
	}
	digest, err := hashRegularFile(resolved)
	if err != nil {
		_ = reader.Close()
		return nil, err
	}
	after, err := os.Lstat(resolved)
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() {
		_ = reader.Close()
		return nil, errors.New("PtYes image ZIP changed during inspection")
	}
	archive.inspection = ArchiveInspection{
		SHA256: digest, ByteLength: before.Size(), ImageCount: int64(len(archive.images)),
	}
	return archive, nil
}

func InspectSourceArchive(value string) (ArchiveInspection, error) {
	archive, err := OpenSourceArchive(value)
	if err != nil {
		return ArchiveInspection{}, err
	}
	defer archive.Close()
	return archive.inspection, nil
}

func (archive *SourceArchive) Inspection() ArchiveInspection {
	if archive == nil {
		return ArchiveInspection{}
	}
	return archive.inspection
}

func (archive *SourceArchive) Close() error {
	if archive == nil || archive.reader == nil {
		return nil
	}
	err := archive.reader.Close()
	archive.reader = nil
	return err
}

func (archive *SourceArchive) Has(legacyPath string) bool {
	if archive == nil {
		return false
	}
	_, exists := archive.images[legacyPath]
	return exists
}

func (archive *SourceArchive) Read(ctx context.Context, legacyPath string) ([]byte, string, error) {
	if archive == nil || archive.reader == nil || ctx == nil {
		return nil, "", errors.New("PtYes image archive is unavailable")
	}
	entry, exists := archive.images[legacyPath]
	if !exists {
		return nil, "", os.ErrNotExist
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	opened, err := entry.file.Open()
	if err != nil {
		return nil, "", errors.New("open PtYes image object")
	}
	raw, readErr := io.ReadAll(io.LimitReader(opened, maxSourceImageBytes+1))
	closeErr := opened.Close()
	if readErr != nil || closeErr != nil || len(raw) < 1 || len(raw) > maxSourceImageBytes ||
		uint64(len(raw)) != entry.file.UncompressedSize64 {
		return nil, "", errors.New("read PtYes image object")
	}
	return raw, entry.extension, nil
}

func (archive *SourceArchive) index() error {
	if archive == nil || archive.reader == nil || len(archive.reader.File) == 0 ||
		len(archive.reader.File) > maxArchiveEntries {
		return errors.New("PtYes image ZIP has an invalid entry count")
	}
	for _, entry := range archive.reader.File {
		if err := validateArchiveEntry(entry); err != nil {
			return err
		}
		name := strings.TrimSuffix(entry.Name, "/")
		if entry.FileInfo().IsDir() {
			if !validBundleDirectory(name) {
				return errors.New("PtYes image ZIP contains an unexpected directory")
			}
			continue
		}
		legacyPath, extension, image := parseImageObjectName(name)
		if image {
			if entry.UncompressedSize64 < 1 || entry.UncompressedSize64 > maxSourceImageBytes {
				return errors.New("PtYes image ZIP contains an invalid image size")
			}
			if _, duplicate := archive.images[legacyPath]; duplicate {
				return errors.New("PtYes image ZIP contains a duplicate image path")
			}
			archive.images[legacyPath] = sourceImageEntry{file: entry, extension: extension}
			continue
		}
		if !validBundledTorrentObject(name, entry.UncompressedSize64) {
			return errors.New("PtYes image ZIP contains an unexpected file")
		}
	}
	if len(archive.images) == 0 {
		return errors.New("PtYes image ZIP contains no image objects")
	}
	return nil
}

func validateArchiveEntry(entry *zip.File) error {
	if entry == nil || !utf8.ValidString(entry.Name) || entry.Name == "" ||
		strings.ContainsRune(entry.Name, '\x00') || strings.Contains(entry.Name, "\\") {
		return errors.New("PtYes image ZIP contains an unsafe entry")
	}
	name := strings.TrimSuffix(entry.Name, "/")
	if name == "" || strings.HasPrefix(name, "/") || path.Clean(name) != name ||
		entry.Mode()&os.ModeSymlink != 0 {
		return errors.New("PtYes image ZIP contains an unsafe path")
	}
	if !entry.FileInfo().IsDir() && !entry.Mode().IsRegular() {
		return errors.New("PtYes image ZIP contains a non-regular object")
	}
	return nil
}

func validBundleDirectory(name string) bool {
	parts := strings.Split(name, "/")
	switch len(parts) {
	case 1:
		return parts[0] == "uploads" || parts[0] == "torrents" || validLowerHexPrefix(parts[0])
	case 2:
		return parts[0] == "uploads" && parts[1] == "images" ||
			parts[0] == "torrents" && validLowerHexPrefix(parts[1])
	case 3:
		return parts[0] == "uploads" && parts[1] == "images" && validLowerHexPrefix(parts[2])
	default:
		return false
	}
}

func parseImageObjectName(name string) (string, string, bool) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "uploads" || parts[1] != "images" ||
		!validLowerHexPrefix(parts[2]) {
		return "", "", false
	}
	extension := path.Ext(parts[3])
	switch extension {
	case ".jpg", ".png", ".webp", ".gif":
	default:
		return "", "", false
	}
	identifier := strings.TrimSuffix(parts[3], extension)
	parsed, err := uuid.Parse(identifier)
	if err != nil || parsed.String() != identifier || identifier[:2] != parts[2] {
		return "", "", false
	}
	return "/" + name, extension, true
}

func validBundledTorrentObject(name string, size uint64) bool {
	if size < 1 || size > maxBundledTorrentBytes {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) == 3 && parts[0] == "torrents" {
		parts = parts[1:]
	}
	if len(parts) != 2 || !validLowerHexPrefix(parts[0]) || path.Ext(parts[1]) != ".torrent" {
		return false
	}
	identifier := strings.TrimSuffix(parts[1], ".torrent")
	parsed, err := uuid.Parse(identifier)
	return err == nil && parsed.String() == identifier && identifier[:2] == parts[0]
}

func validLowerHexPrefix(value string) bool {
	if len(value) != 2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func hashRegularFile(name string) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	file, err := os.Open(name)
	if err != nil {
		return result, errors.New("open PtYes image ZIP for hashing")
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return result, fmt.Errorf("hash PtYes image ZIP")
	}
	copy(result[:], hasher.Sum(nil))
	return result, nil
}
