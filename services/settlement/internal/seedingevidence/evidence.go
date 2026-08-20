package seedingevidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
)

type intervalFact struct {
	EventID        uuid.UUID
	UserID         uuid.UUID
	TorrentID      int64
	InfoHashV1     [20]byte
	StartsAt       time.Time
	EndsAt         time.Time
	RawUploaded    int64
	SourceSequence int64
}

type snapshotCounts struct {
	Seeders  int32
	Leechers int32
}

type evidenceItem struct {
	UserID         uuid.UUID
	TorrentID      int64
	InfoHashV1     [20]byte
	ActiveSeconds  int64
	RawUploaded    int64
	FirstActiveAt  time.Time
	LastActiveAt   time.Time
	Snapshot       snapshotCounts
	Sources        []intervalFact
	EvidenceSHA256 [sha256.Size]byte
}

type itemDigestSource struct {
	EventID        string    `json:"event_id"`
	SourceSequence int64     `json:"source_sequence"`
	StartsAt       time.Time `json:"starts_at"`
	EndsAt         time.Time `json:"ends_at"`
	RawUploaded    int64     `json:"raw_uploaded"`
}

type itemDigestDocument struct {
	UserID           string             `json:"user_id"`
	TorrentID        int64              `json:"torrent_id"`
	InfoHashV1       string             `json:"info_hash_v1"`
	ActiveSeconds    int64              `json:"active_seconds"`
	RawUploaded      int64              `json:"raw_uploaded"`
	SnapshotSeeders  int32              `json:"snapshot_seeders"`
	SnapshotLeechers int32              `json:"snapshot_leechers"`
	Sources          []itemDigestSource `json:"sources"`
}

type windowDigestItem struct {
	UserID         string `json:"user_id"`
	TorrentID      int64  `json:"torrent_id"`
	EvidenceSHA256 string `json:"evidence_sha256"`
}

type windowDigestDocument struct {
	SchemaVersion              string             `json:"schema_version"`
	WindowStart                time.Time          `json:"window_start"`
	WindowEnd                  time.Time          `json:"window_end"`
	AnnounceSourceStream       string             `json:"announce_source_stream"`
	AnnounceFenceSequence      int64              `json:"announce_fence_sequence"`
	AnnounceFenceReceivedAt    time.Time          `json:"announce_fence_received_at"`
	SelectedSnapshotID         string             `json:"selected_snapshot_id"`
	SelectedSnapshotSequence   int64              `json:"selected_snapshot_sequence"`
	SelectedSnapshotObservedAt time.Time          `json:"selected_snapshot_observed_at"`
	SnapshotFenceID            string             `json:"snapshot_fence_id"`
	SnapshotFenceSequence      int64              `json:"snapshot_fence_sequence"`
	SnapshotFenceObservedAt    time.Time          `json:"snapshot_fence_observed_at"`
	Items                      []windowDigestItem `json:"items"`
}

type itemKey struct {
	UserID    uuid.UUID
	TorrentID int64
}

// assembleItems unions overlapping ranges per user and torrent. A user with
// two clients seeding the same torrent for the same minute earns one minute of
// activity, while all source intervals remain linked for audit.
func assembleItems(facts []intervalFact, snapshot map[[20]byte]snapshotCounts) ([]evidenceItem, error) {
	groups := make(map[itemKey][]intervalFact)
	for _, fact := range facts {
		if fact.EventID == uuid.Nil || fact.UserID == uuid.Nil || fact.TorrentID < 1 ||
			!fact.EndsAt.After(fact.StartsAt) || fact.RawUploaded < 0 || fact.SourceSequence < 1 {
			return nil, ErrInvariant
		}
		key := itemKey{UserID: fact.UserID, TorrentID: fact.TorrentID}
		groups[key] = append(groups[key], fact)
	}
	keys := make([]itemKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(left, right itemKey) int {
		if order := bytes.Compare(left.UserID[:], right.UserID[:]); order != 0 {
			return order
		}
		if left.TorrentID < right.TorrentID {
			return -1
		}
		if left.TorrentID > right.TorrentID {
			return 1
		}
		return 0
	})
	items := make([]evidenceItem, 0, len(keys))
	for _, key := range keys {
		sources := groups[key]
		slices.SortFunc(sources, compareIntervalFacts)
		infoHash := sources[0].InfoHashV1
		var active time.Duration
		var rawUploaded int64
		unionStart, unionEnd := sources[0].StartsAt, sources[0].EndsAt
		for index, source := range sources {
			if source.InfoHashV1 != infoHash || rawUploaded > math.MaxInt64-source.RawUploaded {
				return nil, ErrInvariant
			}
			rawUploaded += source.RawUploaded
			if index == 0 {
				continue
			}
			if !source.StartsAt.After(unionEnd) {
				if source.EndsAt.After(unionEnd) {
					unionEnd = source.EndsAt
				}
				continue
			}
			active += unionEnd.Sub(unionStart)
			unionStart, unionEnd = source.StartsAt, source.EndsAt
		}
		active += unionEnd.Sub(unionStart)
		activeSeconds := int64(active / time.Second)
		if activeSeconds < 0 || activeSeconds > 3600 {
			return nil, ErrInvariant
		}
		item := evidenceItem{
			UserID: key.UserID, TorrentID: key.TorrentID, InfoHashV1: infoHash,
			ActiveSeconds: activeSeconds, RawUploaded: rawUploaded,
			FirstActiveAt: sources[0].StartsAt, LastActiveAt: sources[0].EndsAt,
			Snapshot: snapshot[infoHash], Sources: sources,
		}
		for _, source := range sources[1:] {
			if source.StartsAt.Before(item.FirstActiveAt) {
				item.FirstActiveAt = source.StartsAt
			}
			if source.EndsAt.After(item.LastActiveAt) {
				item.LastActiveAt = source.EndsAt
			}
		}
		digest, err := digestItem(item)
		if err != nil {
			return nil, err
		}
		item.EvidenceSHA256 = digest
		items = append(items, item)
	}
	return items, nil
}

func compareIntervalFacts(left, right intervalFact) int {
	if left.StartsAt.Before(right.StartsAt) {
		return -1
	}
	if left.StartsAt.After(right.StartsAt) {
		return 1
	}
	if left.EndsAt.Before(right.EndsAt) {
		return -1
	}
	if left.EndsAt.After(right.EndsAt) {
		return 1
	}
	return bytes.Compare(left.EventID[:], right.EventID[:])
}

func digestItem(item evidenceItem) ([sha256.Size]byte, error) {
	sources := make([]itemDigestSource, len(item.Sources))
	for index, source := range item.Sources {
		sources[index] = itemDigestSource{
			EventID: source.EventID.String(), SourceSequence: source.SourceSequence,
			StartsAt: source.StartsAt.UTC().Round(0), EndsAt: source.EndsAt.UTC().Round(0),
			RawUploaded: source.RawUploaded,
		}
	}
	document := itemDigestDocument{
		UserID: item.UserID.String(), TorrentID: item.TorrentID,
		InfoHashV1: hex.EncodeToString(item.InfoHashV1[:]), ActiveSeconds: item.ActiveSeconds,
		RawUploaded: item.RawUploaded, SnapshotSeeders: item.Snapshot.Seeders,
		SnapshotLeechers: item.Snapshot.Leechers, Sources: sources,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

func digestWindow(document windowDigestDocument) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(document)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}
