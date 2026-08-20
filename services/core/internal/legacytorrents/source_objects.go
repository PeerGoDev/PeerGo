package legacytorrents

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

const maxSourceArchiveEntries = 1_000_000

// sourceObjectRoot is a read-only view of either PtYes's historical
// torrent_dir or a strict ZIP snapshot of that directory. PtYes stores each
// object as <uuid[:2]>/<uuid>.torrent; ZIP files may have one outer
// "torrents/" directory. Resolved host paths remain process-local and are
// never written to migration tables.
type sourceObjectRoot struct {
	resolved       string
	archive        *zip.ReadCloser
	archiveObjects map[uuid.UUID]*zip.File
	archiveAliases map[uuid.UUID]uuid.UUID
}

type sourceObjectReadError struct {
	code string
	err  error
}

func (problem *sourceObjectReadError) Error() string {
	return "read PtYes torrent object: " + problem.code
}

func (problem *sourceObjectReadError) Unwrap() error {
	return problem.err
}

func openSourceObjectRoot(value string) (*sourceObjectRoot, error) {
	if value == "" || !filepath.IsAbs(value) {
		return nil, errors.New("PtYes torrent source must be an absolute directory or ZIP file")
	}
	cleaned := filepath.Clean(value)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return nil, errors.New("resolve PtYes torrent source")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, errors.New("inspect PtYes torrent source")
	}
	if info.IsDir() {
		return &sourceObjectRoot{resolved: resolved}, nil
	}
	if !info.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(resolved), ".zip") {
		return nil, errors.New("PtYes torrent source is not a directory or ZIP file")
	}
	archive, err := zip.OpenReader(resolved)
	if err != nil {
		return nil, errors.New("open PtYes torrent ZIP source")
	}
	root := &sourceObjectRoot{
		resolved: resolved, archive: archive,
		archiveObjects: make(map[uuid.UUID]*zip.File),
	}
	if err := root.indexArchive(); err != nil {
		_ = archive.Close()
		return nil, err
	}
	return root, nil
}

func (root *sourceObjectRoot) close() error {
	if root == nil || root.archive == nil {
		return nil
	}
	err := root.archive.Close()
	root.archive = nil
	return err
}

func (root *sourceObjectRoot) archiveObjectCount() int64 {
	if root == nil || root.archive == nil {
		return 0
	}
	return int64(len(root.archiveObjects))
}

func (root *sourceObjectRoot) indexArchive() error {
	if root == nil || root.archive == nil || len(root.archive.File) == 0 ||
		len(root.archive.File) > maxSourceArchiveEntries {
		return errors.New("PtYes torrent ZIP has an invalid entry count")
	}
	var outerDirectory *bool
	for _, entry := range root.archive.File {
		if entry == nil || !utf8.ValidString(entry.Name) || entry.Name == "" ||
			strings.ContainsRune(entry.Name, '\x00') || strings.Contains(entry.Name, "\\") {
			return errors.New("PtYes torrent ZIP contains an unsafe entry")
		}
		name := strings.TrimSuffix(entry.Name, "/")
		if name == "" || strings.HasPrefix(name, "/") || path.Clean(name) != name {
			return errors.New("PtYes torrent ZIP contains an unsafe path")
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return errors.New("PtYes torrent ZIP contains a symbolic link")
		}
		if entry.FileInfo().IsDir() {
			if !validArchiveDirectory(name) {
				return errors.New("PtYes torrent ZIP contains an unexpected directory")
			}
			continue
		}
		if !entry.Mode().IsRegular() || entry.UncompressedSize64 < 1 ||
			entry.UncompressedSize64 > uint64(torrents.MaxMetainfoBytes) {
			if validBundledImageObject(name, entry) {
				continue
			}
			return errors.New("PtYes torrent ZIP contains an invalid object file")
		}
		if strings.HasPrefix(name, "uploads/") {
			if !validBundledImageObject(name, entry) {
				return errors.New("PtYes combined ZIP contains an invalid image object")
			}
			continue
		}
		publicID, hasOuterDirectory, err := parseArchiveObjectName(name)
		if err != nil {
			return err
		}
		if outerDirectory == nil {
			value := hasOuterDirectory
			outerDirectory = &value
		} else if *outerDirectory != hasOuterDirectory {
			return errors.New("PtYes torrent ZIP mixes incompatible root layouts")
		}
		if _, exists := root.archiveObjects[publicID]; exists {
			return errors.New("PtYes torrent ZIP contains a duplicate object identity")
		}
		root.archiveObjects[publicID] = entry
	}
	if len(root.archiveObjects) == 0 {
		return errors.New("PtYes torrent ZIP contains no torrent objects")
	}
	return nil
}

