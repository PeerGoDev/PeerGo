package traffic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/peergo/peergo/contracts/go/settlementhnrv1"
	"github.com/peergo/peergo/services/core/internal/generated/trafficdb"
)

// ApplyHNR commits the external inbox and the latest user-safe obligation
// snapshot atomically. The advisory lock closes the empty-row race for the
// first version; all later versions must be exactly contiguous.
func (repository *PostgresRepository) ApplyHNR(ctx context.Context, payload []byte, receivedAt time.Time) (HNRApplyResult, error) {
	if receivedAt.IsZero() {
		return HNRApplyResult{}, ErrInput
	}
	event, err := settlementhnrv1.Decode(payload)
	if err != nil {
		return HNRApplyResult{}, ErrInput
	}
	eventID, err := uuid.Parse(event.EventID)
	if err != nil {
		return HNRApplyResult{}, ErrInput
	}
	obligationID, err := uuid.Parse(event.ObligationID)
	if err != nil {
		return HNRApplyResult{}, ErrInput
	}
	userID, err := uuid.Parse(event.UserID)
	if err != nil {
		return HNRApplyResult{}, ErrInput
	}
	payloadDigest := sha256.Sum256(payload)
	receivedAt = receivedAt.UTC().Truncate(time.Microsecond)
	appliedAt := repository.now().UTC().Truncate(time.Microsecond)
	if appliedAt.Before(receivedAt) {
		appliedAt = receivedAt
	}

	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return HNRApplyResult{}, fmt.Errorf("begin Core H&R projection: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := trafficdb.New(tx)
	if err := queries.LockHNRProjectionAggregate(ctx, obligationID); err != nil {
		return HNRApplyResult{}, classifyDatabaseError("lock Core H&R projection", err)
	}
	existingInbox, err := queries.GetHNRProjectionInbox(ctx, eventID)
	if err == nil {
		if !bytes.Equal(existingInbox.PayloadSha256, payloadDigest[:]) || !bytes.Equal([]byte(existingInbox.PayloadJson), payload) {
			return HNRApplyResult{}, ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return HNRApplyResult{}, classifyDatabaseError("commit duplicate Core H&R projection", err)
		}
		return HNRApplyResult{EventID: eventID, Duplicate: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return HNRApplyResult{}, classifyDatabaseError("read Core H&R inbox", err)
	}

	existing, err := queries.GetUserHNRObligationForUpdate(ctx, obligationID)
	if errors.Is(err, pgx.ErrNoRows) {
		if event.ObligationVersion != 1 {
			return HNRApplyResult{}, ErrInvariant
		}
		if err := queries.InsertUserHNRObligation(ctx, insertHNRParams(event, obligationID, userID, appliedAt)); err != nil {
			return HNRApplyResult{}, classifyDatabaseError("insert Core H&R obligation", err)
		}
	} else if err != nil {
		return HNRApplyResult{}, classifyDatabaseError("lock Core H&R obligation", err)
	} else {
		if validateHNRTransition(existing, event, userID) != nil {
			return HNRApplyResult{}, ErrInvariant
		}
		rows, err := queries.UpdateUserHNRObligation(ctx, trafficdb.UpdateUserHNRObligationParams{
			State: string(event.State), SeededSeconds: event.SeededSeconds, RawUploaded: event.RawUploaded,
			RawRatioBasisPoints: event.RawRatioBasisPoints,
			SatisfiedBy:         nullableSatisfiedBy(event.SatisfiedBy), SatisfiedAt: nullableTrafficTime(event.SatisfiedAt),
			NewVersion: event.ObligationVersion, OccurredAt: trafficTimestamp(event.OccurredAt), AppliedAt: trafficTimestamp(appliedAt),
			ObligationID: obligationID, ExpectedVersion: existing.Version,
		})
		if err != nil {
			return HNRApplyResult{}, classifyDatabaseError("advance Core H&R obligation", err)
		}
		if rows != 1 {
			return HNRApplyResult{}, ErrInvariant
		}
	}
	if err := queries.InsertHNRProjectionInbox(ctx, trafficdb.InsertHNRProjectionInboxParams{
		EventID: eventID, PayloadSha256: payloadDigest[:], PayloadJson: string(payload),
		ObligationID: obligationID, ObligationVersion: event.ObligationVersion,
		OccurredAt: trafficTimestamp(event.OccurredAt), ReceivedAt: trafficTimestamp(receivedAt), AppliedAt: trafficTimestamp(appliedAt),
	}); err != nil {
		return HNRApplyResult{}, classifyDatabaseError("insert Core H&R inbox", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return HNRApplyResult{}, classifyDatabaseError("commit Core H&R projection", err)
	}
	return HNRApplyResult{EventID: eventID}, nil
}

func (repository *PostgresRepository) ListHNR(ctx context.Context, userID uuid.UUID, query HNRQuery) (HNRPage, error) {
	if query.Filter == "" {
		query.Filter = HNRFilterOpen
	}
	if query.Limit == 0 {
		query.Limit = DefaultHNRLimit
	}
	if userID == uuid.Nil || !validHNRFilter(query.Filter) || query.Limit < 1 || query.Limit > MaximumHNRLimit ||
		(query.Cursor != nil && (query.Cursor.CompletedAt.IsZero() || query.Cursor.ObligationID == uuid.Nil)) {
		return HNRPage{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return HNRPage{}, fmt.Errorf("begin Core H&R page: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := trafficdb.New(tx)
	summaryRow, err := queries.GetUserHNRSummary(ctx, userID)
	if err != nil {
		return HNRPage{}, fmt.Errorf("read Core H&R summary: %w", err)
	}
	if !summaryRow.AsOf.Valid {
		return HNRPage{}, ErrInvariant
	}
	summary := HNRSummary{
		Total: summaryRow.Total, Tracking: summaryRow.Tracking, Grace: summaryRow.Grace,
		Overdue: summaryRow.Overdue, Satisfied: summaryRow.Satisfied, Exempt: summaryRow.Exempt,
	}
	if !validHNRSummary(summary) {
		return HNRPage{}, ErrInvariant
	}
	params := trafficdb.ListUserHNRObligationsParams{
		StatusFilter: string(query.Filter), ResultLimit: int32(query.Limit + 1), UserID: userID,
	}
	if query.Cursor != nil {
		params.CursorCompletedAt = trafficTimestamp(query.Cursor.CompletedAt)
		params.CursorObligationID = pgtype.UUID{Bytes: query.Cursor.ObligationID, Valid: true}
	}
	rows, err := queries.ListUserHNRObligations(ctx, params)
	if err != nil {
		return HNRPage{}, fmt.Errorf("list Core H&R obligations: %w", err)
	}
	items := make([]HNREntry, 0, min(len(rows), query.Limit))
	for index, row := range rows {
		if index == query.Limit {
			break
		}
		entry, err := hnrEntryFromRow(row)
		if err != nil {
			return HNRPage{}, err
		}
		items = append(items, entry)
	}
	var nextCursor *HNRCursor
	if len(rows) > query.Limit {
		last := items[len(items)-1]
		nextCursor = &HNRCursor{CompletedAt: last.CompletedAt, ObligationID: last.ObligationID}
	}
	if err := tx.Commit(ctx); err != nil {
		return HNRPage{}, fmt.Errorf("commit Core H&R page: %w", err)
	}
	return HNRPage{
		AsOf: summaryRow.AsOf.Time.UTC().Truncate(time.Microsecond), Summary: summary,
		Items: items, NextCursor: nextCursor,
	}, nil
}

func insertHNRParams(event settlementhnrv1.Event, obligationID, userID uuid.UUID, appliedAt time.Time) trafficdb.InsertUserHNRObligationParams {
	return trafficdb.InsertUserHNRObligationParams{
		ObligationID: obligationID, UserID: userID, TorrentID: event.TorrentID,
		CompletedAt: trafficTimestamp(event.CompletedAt), State: string(event.State),
		SeededSeconds: event.SeededSeconds, RequiredSeedSeconds: event.RequiredSeedSeconds,
		RawUploaded: event.RawUploaded, RawDownloaded: event.RawDownloaded,
		RawRatioBasisPoints: event.RawRatioBasisPoints, RequiredRatioBasisPoints: event.RequiredRatioBPS,
		AssessmentDueAt: trafficTimestamp(event.AssessmentDueAt), GraceEndsAt: trafficTimestamp(event.GraceEndsAt),
		SatisfiedBy: nullableSatisfiedBy(event.SatisfiedBy), SatisfiedAt: nullableTrafficTime(event.SatisfiedAt),
		Version: event.ObligationVersion, OccurredAt: trafficTimestamp(event.OccurredAt), AppliedAt: trafficTimestamp(appliedAt),
	}
}

func validateHNRTransition(existing trafficdb.TrafficUserHnrObligation, event settlementhnrv1.Event, userID uuid.UUID) error {
	if existing.ObligationID == uuid.Nil || existing.UserID != userID || existing.TorrentID != event.TorrentID ||
		!existing.CompletedAt.Valid || !existing.CompletedAt.Time.Equal(event.CompletedAt) ||
		existing.RequiredSeedSeconds != event.RequiredSeedSeconds || existing.RawDownloaded != event.RawDownloaded ||
		existing.RequiredRatioBasisPoints != event.RequiredRatioBPS || !existing.AssessmentDueAt.Valid ||
		!existing.AssessmentDueAt.Time.Equal(event.AssessmentDueAt) || !existing.GraceEndsAt.Valid ||
		!existing.GraceEndsAt.Time.Equal(event.GraceEndsAt) || existing.Version+1 != event.ObligationVersion ||
		existing.SeededSeconds > event.SeededSeconds || existing.RawUploaded > event.RawUploaded ||
		existing.RawRatioBasisPoints > event.RawRatioBasisPoints || !existing.OccurredAt.Valid ||
		event.OccurredAt.Before(existing.OccurredAt.Time) || existing.State != string(settlementhnrv1.StateTracking) ||
		(event.State != settlementhnrv1.StateTracking && event.State != settlementhnrv1.StateSatisfied) {
		return ErrInvariant
	}
	return nil
}

func hnrEntryFromRow(row trafficdb.ListUserHNRObligationsRow) (HNREntry, error) {
	if row.ObligationID == uuid.Nil || row.TorrentID < 1 || strings.TrimSpace(row.TorrentTitle) == "" ||
		!row.CompletedAt.Valid || !row.AssessmentDueAt.Valid || !row.GraceEndsAt.Valid || !row.OccurredAt.Valid ||
		row.SeededSeconds < 0 || row.RequiredSeedSeconds < 0 || row.RawUploaded < 0 || row.RawDownloaded < 0 ||
		row.RawRatioBasisPoints < 0 || row.RequiredRatioBasisPoints < 0 ||
		row.AssessmentDueAt.Time.Before(row.CompletedAt.Time) || row.GraceEndsAt.Time.Before(row.AssessmentDueAt.Time) {
		return HNREntry{}, ErrInvariant
	}
	status := HNRStatus(row.DisplayStatus)
	if !validHNRStatus(status) {
		return HNREntry{}, ErrInvariant
	}
	var satisfiedBy *settlementhnrv1.SatisfiedBy
	if row.SatisfiedBy != "" {
		value := settlementhnrv1.SatisfiedBy(row.SatisfiedBy)
		if value != settlementhnrv1.SatisfiedBySeedTime && value != settlementhnrv1.SatisfiedByRawRatio && value != settlementhnrv1.SatisfiedByExempt {
			return HNREntry{}, ErrInvariant
		}
		satisfiedBy = &value
	}
	var satisfiedAt *time.Time
	if row.SatisfiedAt.Valid {
		value := row.SatisfiedAt.Time.UTC().Round(0)
		satisfiedAt = &value
	}
	if (status == HNRStatusTracking || status == HNRStatusGrace || status == HNRStatusOverdue) && (satisfiedBy != nil || satisfiedAt != nil) ||
		(status == HNRStatusSatisfied && (satisfiedBy == nil || satisfiedAt == nil || *satisfiedBy == settlementhnrv1.SatisfiedByExempt)) ||
		(status == HNRStatusExempt && (satisfiedBy == nil || *satisfiedBy != settlementhnrv1.SatisfiedByExempt || satisfiedAt == nil)) {
		return HNREntry{}, ErrInvariant
	}
	entry := HNREntry{
		ObligationID: row.ObligationID, TorrentID: row.TorrentID, TorrentTitle: row.TorrentTitle,
		CompletedAt: row.CompletedAt.Time.UTC().Round(0), Status: status,
		SeededSeconds: row.SeededSeconds, RequiredSeedSeconds: row.RequiredSeedSeconds,
		RawUploaded: row.RawUploaded, RawDownloaded: row.RawDownloaded,
		RawRatioBasisPoints: row.RawRatioBasisPoints, RequiredRatioBasisPoints: row.RequiredRatioBasisPoints,
		AssessmentDueAt: row.AssessmentDueAt.Time.UTC().Round(0), GraceEndsAt: row.GraceEndsAt.Time.UTC().Round(0),
		SatisfiedBy: satisfiedBy, SatisfiedAt: satisfiedAt, UpdatedAt: row.OccurredAt.Time.UTC().Round(0),
		CanAppeal: row.CanAppeal,
	}
	if row.AppealStatus != "" {
		appealStatus := HNRAppealStatus(row.AppealStatus)
		if !validHNRAppealStatus(appealStatus) || !row.AppealStatement.Valid ||
			!validHNRAppealStatement(row.AppealStatement.String) || !row.AppealCreatedAt.Valid {
			return HNREntry{}, ErrInvariant
		}
		appeal := &MyHNRAppeal{
			Status: appealStatus, Statement: row.AppealStatement.String,
			SubmittedAt: row.AppealCreatedAt.Time.UTC().Round(0),
		}
		if row.AppealResolvedAt.Valid {
			value := row.AppealResolvedAt.Time.UTC().Round(0)
			appeal.ResolvedAt = &value
		}
		if row.AppealResponse.Valid {
			appeal.Response = row.AppealResponse.String
		}
		if (appealStatus == HNRAppealPending && (appeal.ResolvedAt != nil || appeal.Response != "")) ||
			(appealStatus == HNRAppealObligationResolved && (appeal.ResolvedAt == nil || appeal.Response != "")) ||
			((appealStatus == HNRAppealApproved || appealStatus == HNRAppealRejected) &&
				(appeal.ResolvedAt == nil || !validHNRAppealResponse(appeal.Response))) {
			return HNREntry{}, ErrInvariant
		}
		entry.Appeal = appeal
	}
	if entry.Appeal != nil && entry.CanAppeal {
		return HNREntry{}, ErrInvariant
	}
	return entry, nil
}

func validHNRFilter(value HNRFilter) bool {
	switch value {
	case HNRFilterAll, HNRFilterOpen, HNRFilterTracking, HNRFilterGrace, HNRFilterOverdue, HNRFilterSatisfied, HNRFilterExempt:
		return true
	default:
		return false
	}
}

func validHNRStatus(value HNRStatus) bool {
	switch value {
	case HNRStatusTracking, HNRStatusGrace, HNRStatusOverdue, HNRStatusSatisfied, HNRStatusExempt:
		return true
	default:
		return false
	}
}

func validHNRSummary(summary HNRSummary) bool {
	return summary.Total >= 0 && summary.Tracking >= 0 && summary.Grace >= 0 && summary.Overdue >= 0 &&
		summary.Satisfied >= 0 && summary.Exempt >= 0 &&
		summary.Total == summary.Tracking+summary.Grace+summary.Overdue+summary.Satisfied+summary.Exempt
}

func nullableSatisfiedBy(value *settlementhnrv1.SatisfiedBy) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(*value), Valid: true}
}

func nullableTrafficTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return trafficTimestamp(*value)
}

var _ HNRProjector = (*PostgresRepository)(nil)
var _ HNRRepository = (*PostgresRepository)(nil)
