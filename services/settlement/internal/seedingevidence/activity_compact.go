package seedingevidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

const SchemaVersionV3 = "seeding.evidence.v3"

type activityProfile struct {
	ActivatesAt   time.Time
	CreditSeconds int32
	Verified      int32
	Required      int32
}
type compactRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func (repository *PostgresRepository) ensureActivityProfile(ctx context.Context) error {
	// Snapshot-only projectors deliberately do not register hourly policy.
	if repository.config.MaxIntervalCredit == 0 {
		return nil
	}
	_, err := repository.pool.Exec(ctx, `INSERT INTO settlement.activity_projection_profiles
  (source_stream,activates_at,max_interval_credit_seconds)
  SELECT $1,greatest(date_trunc('hour',clock_timestamp() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC',
    coalesce((SELECT date_trunc('hour',last_received_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
      FROM settlement.ingest_stream_cursors WHERE source_stream=$1),'-infinity'::timestamptz))+interval '1 hour',$2
  ON CONFLICT(source_stream) DO NOTHING`, repository.config.AnnounceStream, int32(repository.config.MaxIntervalCredit/time.Second))
	if err != nil {
		return databaseError("register incremental activity profile", err)
	}
	var seconds int32
	if err = repository.pool.QueryRow(ctx, `SELECT max_interval_credit_seconds FROM settlement.activity_projection_profiles
  WHERE source_stream=$1`, repository.config.AnnounceStream).Scan(&seconds); err != nil {
		return err
	}
	if seconds != int32(repository.config.MaxIntervalCredit/time.Second) {
		return ErrConflict
	}
	return nil
}
func loadActivityProfile(ctx context.Context, tx pgx.Tx, stream string) (activityProfile, bool, error) {
	var p activityProfile
	err := tx.QueryRow(ctx, `SELECT activates_at,max_interval_credit_seconds,shadow_verified_windows,required_shadow_windows
  FROM settlement.activity_projection_profiles WHERE source_stream=$1`, stream).Scan(&p.ActivatesAt, &p.CreditSeconds, &p.Verified, &p.Required)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, false, nil
	}
	return p, err == nil, err
}
func loadCompactActivity(ctx context.Context, tx pgx.Tx, stream string, hour time.Time, fence int64, snapshot map[[20]byte]snapshotCounts) ([]evidenceItem, error) {
	rows, err := tx.Query(ctx, `SELECT user_id,torrent_id,info_hash_v1,raw_uploaded,source_count,source_commitment,last_source_sequence,
  ARRAY(SELECT (extract(epoch FROM lower(r))*1000000)::bigint FROM unnest(active_ranges) r ORDER BY lower(r)),
  ARRAY(SELECT (extract(epoch FROM upper(r))*1000000)::bigint FROM unnest(active_ranges) r ORDER BY lower(r))
  FROM settlement.seeding_activity_hours WHERE source_stream=$1 AND window_start=$2 ORDER BY user_id,torrent_id`, stream, hour)
	if err != nil {
		return nil, databaseError("read incremental activity hour", err)
	}
	defer rows.Close()
	result := make([]evidenceItem, 0)
	for rows.Next() {
		var item evidenceItem
		var hash, root []byte
		var count, sequence int64
		var starts, ends []int64
		if err = rows.Scan(&item.UserID, &item.TorrentID, &hash, &item.RawUploaded, &count, &root, &sequence, &starts, &ends); err != nil {
			return nil, err
		}
		if len(hash) != 20 || len(root) != 32 || count < 1 || count > math.MaxInt32 || sequence > fence ||
			len(starts) == 0 || len(starts) != len(ends) {
			return nil, ErrInvariant
		}
		copy(item.InfoHashV1[:], hash)
		copy(item.SourceCommitment[:], root)
		item.Compact = true
		item.SourceCount = int32(count)
		var duration time.Duration
		for i, start := range starts {
			r := compactRange{Start: time.UnixMicro(start).UTC(), End: time.UnixMicro(ends[i]).UTC()}
			if r.Start.Before(hour) || r.End.After(hour.Add(time.Hour)) || !r.End.After(r.Start) ||
				(i > 0 && !r.Start.After(item.Ranges[i-1].End)) {
				return nil, ErrInvariant
			}
			duration += r.End.Sub(r.Start)
			item.Ranges = append(item.Ranges, r)
		}
		item.ActiveSeconds = int64(duration / time.Second)
		item.FirstActiveAt = item.Ranges[0].Start
		item.LastActiveAt = item.Ranges[len(item.Ranges)-1].End
		item.Snapshot = snapshot[item.InfoHashV1]
		item.EvidenceSHA256, err = digestCompactItem(item)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func digestCompactItem(item evidenceItem) ([32]byte, error) {
	document := struct {
		Schema           string         `json:"schema"`
		UserID           string         `json:"user_id"`
		TorrentID        int64          `json:"torrent_id"`
		InfoHash         string         `json:"info_hash_v1"`
		ActiveSeconds    int64          `json:"active_seconds"`
		RawUploaded      int64          `json:"raw_uploaded"`
		Seeders          int32          `json:"snapshot_seeders"`
		Leechers         int32          `json:"snapshot_leechers"`
		SourceCount      int32          `json:"source_count"`
		SourceCommitment string         `json:"source_commitment"`
		Ranges           []compactRange `json:"ranges"`
	}{SchemaVersionV3, item.UserID.String(), item.TorrentID, hex.EncodeToString(item.InfoHashV1[:]), item.ActiveSeconds,
		item.RawUploaded, item.Snapshot.Seeders, item.Snapshot.Leechers, item.SourceCount, hex.EncodeToString(item.SourceCommitment[:]), item.Ranges}
	b, err := json.Marshal(document)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(b), nil
}
func activitySourceCommitment(facts []intervalFact) [32]byte {
	ordered := append([]intervalFact(nil), facts...)
	slices.SortFunc(ordered, func(a, b intervalFact) int {
		if a.SourceSequence < b.SourceSequence {
			return -1
		}
		if a.SourceSequence > b.SourceSequence {
			return 1
		}
		return bytes.Compare(a.EventID[:], b.EventID[:])
	})
	var root [32]byte
	for _, fact := range ordered {
		b := append([]byte(nil), fact.EventID[:]...)
		for _, n := range []int64{fact.SourceSequence, fact.StartsAt.UnixMicro() - 946684800000000, fact.EndsAt.UnixMicro() - 946684800000000, fact.RawUploaded} {
			b = binary.BigEndian.AppendUint64(b, uint64(n))
		}
		leaf := sha256.Sum256(b)
		root = sha256.Sum256(append(root[:], leaf[:]...))
	}
	return root
}
func verifyCompactActivity(compact, legacy []evidenceItem) error {
	if len(compact) != len(legacy) {
		return ErrEvidenceDrift
	}
	for i, got := range compact {
		want := legacy[i]
		if got.UserID != want.UserID || got.TorrentID != want.TorrentID || got.InfoHashV1 != want.InfoHashV1 ||
			got.ActiveSeconds != want.ActiveSeconds || got.RawUploaded != want.RawUploaded ||
			!got.FirstActiveAt.Equal(want.FirstActiveAt) || !got.LastActiveAt.Equal(want.LastActiveAt) ||
			got.SourceCount != int32(len(want.Sources)) || got.SourceCommitment != activitySourceCommitment(want.Sources) {
			return ErrEvidenceDrift
		}
	}
	return nil
}
func insertCompactEvidenceItem(ctx context.Context, tx pgx.Tx, hour time.Time, item evidenceItem) error {
	ranges := make([][2]time.Time, len(item.Ranges))
	for i, r := range item.Ranges {
		ranges[i] = [2]time.Time{r.Start, r.End}
	}
	b, err := json.Marshal(ranges)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO ledger.seeding_evidence_items
 (window_start,user_id,torrent_id,info_hash_v1,active_seconds,raw_uploaded,source_interval_count,
 first_active_at,last_active_at,snapshot_seeders,snapshot_leechers,evidence_sha256,active_ranges,source_commitment)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,
 (SELECT range_agg(tstzrange((r->>0)::timestamptz,(r->>1)::timestamptz,'[)'))
  FROM jsonb_array_elements($13::jsonb) r),$14)`,
		hour, item.UserID, item.TorrentID, item.InfoHashV1[:], item.ActiveSeconds, item.RawUploaded, item.SourceCount,
		item.FirstActiveAt, item.LastActiveAt, item.Snapshot.Seeders, item.Snapshot.Leechers, item.EvidenceSHA256[:], b, item.SourceCommitment[:])
	if err != nil {
		return databaseError("insert compact seeding evidence item", err)
	}
	return nil
}
