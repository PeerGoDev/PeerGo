package hnr

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
)

func incrementalProgress(ctx context.Context, tx pgx.Tx, queries *ledgerdb.Queries, record obligationRecord, at time.Time) (Progress, error) {
	var payload []byte
	err := tx.QueryRow(ctx, `SELECT state_json FROM settlement.hnr_progress_accumulators
  WHERE obligation_id=$1 FOR UPDATE`, record.ID).Scan(&payload)
	var previous *IncrementalState
	if err == nil {
		if len(payload) > 16<<20 {
			return Progress{}, ErrInvariant
		}
		previous = &IncrementalState{}
		if err = json.Unmarshal(payload, previous); err != nil {
			return Progress{}, ErrInvariant
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Progress{}, classifyHNRError("read incremental H&R progress", err)
	}
	cursors := map[string]int64{}
	if previous != nil {
		cursors = previous.Cursors
	}
	encoded, err := json.Marshal(cursors)
	if err != nil {
		return Progress{}, err
	}
	rows, err := tx.Query(ctx, `SELECT raw.event_id,raw.starts_at,raw.ends_at,raw.previous_left,raw.current_left,raw.raw_uploaded,
  coalesce(raw.source_stream,inbox.source_stream),coalesce(raw.source_sequence,inbox.source_sequence)
  FROM ledger.raw_session_intervals raw
  LEFT JOIN settlement.event_inbox inbox ON inbox.event_id=raw.event_id AND raw.source_stream IS NULL
  WHERE raw.user_id=$1 AND raw.torrent_id=$2 AND raw.ends_at>$3
   AND (coalesce(raw.source_sequence,inbox.source_sequence) IS NULL
    OR coalesce(raw.source_sequence,inbox.source_sequence)>coalesce(
      ($4::jsonb->>coalesce(raw.source_stream,inbox.source_stream))::bigint,0))
  ORDER BY coalesce(raw.source_stream,inbox.source_stream),coalesce(raw.source_sequence,inbox.source_sequence),raw.event_id`,
		record.Assessment.UserID, record.Assessment.TorrentID, record.Assessment.CompletedAt, encoded)
	if err != nil {
		return Progress{}, classifyHNRError("read incremental H&R delta", err)
	}
	var delta []SequencedInterval
	unknown := false
	for rows.Next() {
		var v SequencedInterval
		var start, end pgtype.Timestamptz
		var stream pgtype.Text
		var seq pgtype.Int8
		if err = rows.Scan(&v.EventID, &start, &end, &v.PreviousLeft, &v.CurrentLeft, &v.RawUploaded, &stream, &seq); err != nil {
			rows.Close()
			return Progress{}, err
		}
		if !start.Valid || !end.Valid {
			rows.Close()
			return Progress{}, ErrInvariant
		}
		v.StartsAt = start.Time.UTC().Round(0)
		v.EndsAt = end.Time.UTC().Round(0)
		if !stream.Valid || !seq.Valid {
			unknown = true
		} else {
			v.Stream = stream.String
			v.Sequence = seq.Int64
		}
		delta = append(delta, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Progress{}, err
	}
	if unknown {
		// An unsequenced legacy row cannot be represented by a safe high-water
		// cursor. Keep the old replay path and its existing raw-retention pins.
		if previous != nil {
			return Progress{}, ErrInvariant
		}
		legacy, err := listRawIntervals(ctx, queries, record.Assessment)
		if err != nil {
			return Progress{}, err
		}
		return Evaluate(record.progressInput(), legacy)
	}
	next, progress, err := AdvanceIncremental(record.progressInput(), previous, delta)
	if err != nil {
		return Progress{}, err
	}
	if previous == nil {
		legacy := make([]RawInterval, len(delta))
		for i, v := range delta {
			legacy[i] = v.RawInterval
		}
		expected, err := Evaluate(record.progressInput(), legacy)
		if err != nil {
			return Progress{}, err
		}
		if !reflect.DeepEqual(progress, expected) {
			return Progress{}, ErrInvariant
		}
	}
	stateJSON, err := json.Marshal(next)
	if err != nil {
		return Progress{}, err
	}
	cursorJSON, err := json.Marshal(next.Cursors)
	if err != nil {
		return Progress{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO settlement.hnr_progress_accumulators(obligation_id,state_json,source_cursors,updated_at)
  VALUES($1,$2,$3,$4) ON CONFLICT(obligation_id) DO UPDATE
  SET state_json=EXCLUDED.state_json,source_cursors=EXCLUDED.source_cursors,updated_at=EXCLUDED.updated_at`,
		record.ID, stateJSON, cursorJSON, at)
	if err != nil {
		return Progress{}, classifyHNRError("persist incremental H&R checkpoint", err)
	}
	return progress, nil
}
