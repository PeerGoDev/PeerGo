package legacytorrents

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/torrents"
)

const (
	maxArchiveRecoveryCandidates = 10_000
	maxArchiveRecoveryBytes      = uint64(2 << 30)
)

// expectedSourceObject is the minimum database identity needed to recover a
// renamed ZIP member. The raw metainfo remains authoritative for its parsed
// file tree; the SQL values are used only as exact matching constraints.
type expectedSourceObject struct {
	legacyID int64
	publicID uuid.UUID
	infoHash torrents.InfoHashV1
	size     int64
}

type sourceObjectIdentity struct {
	infoHash torrents.InfoHashV1
	size     int64
}

type sourceObjectRecoveryResult struct {
	ArchiveObjects      int64
	DirectObjects       int64
	UnreferencedObjects int64
	MissingObjects      int64
	RecoveredObjects    int64
	AmbiguousObjects    int64
	RecoveredLegacyIDs  []int64
}

// prepareSourceObjectRecovery handles a common finite-snapshot mismatch: the
// database UUID changed while an otherwise identical immutable .torrent still
// exists under an older UUID in the ZIP. Recovery is accepted only when one
// missing SQL row and one unreferenced ZIP object have the same exact v1
// infohash and parsed total size. Ambiguity stays blocking.
func prepareSourceObjectRecovery(
	ctx context.Context,
	source *pgxpool.Pool,
	root *sourceObjectRoot,
) (sourceObjectRecoveryResult, error) {
	if source == nil || root == nil {
		return sourceObjectRecoveryResult{}, errors.New("PtYes source object recovery configuration is invalid")
	}
	expected, err := queryExpectedSourceObjects(ctx, source)
	if err != nil {
		return sourceObjectRecoveryResult{}, err
	}
	return root.resolveArchiveObjects(expected)
}

func queryExpectedSourceObjects(ctx context.Context, source *pgxpool.Pool) ([]expectedSourceObject, error) {
	rows, err := source.Query(ctx, `
SELECT id::bigint, uuid, info_hash, size
FROM torrents
ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes torrent object identities: %w", err)
	}
	defer rows.Close()
	result := make([]expectedSourceObject, 0)
	for rows.Next() {
		var legacyID int64
		var publicIDText, infoHashText string
		var size int64
		if err := rows.Scan(&legacyID, &publicIDText, &infoHashText, &size); err != nil {
			return nil, fmt.Errorf("scan PtYes torrent object identity: %w", err)
		}
		publicID, publicIDErr := uuid.Parse(publicIDText)
		infoHash, hashErr := torrents.ParseInfoHashV1Hex(infoHashText)
		if legacyID < 1 || publicIDErr != nil || publicID == uuid.Nil ||
			publicID.String() != publicIDText || hashErr != nil || size < 1 {
			return nil, errors.New("PtYes torrent object identity is invalid")
		}
		result = append(result, expectedSourceObject{
			legacyID: legacyID, publicID: publicID, infoHash: infoHash, size: size,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read PtYes torrent object identities: %w", err)
	}
	return result, nil
}

func (root *sourceObjectRoot) resolveArchiveObjects(
	expected []expectedSourceObject,
) (sourceObjectRecoveryResult, error) {
	result := sourceObjectRecoveryResult{}
	if root == nil || root.archive == nil {
		return result, nil
	}
	result.ArchiveObjects = int64(len(root.archiveObjects))
	expectedIDs := make(map[uuid.UUID]struct{}, len(expected))
	missingByIdentity := make(map[sourceObjectIdentity][]expectedSourceObject)
	for _, object := range expected {
		if object.legacyID < 1 || object.publicID == uuid.Nil || object.size < 1 {
			return sourceObjectRecoveryResult{}, errors.New("PtYes expected torrent object identity is invalid")
		}
		if _, duplicate := expectedIDs[object.publicID]; duplicate {
			return sourceObjectRecoveryResult{}, errors.New("PtYes database contains a duplicate torrent object UUID")
		}
		expectedIDs[object.publicID] = struct{}{}
		if _, exists := root.archiveObjects[object.publicID]; exists {
			result.DirectObjects++
			continue
		}
		result.MissingObjects++
		identity := sourceObjectIdentity{infoHash: object.infoHash, size: object.size}
		missingByIdentity[identity] = append(missingByIdentity[identity], object)
	}

	unreferenced := make([]uuid.UUID, 0)
	var recoveryBytes uint64
	for publicID, entry := range root.archiveObjects {
		if _, exists := expectedIDs[publicID]; exists {
			continue
		}
		unreferenced = append(unreferenced, publicID)
		if entry.UncompressedSize64 > maxArchiveRecoveryBytes-recoveryBytes {
			return sourceObjectRecoveryResult{}, errors.New("PtYes unreferenced ZIP objects exceed the recovery byte budget")
		}
		recoveryBytes += entry.UncompressedSize64
	}
	result.UnreferencedObjects = int64(len(unreferenced))
	if len(missingByIdentity) == 0 || len(unreferenced) == 0 {
		return result, nil
	}
	if len(unreferenced) > maxArchiveRecoveryCandidates {
		return sourceObjectRecoveryResult{}, errors.New("PtYes ZIP has too many unreferenced objects for bounded recovery")
	}
	sort.Slice(unreferenced, func(left, right int) bool {
		return unreferenced[left].String() < unreferenced[right].String()
	})

	candidates := make(map[sourceObjectIdentity][]uuid.UUID)
	for _, publicID := range unreferenced {
		raw, err := readArchiveEntry(root.archiveObjects[publicID])
		if err != nil {
			return sourceObjectRecoveryResult{}, err
		}
		parsed, err := torrents.InspectLegacyV1OrHybrid(raw)
		if err != nil {
			// An unreferenced historical object is outside migration scope. It
			// can be ignored, but can never become a recovery candidate.
			continue
		}
		identity := sourceObjectIdentity{infoHash: parsed.InfoHashV1, size: parsed.TotalSizeBytes}
		if _, needed := missingByIdentity[identity]; needed {
			candidates[identity] = append(candidates[identity], publicID)
		}
	}

	root.archiveAliases = make(map[uuid.UUID]uuid.UUID)
	identities := make([]sourceObjectIdentity, 0, len(missingByIdentity))
	for identity := range missingByIdentity {
		identities = append(identities, identity)
	}
	sort.Slice(identities, func(left, right int) bool {
		if identities[left].infoHash != identities[right].infoHash {
			return identities[left].infoHash.Hex() < identities[right].infoHash.Hex()
		}
		return identities[left].size < identities[right].size
	})
	for _, identity := range identities {
		missing := missingByIdentity[identity]
		matching := candidates[identity]
		if len(missing) == 1 && len(matching) == 1 {
			root.archiveAliases[missing[0].publicID] = matching[0]
			result.RecoveredObjects++
			result.RecoveredLegacyIDs = append(result.RecoveredLegacyIDs, missing[0].legacyID)
			continue
		}
		if len(matching) > 0 {
			result.AmbiguousObjects += int64(len(missing))
		}
	}
	sort.Slice(result.RecoveredLegacyIDs, func(left, right int) bool {
		return result.RecoveredLegacyIDs[left] < result.RecoveredLegacyIDs[right]
	})
	return result, nil
}