func validArchiveDirectory(name string) bool {
	parts := strings.Split(name, "/")
	if len(parts) == 1 {
		return parts[0] == "torrents" || parts[0] == "uploads" || validLowerHexPrefix(parts[0])
	}
	if len(parts) == 2 {
		return parts[0] == "torrents" && validLowerHexPrefix(parts[1]) ||
			parts[0] == "uploads" && parts[1] == "images"
	}
	return len(parts) == 3 && parts[0] == "uploads" && parts[1] == "images" &&
		validLowerHexPrefix(parts[2])
}

// validBundledImageObject lets formal cutover use one immutable asset ZIP
// containing both torrents/ and uploads/images/. Torrent validation still
// rejects every unrelated namespace and never reads the image bytes.
func validBundledImageObject(name string, entry *zip.File) bool {
	if entry == nil || entry.UncompressedSize64 < 1 || entry.UncompressedSize64 > 32<<20 {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "uploads" || parts[1] != "images" ||
		!validLowerHexPrefix(parts[2]) {
		return false
	}
	extension := path.Ext(parts[3])
	switch extension {
	case ".jpg", ".png", ".webp", ".gif":
	default:
		return false
	}
	identifier := strings.TrimSuffix(parts[3], extension)
	parsed, err := uuid.Parse(identifier)
	return err == nil && parsed.String() == identifier && identifier[:2] == parts[2]
}

func parseArchiveObjectName(name string) (uuid.UUID, bool, error) {
	parts := strings.Split(name, "/")
	hasOuterDirectory := false
	if len(parts) == 3 && parts[0] == "torrents" {
		hasOuterDirectory = true
		parts = parts[1:]
	}
	if len(parts) != 2 || !validLowerHexPrefix(parts[0]) ||
		!strings.HasSuffix(parts[1], ".torrent") {
		return uuid.Nil, false, errors.New("PtYes torrent ZIP contains an unexpected file")
	}
	identifier := strings.TrimSuffix(parts[1], ".torrent")
	publicID, err := uuid.Parse(identifier)
	if err != nil || publicID.String() != identifier || identifier[:2] != parts[0] {
		return uuid.Nil, false, errors.New("PtYes torrent ZIP contains an invalid object identity")
	}
	return publicID, hasOuterDirectory, nil
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

func (root *sourceObjectRoot) read(publicID uuid.UUID) ([]byte, error) {
	if root == nil || root.resolved == "" || publicID == uuid.Nil {
		return nil, &sourceObjectReadError{code: "invalid_object_identity", err: errInvalidSourceTorrent}
	}
	if root.archive != nil {
		return root.readArchive(publicID)
	}
	return root.readDirectory(publicID)
}

func (root *sourceObjectRoot) readArchive(publicID uuid.UUID) ([]byte, error) {
	resolvedID := publicID
	if alias, exists := root.archiveAliases[publicID]; exists {
		resolvedID = alias
	}
	entry, exists := root.archiveObjects[resolvedID]
	if !exists {
		return nil, &sourceObjectReadError{code: "object_missing", err: os.ErrNotExist}
	}
	return readArchiveEntry(entry)
}

func readArchiveEntry(entry *zip.File) ([]byte, error) {
	if entry == nil {
		return nil, &sourceObjectReadError{code: "object_missing", err: os.ErrNotExist}
	}
	reader, err := entry.Open()
	if err != nil {
		return nil, &sourceObjectReadError{code: "object_read_failed", err: err}
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, torrents.MaxMetainfoBytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, &sourceObjectReadError{code: "object_read_failed", err: readErr}
	}
	if closeErr != nil {
		return nil, &sourceObjectReadError{code: "object_read_failed", err: closeErr}
	}
	if len(raw) < 1 || len(raw) > torrents.MaxMetainfoBytes ||
		uint64(len(raw)) != entry.UncompressedSize64 {
		return nil, &sourceObjectReadError{code: "object_changed_during_read", err: errInvalidSourceTorrent}
	}
	return raw, nil
}

func (root *sourceObjectRoot) readDirectory(publicID uuid.UUID) ([]byte, error) {
	name := publicID.String()
	directory := filepath.Join(root.resolved, name[:2])
	objectPath := filepath.Join(directory, name+".torrent")

	directoryInfo, err := os.Lstat(directory)
	if err != nil {
		return nil, sourceObjectFilesystemError(err)
	}
	if !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 {
		return nil, &sourceObjectReadError{code: "unsafe_object_path", err: errInvalidSourceTorrent}
	}
	objectInfo, err := os.Lstat(objectPath)
	if err != nil {
		return nil, sourceObjectFilesystemError(err)
	}
	if !objectInfo.Mode().IsRegular() || objectInfo.Mode()&os.ModeSymlink != 0 ||
		objectInfo.Size() < 1 || objectInfo.Size() > torrents.MaxMetainfoBytes {
		return nil, &sourceObjectReadError{code: "invalid_object_file", err: errInvalidSourceTorrent}
	}
	resolvedObject, err := filepath.EvalSymlinks(objectPath)
	if err != nil || resolvedObject != objectPath {
		return nil, &sourceObjectReadError{code: "unsafe_object_path", err: errInvalidSourceTorrent}
	}

	file, err := os.Open(objectPath)
	if err != nil {
		return nil, sourceObjectFilesystemError(err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(objectInfo, openedInfo) || !openedInfo.Mode().IsRegular() {
		return nil, &sourceObjectReadError{code: "object_changed_during_read", err: errInvalidSourceTorrent}
	}
	raw, err := io.ReadAll(io.LimitReader(file, torrents.MaxMetainfoBytes+1))
	if err != nil {
		return nil, &sourceObjectReadError{code: "object_read_failed", err: err}
	}
	if len(raw) < 1 || len(raw) > torrents.MaxMetainfoBytes || int64(len(raw)) != openedInfo.Size() {
		return nil, &sourceObjectReadError{code: "object_changed_during_read", err: errInvalidSourceTorrent}
	}
	return raw, nil
}

func sourceObjectFilesystemError(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return &sourceObjectReadError{code: "object_missing", err: err}
	}
	if errors.Is(err, os.ErrPermission) {
		return &sourceObjectReadError{code: "object_permission_denied", err: err}
	}
	return &sourceObjectReadError{code: "object_stat_failed", err: err}
}

func sourceObjectErrorCode(err error) string {
	var problem *sourceObjectReadError
	if errors.As(err, &problem) && problem.code != "" {
		return problem.code
	}
	if code, ok := torrents.ValidationCodeOf(err); ok {
		return string(code)
	}
	return "object_validation_failed"
}

func reconcileSourceMetainfo(
	source sourceTorrent,
	manifest sourceFileManifest,
	parsed torrents.ParsedMetainfo,
) error {
	expectedHash, err := source.parsedInfoHash()
	if err != nil || parsed.InfoHashV1 != expectedHash {
		return sourceTorrentError(source.LegacyID, "object_info_hash_mismatch")
	}
	if !parsed.Private {
		return sourceTorrentError(source.LegacyID, "object_private_marker_missing")
	}
	if parsed.TotalSizeBytes != source.SizeBytes {
		return sourceTorrentError(source.LegacyID, "object_total_size_mismatch")
	}
	// Eleven audited PtYes rows have no database-side file manifest. Their raw
	// immutable object is authoritative; every other row must match the old
	// ordered path/size list exactly before it may become live data.
	if len(manifest.Files) == 0 {
		return nil
	}
	if len(parsed.Files) != len(manifest.Files) {
		return sourceTorrentError(source.LegacyID, "object_file_count_mismatch")
	}
	for index, file := range parsed.Files {
		expected := manifest.Files[index]
		if file.Index != index || file.DisplayPath != expected.Path || file.LengthBytes != expected.Size {
			return sourceTorrentError(source.LegacyID, "object_file_manifest_mismatch")
		}
	}
	return nil
}

func wrapSourceObjectError(legacyID int64, err error) error {
	return sourceTorrentError(legacyID, sourceObjectErrorCode(err))
}
