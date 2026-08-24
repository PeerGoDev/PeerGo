package traffic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
	"github.com/peergo/peergo/services/core/internal/generated/trafficdb"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewPostgresRepository(pool *pgxpool.Pool, now func() time.Time) (*PostgresRepository, error) {
	if pool == nil {
		return nil, ErrInput
	}
	if now == nil {
		now = time.Now
	}
	return &PostgresRepository{pool: pool, now: now}, nil
}

// Apply validates canonical event bytes, inserts the external idempotency
// fence, appends an immutable Core projection entry and advances both total
// read models in one Core transaction. A JetStream ACK may happen only after
// this method returns successfully.
func (repository *PostgresRepository) Apply(ctx context.Context, payload []byte, receivedAt time.Time) (ApplyResult, error) {
	if receivedAt.IsZero() {
		return ApplyResult{}, ErrInput
	}
	event, err := settlementtrafficv1.Decode(payload)
	if err != nil {
		return ApplyResult{}, ErrInput
	}
	eventID, err := uuid.Parse(event.EventID)
	if err != nil {
		return ApplyResult{}, ErrInput
	}
	userID, err := uuid.Parse(event.UserID)
	if err != nil {
		return ApplyResult{}, ErrInput
	}
	settlementDigest, err := hex.DecodeString(event.SettlementSHA256)
	if err != nil || len(settlementDigest) != sha256.Size {
		return ApplyResult{}, ErrInput
	}
	payloadDigest := sha256.Sum256(payload)
	appliedAt := repository.now().UTC().Round(0)
	receivedAt = receivedAt.UTC().Round(0)
	if appliedAt.Before(receivedAt) {
		appliedAt = receivedAt
	}

	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ApplyResult{}, fmt.Errorf("begin Core traffic projection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := trafficdb.New(tx)
	_, err = queries.InsertTrafficSettlementInbox(ctx, trafficdb.InsertTrafficSettlementInboxParams{
		EventID: eventID, PayloadSha256: payloadDigest[:],
		OccurredAt: trafficTimestamp(event.OccurredAt), ReceivedAt: trafficTimestamp(receivedAt), AppliedAt: trafficTimestamp(appliedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingDigest, getErr := queries.GetTrafficSettlementInbox(ctx, eventID)
		if getErr != nil {
			return ApplyResult{}, classifyDatabaseError("read duplicate Core traffic inbox event", getErr)
		}
		if !bytes.Equal(existingDigest, payloadDigest[:]) {
			return ApplyResult{}, ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return ApplyResult{}, classifyDatabaseError("commit duplicate Core traffic projection", err)
		}
		return ApplyResult{EventID: eventID, Duplicate: true}, nil
	}
	if err != nil {
		return ApplyResult{}, classifyDatabaseError("insert Core traffic inbox event", err)
	}
	if err := queries.InsertUserTrafficEntry(ctx, trafficdb.InsertUserTrafficEntryParams{
		SettlementID: eventID, UserID: userID, TorrentID: event.TorrentID,
		IntervalStartsAt: trafficTimestamp(event.IntervalStartsAt), IntervalEndsAt: trafficTimestamp(event.IntervalEndsAt),
		RawUploaded: event.RawUploaded, RawDownloaded: event.RawDownloaded,
		CreditedUploaded: event.CreditedUploaded, ChargedDownloaded: event.ChargedDownloaded,
		SettlementSha256: settlementDigest, OccurredAt: trafficTimestamp(event.OccurredAt), AppliedAt: trafficTimestamp(appliedAt),
	}); err != nil {
		return ApplyResult{}, classifyDatabaseError("append Core user traffic entry", err)
	}
	if event.Explanation != nil {
		// The event contract has already reconciled every public segment with the
		// immutable header. Persist the explanation in the same transaction so
		// Core can never expose a partial breakdown for a committed entry.
		if err := queries.InsertUserTrafficExplanation(ctx, trafficdb.InsertUserTrafficExplanationParams{
			SettlementID: eventID, Status: string(event.Explanation.Status), SegmentCount: event.Explanation.SegmentCount,
		}); err != nil {
			return ApplyResult{}, classifyDatabaseError("append Core user traffic explanation", err)
		}
		for index, segment := range event.Explanation.Segments {
			if err := queries.InsertUserTrafficExplanationSegment(ctx, trafficdb.InsertUserTrafficExplanationSegmentParams{
				SettlementID: eventID, SegmentIndex: int32(index),
				StartsAt: trafficTimestamp(segment.StartsAt), EndsAt: trafficTimestamp(segment.EndsAt),
				RawUploaded: segment.RawUploaded, RawDownloaded: segment.RawDownloaded,
				CreditedUploaded: segment.CreditedUploaded, ChargedDownloaded: segment.ChargedDownloaded,
			}); err != nil {
				return ApplyResult{}, classifyDatabaseError("append Core user traffic explanation segment", err)
			}
		}
	}
	if err := queries.UpsertUserTrafficTotals(ctx, trafficdb.UpsertUserTrafficTotalsParams{
		UserID: userID, RawUploaded: event.RawUploaded, RawDownloaded: event.RawDownloaded,
		CreditedUploaded: event.CreditedUploaded, ChargedDownloaded: event.ChargedDownloaded,
		OccurredAt: trafficTimestamp(event.OccurredAt), UpdatedAt: trafficTimestamp(appliedAt),
	}); err != nil {
		return ApplyResult{}, classifyDatabaseError("advance Core user traffic totals", err)
	}
	if err := queries.UpsertUserTorrentTrafficTotals(ctx, trafficdb.UpsertUserTorrentTrafficTotalsParams{
		UserID: userID, TorrentID: event.TorrentID, RawUploaded: event.RawUploaded, RawDownloaded: event.RawDownloaded,
		CreditedUploaded: event.CreditedUploaded, ChargedDownloaded: event.ChargedDownloaded,
		OccurredAt: trafficTimestamp(event.OccurredAt), UpdatedAt: trafficTimestamp(appliedAt),
	}); err != nil {
		return ApplyResult{}, classifyDatabaseError("advance Core user torrent traffic totals", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplyResult{}, classifyDatabaseError("commit Core traffic projection", err)
	}
	return ApplyResult{EventID: eventID}, nil
}

// Overview reads totals and recent immutable entries from one repeatable-read
// snapshot. Without that boundary a concurrently applied settlement could
// appear in totals but not in the entry list (or the reverse), producing a
// misleading user-facing account view.
func (repository *PostgresRepository) Overview(ctx context.Context, userID uuid.UUID, limit int) (Overview, error) {
	if userID == uuid.Nil || limit < 1 || limit > MaximumOverviewLimit {
		return Overview{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Overview{}, fmt.Errorf("begin Core traffic overview: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := trafficdb.New(tx)

	var totals Totals
	hasTotals := true
	row, err := queries.GetUserTrafficTotals(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		hasTotals = false
	} else if err != nil {
		return Overview{}, fmt.Errorf("read Core traffic totals: %w", err)
	} else {
		if row.RawUploaded < 0 || row.RawDownloaded < 0 || row.CreditedUploaded < 0 ||
			row.ChargedDownloaded < 0 || row.EntryCount < 0 || !row.UpdatedAt.Valid ||
			(row.EntryCount > 0 && !row.LastOccurredAt.Valid) {
			return Overview{}, ErrInvariant
		}
		totals = Totals{
			RawUploaded: row.RawUploaded, RawDownloaded: row.RawDownloaded,
			CreditedUploaded: row.CreditedUploaded, ChargedDownloaded: row.ChargedDownloaded,
			EntryCount: row.EntryCount,
		}
		if row.LastOccurredAt.Valid {
			value := row.LastOccurredAt.Time.UTC().Round(0)
			totals.LastSettledAt = &value
		}
		value := row.UpdatedAt.Time.UTC().Round(0)
		totals.ProjectionUpdatedAt = &value
	}

	rows, err := queries.ListUserTrafficEntries(ctx, trafficdb.ListUserTrafficEntriesParams{
		UserID: userID, ResultLimit: int32(limit),
	})
	if err != nil {
		return Overview{}, fmt.Errorf("list Core traffic entries: %w", err)
	}
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		if row.SettlementID == uuid.Nil || row.TorrentID < 1 || strings.TrimSpace(row.TorrentTitle) == "" ||
			!row.IntervalStartsAt.Valid || !row.IntervalEndsAt.Valid || !row.OccurredAt.Valid ||
			!row.IntervalEndsAt.Time.After(row.IntervalStartsAt.Time) || row.RawUploaded < 0 || row.RawDownloaded < 0 ||
			row.CreditedUploaded < 0 || row.ChargedDownloaded < 0 {
			return Overview{}, ErrInvariant
		}
		explanation, err := explanationFromProjection(row.ExplanationStatus, row.ExplanationSegmentCount)
		if err != nil {
			return Overview{}, err
		}
		entries = append(entries, Entry{
			ID: row.SettlementID, TorrentID: row.TorrentID, TorrentTitle: row.TorrentTitle,
			IntervalStartedAt: row.IntervalStartsAt.Time.UTC().Round(0),
			IntervalEndedAt:   row.IntervalEndsAt.Time.UTC().Round(0),
			RawUploaded:       row.RawUploaded, RawDownloaded: row.RawDownloaded,
			CreditedUploaded: row.CreditedUploaded, ChargedDownloaded: row.ChargedDownloaded,
			SettledAt: row.OccurredAt.Time.UTC().Round(0), Explanation: explanation,
		})
	}
	for index := range entries {
		if validateExplanationProjection(entries[index]) != nil {
			return Overview{}, ErrInvariant
		}
	}
	if (!hasTotals && len(entries) != 0) || (hasTotals && totals.EntryCount < int64(len(entries))) {
		return Overview{}, ErrInvariant
	}
	activityRows, err := queries.ListUserTorrentActivity(ctx, trafficdb.ListUserTorrentActivityParams{
		UserID: userID, ResultLimit: MaximumTorrentActivity,
	})
	if err != nil {
		return Overview{}, fmt.Errorf("list Core user torrent activity: %w", err)
	}
	activity := make([]TorrentActivity, 0, len(activityRows))
	for _, row := range activityRows {
		completed := row.Completed.Valid && row.Completed.Bool
		if row.TorrentID < 1 || strings.TrimSpace(row.TorrentTitle) == "" || row.TotalSizeBytes < 1 ||
			row.RawUploaded < 0 || row.RawDownloaded < 0 || row.ProgressBasisPoints < 0 ||
			row.ProgressBasisPoints > 10_000 || !row.Completed.Valid || !row.LastOccurredAt.Valid ||
			(completed && row.ProgressBasisPoints != 10_000) {
			return Overview{}, ErrInvariant
		}
		activity = append(activity, TorrentActivity{
			TorrentID: row.TorrentID, TorrentTitle: row.TorrentTitle, TotalSizeBytes: row.TotalSizeBytes,
			RawUploaded: row.RawUploaded, RawDownloaded: row.RawDownloaded,
			ProgressBasisPts: int(row.ProgressBasisPoints), Completed: completed,
			LastSettledAt: row.LastOccurredAt.Time.UTC().Round(0),
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return Overview{}, fmt.Errorf("commit Core traffic overview: %w", err)
	}
	return Overview{Totals: totals, Entries: entries, TorrentActivity: activity}, nil
}

func explanationFromProjection(status pgtype.Text, count pgtype.Int4) (Explanation, error) {
	if !status.Valid && !count.Valid {
		return Explanation{Status: ExplanationNotAvailable, Segments: []ExplanationSegment{}}, nil
	}
	if !status.Valid || !count.Valid || count.Int32 < 1 {
		return Explanation{}, ErrInvariant
	}
	result := Explanation{Status: ExplanationStatus(status.String), SegmentCount: count.Int32, Segments: []ExplanationSegment{}}
	switch result.Status {
	case ExplanationComplete:
		if result.SegmentCount > settlementtrafficv1.MaxExplanationSegments {
			return Explanation{}, ErrInvariant
		}
		result.Segments = make([]ExplanationSegment, 0, result.SegmentCount)
	case ExplanationTooManySegments:
		if result.SegmentCount <= settlementtrafficv1.MaxExplanationSegments {
			return Explanation{}, ErrInvariant
		}
	default:
		return Explanation{}, ErrInvariant
	}
	return result, nil
}

func validateExplanationProjection(entry Entry) error {
	switch entry.Explanation.Status {
	case ExplanationNotAvailable:
		if entry.Explanation.SegmentCount != 0 || len(entry.Explanation.Segments) != 0 {
			return ErrInvariant
		}
		return nil
	case ExplanationTooManySegments:
		if entry.Explanation.SegmentCount <= settlementtrafficv1.MaxExplanationSegments || len(entry.Explanation.Segments) != 0 {
			return ErrInvariant
		}
		return nil
	case ExplanationComplete:
		if entry.Explanation.SegmentCount < 1 || int(entry.Explanation.SegmentCount) != len(entry.Explanation.Segments) {
			return ErrInvariant
		}
	default:
		return ErrInvariant
	}

	cursor := entry.IntervalStartedAt
	var rawUploaded, rawDownloaded, creditedUploaded, chargedDownloaded int64
	for index, segment := range entry.Explanation.Segments {
		if segment.Index != int32(index) || !segment.StartsAt.Equal(cursor) || !segment.EndsAt.After(segment.StartsAt) ||
			segment.EndsAt.After(entry.IntervalEndedAt) {
			return ErrInvariant
		}
		var ok bool
		if rawUploaded, ok = addProjectedBytes(rawUploaded, segment.RawUploaded); !ok {
			return ErrInvariant
		}
		if rawDownloaded, ok = addProjectedBytes(rawDownloaded, segment.RawDownloaded); !ok {
			return ErrInvariant
		}
		if creditedUploaded, ok = addProjectedBytes(creditedUploaded, segment.CreditedUploaded); !ok {
			return ErrInvariant
		}
		if chargedDownloaded, ok = addProjectedBytes(chargedDownloaded, segment.ChargedDownloaded); !ok {
			return ErrInvariant
		}
		cursor = segment.EndsAt
	}
	if !cursor.Equal(entry.IntervalEndedAt) || rawUploaded != entry.RawUploaded || rawDownloaded != entry.RawDownloaded ||
		creditedUploaded != entry.CreditedUploaded || chargedDownloaded != entry.ChargedDownloaded {
		return ErrInvariant
	}
	return nil
}

func addProjectedBytes(left, right int64) (int64, bool) {
	if right < 0 || right > math.MaxInt64-left {
		return 0, false
	}
	return left + right, true
}

func trafficTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC().Truncate(time.Microsecond), Valid: true}
}

func classifyDatabaseError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) &&
		(strings.HasPrefix(postgresError.Code, "22") || strings.HasPrefix(postgresError.Code, "23") || postgresError.Code == "P0001") {
		return fmt.Errorf("%w: %s: %v", ErrInvariant, operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ Projector = (*PostgresRepository)(nil)
var _ OverviewRepository = (*PostgresRepository)(nil)
