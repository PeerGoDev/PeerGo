package legacytorrents

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

const (
	torrentExclusionVersion  = "peergo-ptyes-torrent-exclusions-v1"
	torrentExclusionColumns  = "legacy_id\tpublic_id\tinfo_hash_v1\tsize_bytes\treason"
	torrentExclusionReason   = "object_missing"
	maxTorrentExclusionBytes = 1 << 20
	excludedObjectDomain     = "peergo:migration:ptyes-missing-torrent-object:v1\x00"
)

type torrentExclusion struct {
	legacyID int64
	publicID uuid.UUID
	infoHash torrents.InfoHashV1
	size     int64
}

// TorrentExclusionManifest is an explicit, snapshot-bound operator decision.
// Its zero value excludes nothing. A populated value can skip only a source
// object that is physically missing; it cannot waive parse, hash, size, path,
// privacy, or database-manifest failures.
type TorrentExclusionManifest struct {
	snapshotSHA256 [sha256.Size]byte
	contentSHA256  [sha256.Size]byte
	entries        map[int64]torrentExclusion
}

func (manifest TorrentExclusionManifest) Len() int {
	return len(manifest.entries)
}

func (manifest TorrentExclusionManifest) ContentSHA256() [sha256.Size]byte {
	return manifest.contentSHA256
}

// LoadTorrentExclusionManifest reads a deliberately small, strict TSV file.
// Requiring an absolute regular non-symlink path prevents an operator command
// from silently switching policy files between validation and import.
func LoadTorrentExclusionManifest(
	value string,
	expectedSnapshot [sha256.Size]byte,
) (TorrentExclusionManifest, error) {
	if expectedSnapshot == ([sha256.Size]byte{}) {
		return TorrentExclusionManifest{}, errors.New("torrent exclusion snapshot digest is missing")
	}
	if value == "" || !filepath.IsAbs(value) {
		return TorrentExclusionManifest{}, errors.New("torrent exclusion manifest path must be absolute")
	}
	cleaned := filepath.Clean(value)
	info, err := os.Lstat(cleaned)
	if err != nil {
		return TorrentExclusionManifest{}, errors.New("inspect torrent exclusion manifest")
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Size() < 1 || info.Size() > maxTorrentExclusionBytes {
		return TorrentExclusionManifest{}, errors.New("torrent exclusion manifest is not a bounded regular file")
	}
	file, err := os.Open(cleaned)
	if err != nil {
		return TorrentExclusionManifest{}, errors.New("open torrent exclusion manifest")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxTorrentExclusionBytes+1))
	if err != nil || len(raw) < 1 || len(raw) > maxTorrentExclusionBytes || int64(len(raw)) != info.Size() {
		return TorrentExclusionManifest{}, errors.New("read torrent exclusion manifest")
	}
	return parseTorrentExclusionManifest(raw, expectedSnapshot)
}

func parseTorrentExclusionManifest(
	raw []byte,
	expectedSnapshot [sha256.Size]byte,
) (TorrentExclusionManifest, error) {
	if strings.ContainsRune(string(raw), '\r') {
		return TorrentExclusionManifest{}, errors.New("torrent exclusion manifest must use LF line endings")
	}
	text := strings.TrimSuffix(string(raw), "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 4 || lines[0] != torrentExclusionVersion || lines[2] != torrentExclusionColumns {
		return TorrentExclusionManifest{}, errors.New("torrent exclusion manifest header is invalid")
	}
	snapshotText, ok := strings.CutPrefix(lines[1], "snapshot_sha256\t")
	if !ok || len(snapshotText) != sha256.Size*2 || strings.ToLower(snapshotText) != snapshotText {
		return TorrentExclusionManifest{}, errors.New("torrent exclusion snapshot digest is invalid")
	}
	snapshotBytes, err := hex.DecodeString(snapshotText)
	if err != nil || len(snapshotBytes) != sha256.Size {
		return TorrentExclusionManifest{}, errors.New("torrent exclusion snapshot digest is invalid")
	}
	var snapshot [sha256.Size]byte
	copy(snapshot[:], snapshotBytes)
	if snapshot != expectedSnapshot {
		return TorrentExclusionManifest{}, errors.New("torrent exclusion manifest belongs to a different database snapshot")
	}
	result := TorrentExclusionManifest{
		snapshotSHA256: snapshot,
		contentSHA256:  sha256.Sum256(raw),
		entries:        make(map[int64]torrentExclusion, len(lines)-3),
	}
	var previousID int64
	for lineIndex, line := range lines[3:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 5 || fields[4] != torrentExclusionReason {
			return TorrentExclusionManifest{}, fmt.Errorf("torrent exclusion manifest row %d is invalid", lineIndex+1)
		}
		legacyID, idErr := strconv.ParseInt(fields[0], 10, 64)
		publicID, uuidErr := uuid.Parse(fields[1])
		infoHash, hashErr := torrents.ParseInfoHashV1Hex(fields[2])
		size, sizeErr := strconv.ParseInt(fields[3], 10, 64)
		if idErr != nil || legacyID <= previousID || uuidErr != nil || publicID == uuid.Nil ||
			publicID.String() != fields[1] || hashErr != nil || infoHash.Hex() != fields[2] ||
			sizeErr != nil || size < 1 {
			return TorrentExclusionManifest{}, fmt.Errorf("torrent exclusion manifest row %d is invalid", lineIndex+1)
		}
		previousID = legacyID
		result.entries[legacyID] = torrentExclusion{
			legacyID: legacyID, publicID: publicID, infoHash: infoHash, size: size,
		}
	}
	return result, nil
}

func (manifest TorrentExclusionManifest) match(source sourceTorrent) bool {
	exclusion, exists := manifest.entries[source.LegacyID]
	if !exists {
		return false
	}
	publicID, publicIDErr := source.publicID()
	infoHash, hashErr := source.parsedInfoHash()
	return publicIDErr == nil && hashErr == nil && exclusion.publicID == publicID &&
		exclusion.infoHash == infoHash && exclusion.size == source.SizeBytes
}

func (manifest TorrentExclusionManifest) objectFingerprint(source sourceTorrent) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	if !manifest.match(source) || manifest.contentSHA256 == ([sha256.Size]byte{}) {
		return result, errInvalidSourceTorrent
	}
	publicID, _ := source.publicID()
	infoHash, _ := source.parsedInfoHash()
	digest := sha256.New()
	_, _ = digest.Write([]byte(excludedObjectDomain))
	_, _ = digest.Write(manifest.snapshotSHA256[:])
	_, _ = digest.Write(manifest.contentSHA256[:])
	writeInt64(digest, source.LegacyID)
	_, _ = digest.Write(publicID[:])
	_, _ = digest.Write(infoHash[:])
	writeInt64(digest, source.SizeBytes)
	copy(result[:], digest.Sum(nil))
	return result, nil
}
