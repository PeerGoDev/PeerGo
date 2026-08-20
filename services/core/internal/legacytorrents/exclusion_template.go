package legacytorrents

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TorrentExclusionCandidateResult struct {
	MissingObjects      int64
	RecoveredObjects    int64
	UnreferencedObjects int64
	ContentSHA256       [sha256.Size]byte
}

// WriteTorrentExclusionCandidate creates a review input, never an approval.
// It refuses to overwrite a file and includes the exact immutable database
// identity of only those rows still physically missing after content recovery.
func WriteTorrentExclusionCandidate(
	ctx context.Context,
	source *pgxpool.Pool,
	torrentRoot string,
	snapshot [sha256.Size]byte,
	outputPath string,
) (TorrentExclusionCandidateResult, error) {
	if source == nil || strings.TrimSpace(torrentRoot) == "" ||
		snapshot == ([sha256.Size]byte{}) || !filepath.IsAbs(outputPath) {
		return TorrentExclusionCandidateResult{}, errors.New("torrent exclusion candidate configuration is invalid")
	}
	root, err := openSourceObjectRoot(strings.TrimSpace(torrentRoot))
	if err != nil {
		return TorrentExclusionCandidateResult{}, err
	}
	defer func() { _ = root.close() }()
	recovery, err := prepareSourceObjectRecovery(ctx, source, root)
	if err != nil {
		return TorrentExclusionCandidateResult{}, err
	}
	expected, err := queryExpectedSourceObjects(ctx, source)
	if err != nil {
		return TorrentExclusionCandidateResult{}, err
	}
	exclusions := make([]torrentExclusion, 0)
	for _, object := range expected {
		if _, readErr := root.read(object.publicID); readErr == nil {
			continue
		} else if sourceObjectErrorCode(readErr) != "object_missing" {
			return TorrentExclusionCandidateResult{}, readErr
		}
		exclusions = append(exclusions, torrentExclusion{
			legacyID: object.legacyID, publicID: object.publicID,
			infoHash: object.infoHash, size: object.size,
		})
	}
	if len(exclusions) == 0 {
		return TorrentExclusionCandidateResult{}, errors.New("no physically missing torrent objects require an exclusion candidate")
	}
	raw := renderTorrentExclusionManifest(snapshot, exclusions)
	if err := writeNewPrivateFile(outputPath, raw); err != nil {
		return TorrentExclusionCandidateResult{}, err
	}
	return TorrentExclusionCandidateResult{
		MissingObjects: int64(len(exclusions)), RecoveredObjects: recovery.RecoveredObjects,
		UnreferencedObjects: recovery.UnreferencedObjects, ContentSHA256: sha256.Sum256(raw),
	}, nil
}

func renderTorrentExclusionManifest(
	snapshot [sha256.Size]byte,
	exclusions []torrentExclusion,
) []byte {
	var output strings.Builder
	_, _ = fmt.Fprintf(&output, "%s\nsnapshot_sha256\t%x\n%s\n", torrentExclusionVersion, snapshot, torrentExclusionColumns)
	for _, exclusion := range exclusions {
		_, _ = fmt.Fprintf(
			&output, "%d\t%s\t%s\t%d\t%s\n",
			exclusion.legacyID, exclusion.publicID, exclusion.infoHash.Hex(), exclusion.size,
			torrentExclusionReason,
		)
	}
	return []byte(output.String())
}

func writeNewPrivateFile(value string, raw []byte) error {
	cleaned := filepath.Clean(value)
	parent := filepath.Dir(cleaned)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return errors.New("torrent exclusion candidate parent must be an existing resolvable directory")
	}
	target := filepath.Join(resolvedParent, filepath.Base(cleaned))
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("create torrent exclusion candidate without overwrite")
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = os.Remove(target)
		}
	}()
	if _, err := file.Write(raw); err != nil {
		return errors.New("write torrent exclusion candidate")
	}
	if err := file.Sync(); err != nil {
		return errors.New("sync torrent exclusion candidate")
	}
	if err := file.Close(); err != nil {
		return errors.New("close torrent exclusion candidate")
	}
	directory, err := os.Open(resolvedParent)
	if err != nil {
		return errors.New("open torrent exclusion candidate directory")
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil || closeErr != nil {
		return errors.New("sync torrent exclusion candidate directory")
	}
	complete = true
	return nil
}
