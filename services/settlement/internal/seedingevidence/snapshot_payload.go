package seedingevidence

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
)

func compressSnapshotPayload(payload []byte) ([]byte, error) {
	if len(payload) < 2 || len(payload) > trackerswarmv1.MaxEventBytes {
		return nil, ErrInput
	}
	var out bytes.Buffer
	writer, err := gzip.NewWriterLevel(&out, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err = writer.Write(payload); err != nil {
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func decodeSnapshotPayload(compressed []byte, size int, digest []byte) (trackerswarmv1.SnapshotChunk, error) {
	// Bound both stored and decompressed bytes before trusting database metadata.
	if len(compressed) > trackerswarmv1.MaxEventBytes+1024 || size < 2 || size > trackerswarmv1.MaxEventBytes || len(digest) != sha256.Size {
		return trackerswarmv1.SnapshotChunk{}, ErrInvariant
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return trackerswarmv1.SnapshotChunk{}, ErrInvariant
	}
	payload, err := io.ReadAll(io.LimitReader(reader, trackerswarmv1.MaxEventBytes+1))
	closeErr := reader.Close()
	if err != nil || closeErr != nil || len(payload) != size {
		return trackerswarmv1.SnapshotChunk{}, ErrInvariant
	}
	actual := sha256.Sum256(payload)
	if !bytes.Equal(actual[:], digest) {
		return trackerswarmv1.SnapshotChunk{}, ErrInvariant
	}
	chunk, err := trackerswarmv1.Decode(payload)
	if err != nil {
		return trackerswarmv1.SnapshotChunk{}, ErrInvariant
	}
	return chunk, nil
}

func storeSnapshotPayload(ctx context.Context, tx pgx.Tx, snapshotID uuid.UUID, index int32, payload []byte, digest []byte) error {
	compressed, err := compressSnapshotPayload(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO ledger.seeding_snapshot_payloads
  (snapshot_id,chunk_index,payload_gzip,uncompressed_bytes,payload_sha256) VALUES($1,$2,$3,$4,$5)`,
		snapshotID, index, compressed, len(payload), digest)
	if err != nil {
		return databaseError("store compressed snapshot payload", err)
	}
	return nil
}

// Both formats may coexist during a chunked snapshot's rolling upgrade.
// The canonical chunk digest, route coordinates and cross-chunk uniqueness
// are checked before completion and again when a reward selects the frame.
func readSnapshotCounts(ctx context.Context, tx pgx.Tx, queries *ledgerdb.Queries, id uuid.UUID) (map[[20]byte]snapshotCounts, error) {
	legacy, err := queries.ListSeedingSnapshotEntries(ctx, id)
	if err != nil {
		return nil, databaseError("read legacy snapshot entries", err)
	}
	result, err := snapshotCountsFromRows(legacy)
	if err != nil {
		return nil, err
	}
	var source string
	var epoch, sequence int64
	var chunks int32
	var observed pgtype.Timestamptz
	if err = tx.QueryRow(ctx, `SELECT source_id,routing_epoch,snapshot_sequence,chunk_count,observed_at
  FROM ledger.seeding_swarm_snapshots WHERE snapshot_id=$1`, id).Scan(&source, &epoch, &sequence, &chunks, &observed); err != nil {
		return nil, err
	}
	var missing bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(
        SELECT 1 FROM ledger.seeding_swarm_snapshot_chunks c
        WHERE c.snapshot_id=$1 AND c.compact_payload
          AND NOT EXISTS(SELECT 1 FROM ledger.seeding_snapshot_payloads p
                         WHERE p.snapshot_id=c.snapshot_id AND p.chunk_index=c.chunk_index))`, id).Scan(&missing); err != nil {
		return nil, err
	}
	if missing {
		return nil, ErrInvariant
	}
	rows, err := tx.Query(ctx, `SELECT p.chunk_index,p.payload_gzip,p.uncompressed_bytes,p.payload_sha256,c.event_id
  FROM ledger.seeding_snapshot_payloads p JOIN ledger.seeding_swarm_snapshot_chunks c USING(snapshot_id,chunk_index)
  WHERE p.snapshot_id=$1 ORDER BY p.chunk_index`, id)
	if err != nil {
		return nil, databaseError("read compressed snapshot chunks", err)
	}
	defer rows.Close()
	for rows.Next() {
		var index, size int32
		var compressed, digest []byte
		var eventID uuid.UUID
		if err = rows.Scan(&index, &compressed, &size, &digest, &eventID); err != nil {
			return nil, err
		}
		chunk, decodeErr := decodeSnapshotPayload(compressed, int(size), digest)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if chunk.SnapshotID != id.String() || chunk.EventID != eventID.String() || chunk.ChunkIndex != index ||
			chunk.ChunkCount != chunks || chunk.SourceID != source || chunk.RoutingEpoch != epoch ||
			chunk.SnapshotSequence != sequence || !canonicalEvidenceTime(chunk.ObservedAt).Equal(observed.Time) {
			return nil, ErrInvariant
		}
		for _, entry := range chunk.Entries {
			hash, e := hex.DecodeString(entry.InfoHashV1)
			if e != nil || len(hash) != 20 {
				return nil, ErrInvariant
			}
			var key [20]byte
			copy(key[:], hash)
			if _, exists := result[key]; exists {
				return nil, fmt.Errorf("%w: duplicate torrent across snapshot chunks", ErrConflict)
			}
			result[key] = snapshotCounts{Seeders: entry.Seeders, Leechers: entry.Leechers}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
