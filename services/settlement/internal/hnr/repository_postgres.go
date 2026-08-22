package hnr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
	"github.com/peergo/peergo/services/settlement/internal/hnrpolicy"
)

var hnrErrorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, ErrInput
	}
	return &PostgresRepository{pool: pool}, nil
}

func (repository *PostgresRepository) ClaimNext(ctx context.Context, now time.Time, leaseDuration time.Duration) (PendingWork, bool, error) {
	if now.IsZero() || leaseDuration < time.Second || leaseDuration > 10*time.Minute {
		return PendingWork{}, false, ErrInput
	}
	leaseToken := uuid.New()
	row, err := ledgerdb.New(repository.pool).ClaimNextHNRWork(ctx, ledgerdb.ClaimNextHNRWorkParams{
		LeaseToken: leaseToken, LeaseUntil: hnrTimestamp(now.Add(leaseDuration)), ClaimedAt: hnrTimestamp(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PendingWork{}, false, nil
	}
	if err != nil {
		return PendingWork{}, false, fmt.Errorf("claim H&R work: %w", err)
	}
	if row.IntervalEventID == uuid.Nil || !row.LeaseToken.Valid || uuid.UUID(row.LeaseToken.Bytes) != leaseToken || row.Attempts < 1 {
		return PendingWork{}, false, ErrInvariant
	}
	return PendingWork{IntervalEventID: row.IntervalEventID, LeaseToken: leaseToken, Attempts: row.Attempts}, true, nil
}

// Process serializes one user/torrent aggregate, snapshots the H&R policy at
// a trustworthy completion, advances every open obligation from immutable raw
// intervals, and appends all corresponding Core events in one transaction.
func (repository *PostgresRepository) Process(ctx context.Context, pending PendingWork, processedAt time.Time) error {
	if pending.IntervalEventID == uuid.Nil || pending.LeaseToken == uuid.Nil || pending.Attempts < 1 || processedAt.IsZero() {
		return ErrInput
	}
	processedAt = normalizeHNRTime(processedAt)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin H&R processing: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := ledgerdb.New(tx)
	if err := queries.LockHNRPolicyTimeline(ctx); err != nil {
		return classifyHNRError("lock H&R policy timeline", err)
	}
	row, err := queries.GetClaimedHNRWorkForUpdate(ctx, ledgerdb.GetClaimedHNRWorkForUpdateParams{
		IntervalEventID: pending.IntervalEventID, LeaseToken: pending.LeaseToken,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvariant
	}
	if err != nil {
		return classifyHNRError("lock claimed H&R work", err)
	}
	raw, err := rawWorkFromRow(row)
	if err != nil || raw.EventID != pending.IntervalEventID || row.Attempts != pending.Attempts {
		return ErrInvariant
	}
	if err := queries.LockHNRAggregate(ctx, ledgerdb.LockHNRAggregateParams{UserID: raw.UserID, TorrentID: raw.TorrentID}); err != nil {
		return classifyHNRError("lock H&R aggregate", err)
	}
	if raw.CompletedTransition {
		if err := repository.ensureCompletionAssessment(ctx, queries, raw, processedAt); err != nil {
			return err
		}
	}
	open, err := queries.ListTrackingHNRObligationsForUpdate(ctx, ledgerdb.ListTrackingHNRObligationsForUpdateParams{
		UserID: raw.UserID, TorrentID: raw.TorrentID,
	})
	if err != nil {
		return classifyHNRError("list tracking H&R obligations", err)
	}
	for _, candidate := range open {
		record, err := obligationFromRow(candidate)
		if err != nil {
			return err
		}
		intervals, err := listRawIntervals(ctx, queries, record.Assessment)
		if err != nil {
			return err
		}
		progress, err := Evaluate(record.progressInput(), intervals)
		if err != nil {
			return fmt.Errorf("%w: evaluate H&R progress: %v", ErrInvariant, err)
		}
		if samePublicProgress(record, progress) {
			continue
		}
		if err := repository.advanceObligation(ctx, queries, record, progress, processedAt); err != nil {
			return err
		}
	}
	rows, err := queries.MarkHNRWorkProcessed(ctx, ledgerdb.MarkHNRWorkProcessedParams{
		ProcessedAt: hnrTimestamp(maxHNRTime(processedAt, raw.EndsAt)), IntervalEventID: pending.IntervalEventID, LeaseToken: pending.LeaseToken,
	})
	if err != nil {
		return classifyHNRError("mark H&R work processed", err)
	}
	if rows != 1 {
		return ErrInvariant
	}
	if err := tx.Commit(ctx); err != nil {
		return classifyHNRError("commit H&R processing", err)
	}
	return nil
}

func (repository *PostgresRepository) Release(ctx context.Context, pending PendingWork, availableAt time.Time, errorCode string) error {
	if pending.IntervalEventID == uuid.Nil || pending.LeaseToken == uuid.Nil || availableAt.IsZero() || !hnrErrorCodePattern.MatchString(errorCode) {
		return ErrInput
	}
	rows, err := ledgerdb.New(repository.pool).ReleaseHNRWork(ctx, ledgerdb.ReleaseHNRWorkParams{
		AvailableAt: hnrTimestamp(availableAt), LastErrorCode: errorCode,
		IntervalEventID: pending.IntervalEventID, LeaseToken: pending.LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("release H&R work: %w", err)
	}
	if rows != 1 {
		return ErrInvariant
	}
	return nil
}

// ReconcileIrrelevant terminalizes a bounded batch of legacy v1 work rows that
// cannot affect an H&R obligation. It never deletes raw evidence, completion
// work, work for a tracking obligation, or work behind a pending completion.
// Repeating the operation is safe and makes the production cleanup resumable.
func (repository *PostgresRepository) ReconcileIrrelevant(ctx context.Context, reconciledAt time.Time, batchSize int32) (int64, error) {
	if reconciledAt.IsZero() || batchSize < 1 || batchSize > 10_000 {
		return 0, ErrInput
	}
	count, err := ledgerdb.New(repository.pool).ReconcileIrrelevantHNRWork(ctx, ledgerdb.ReconcileIrrelevantHNRWorkParams{
		ReconciledAt: hnrTimestamp(reconciledAt), BatchSize: batchSize,
	})
	if err != nil {
		return 0, classifyHNRError("reconcile irrelevant H&R work", err)
	}
	if count < 0 || count > int64(batchSize) {
		return 0, ErrInvariant
	}
	return count, nil
}

func (repository *PostgresRepository) AppendRevision(ctx context.Context, revision hnrpolicy.Revision, recordedAt time.Time) (bool, error) {
	if recordedAt.IsZero() || hnrpolicy.ValidateRevision(revision) != nil {
		return false, ErrInput
	}
	// PostgreSQL timestamptz has microsecond precision. Normalize before both
	// canonical encoding and comparison so an exact operator retry cannot turn
	// into a false timeline conflict after the first database round trip.
	revision.EffectiveAt = normalizeHNRTime(revision.EffectiveAt)
	recordedAt = normalizeHNRTime(recordedAt)
	policyJSON, err := hnrpolicy.Encode(revision.Policy)
	if err != nil {
		return false, ErrInput
	}
	digest := sha256.Sum256(policyJSON)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin H&R policy append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := ledgerdb.New(tx)
	if err := queries.LockHNRPolicyTimeline(ctx); err != nil {
		return false, classifyHNRError("lock H&R policy timeline", err)
	}
	existing, err := queries.GetHNRPolicyTimelineRevision(ctx, revision.ID)
	if err == nil {
		if !sameHNRRevision(existing, revision, policyJSON, digest) {
			return false, ErrTimelineConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return false, classifyHNRError("commit duplicate H&R policy verification", err)
		}
		return false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, classifyHNRError("read H&R policy revision before append", err)
	}
	rows, err := queries.AppendHNRPolicyTimelineRevision(ctx, hnrTimelineAppendParams(revision, policyJSON, digest, recordedAt))
	if err != nil {
		return false, classifyHNRError("append H&R policy revision", err)
	}
	if rows != 1 {
		return false, ErrInvariant
	}
	if err := tx.Commit(ctx); err != nil {
		return false, classifyHNRError("commit H&R policy revision", err)
	}
	return true, nil
}

func (repository *PostgresRepository) ensureCompletionAssessment(ctx context.Context, queries *ledgerdb.Queries, raw rawWork, processedAt time.Time) error {
	existing, err := queries.GetHNRCompletionAssessmentByCompletionID(ctx, raw.CompletionID)
	if err == nil {
		if !sameCompletionAssessment(existing, raw) {
			return ErrInvariant
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return classifyHNRError("read H&R completion assessment", err)
	}
	revisions, err := repository.listPolicyRevisions(ctx, queries, raw)
	if err != nil {
		return err
	}
	revision, err := hnrpolicy.ResolveAt(hnrpolicy.Context{
		UserID: raw.UserID, TorrentID: raw.TorrentID,
		TorrentControlSequence: raw.TorrentControlSequence, SubjectControlSequence: raw.SubjectControlSequence,
		At: raw.EndsAt,
	}, revisions)
	if errors.Is(err, hnrpolicy.ErrNoCoverage) {
		return fmt.Errorf("%w: %v", ErrPolicyCoverage, err)
	}
	if errors.Is(err, hnrpolicy.ErrAmbiguous) {
		return fmt.Errorf("%w: %v", ErrTimelineConflict, err)
	}
	if err != nil {
		return fmt.Errorf("%w: resolve H&R policy: %v", ErrInvariant, err)
	}
	assessment, err := newCompletionAssessment(raw, revision, processedAt)
	if err != nil {
		return err
	}
	if err := insertCompletionAssessment(ctx, queries, assessment); err != nil {
		return err
	}
	if assessment.Policy.Mode == hnrpolicy.ModeDisabled {
		return nil
	}
	record, err := repository.newObligation(ctx, queries, assessment)
	if err != nil {
		return err
	}
	if err := insertObligation(ctx, queries, record); err != nil {
		return err
	}
	return repository.appendProjection(ctx, queries, record, record.UpdatedAt)
}

func (repository *PostgresRepository) newObligation(ctx context.Context, queries *ledgerdb.Queries, assessment completionAssessment) (obligationRecord, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return obligationRecord{}, fmt.Errorf("generate H&R obligation ID: %w", err)
	}
	record := obligationRecord{
		ID: id, Assessment: assessment, State: StateTracking,
		RawUploaded: assessment.InitialUploaded, Version: 1,
		LastEvidenceAt: assessment.CompletedAt, CreatedAt: assessment.DecidedAt, UpdatedAt: assessment.DecidedAt,
	}
	if assessment.Policy.Mode == hnrpolicy.ModeExempt {
		by := SatisfiedByExempt
		at := assessment.CompletedAt
		record.State = StateExempt
		record.SatisfiedBy = &by
		record.SatisfiedAt = &at
		record.RawRatioBasisPoints = ratioBasisPoints(record.RawUploaded, assessment.RawDownloaded)
		return record, nil
	}
	intervals, err := listRawIntervals(ctx, queries, assessment)
	if err != nil {
		return obligationRecord{}, err
	}
	progress, err := Evaluate(record.progressInput(), intervals)
	if err != nil {
		return obligationRecord{}, fmt.Errorf("%w: evaluate initial H&R progress: %v", ErrInvariant, err)
	}
	record.applyProgress(progress)
	record.UpdatedAt = maxHNRTime(assessment.DecidedAt, progress.LastEvidenceAt)
	return record, nil
}

func (repository *PostgresRepository) advanceObligation(ctx context.Context, queries *ledgerdb.Queries, record obligationRecord, progress Progress, processedAt time.Time) error {
	if record.Version == int64(^uint64(0)>>1) {
		return ErrInvariant
	}
	updated := record
	updated.Version++
	updated.applyProgress(progress)
	updated.UpdatedAt = maxHNRTime(processedAt, progress.LastEvidenceAt)
	rows, err := queries.UpdateHNRObligationProgress(ctx, ledgerdb.UpdateHNRObligationProgressParams{
		SeededSeconds: updated.SeededSeconds, RawUploaded: updated.RawUploaded,
		RawRatioBasisPoints: updated.RawRatioBasisPoints, State: string(updated.State),
		SatisfiedBy: nullableHNRSatisfiedBy(updated.SatisfiedBy), SatisfiedAt: nullableHNRTime(updated.SatisfiedAt),
		NewVersion: updated.Version, LastEvidenceAt: hnrTimestamp(updated.LastEvidenceAt), UpdatedAt: hnrTimestamp(updated.UpdatedAt),
		ID: updated.ID, ExpectedVersion: record.Version,
	})
	if err != nil {
		return classifyHNRError("update H&R obligation progress", err)
	}
	if rows != 1 {
		return ErrInvariant
	}
	return repository.appendProjection(ctx, queries, updated, updated.UpdatedAt)
}

func (repository *PostgresRepository) listPolicyRevisions(ctx context.Context, queries *ledgerdb.Queries, raw rawWork) ([]hnrpolicy.Revision, error) {
	rows, err := queries.ListHNRPolicyTimelineCandidates(ctx, ledgerdb.ListHNRPolicyTimelineCandidatesParams{
		CompletedAt: hnrTimestamp(raw.EndsAt), UserID: raw.UserID, TorrentID: raw.TorrentID,
		TorrentControlSequence: raw.TorrentControlSequence, SubjectControlSequence: raw.SubjectControlSequence,
	})
	if err != nil {
		return nil, classifyHNRError("list H&R policy candidates", err)
	}
	result := make([]hnrpolicy.Revision, len(rows))
	for index, row := range rows {
		revision, err := hnrRevisionFromRow(row)
		if err != nil {
			return nil, err
		}
		result[index] = revision
	}
	return result, nil
}

func hnrTimelineAppendParams(revision hnrpolicy.Revision, policyJSON []byte, digest [sha256.Size]byte, recordedAt time.Time) ledgerdb.AppendHNRPolicyTimelineRevisionParams {
	return ledgerdb.AppendHNRPolicyTimelineRevisionParams{
		ID: revision.ID, ScopeUserID: nullableHNRUUID(revision.Scope.UserID), ScopeTorrentID: nullableHNRInt64(revision.Scope.TorrentID),
		ScopeTorrentControlSequence: nullableHNRInt64(revision.Scope.TorrentControlSequence),
		ScopeSubjectControlSequence: nullableHNRInt64(revision.Scope.SubjectControlSequence),
		EffectiveAt:                 hnrTimestamp(revision.EffectiveAt), RuleID: revision.Policy.Rule.ID,
		RuleVersion: revision.Policy.Rule.Version, Mode: string(revision.Policy.Mode),
		RequiredSeedSeconds: revision.Policy.RequiredSeedSeconds, RequiredRatioBasisPoints: revision.Policy.RequiredRatioBasisPoints,
		AssessmentWindowSeconds: revision.Policy.AssessmentWindowSeconds, GracePeriodSeconds: revision.Policy.GracePeriodSeconds,
		MaxIntervalCreditSeconds: revision.Policy.MaxIntervalCreditSeconds, PolicyJson: string(policyJSON),
		PolicySha256: digest[:], RecordedAt: hnrTimestamp(recordedAt),
	}
}

func sameHNRRevision(row ledgerdb.SettlementHnrPolicyTimelineRevision, revision hnrpolicy.Revision, policyJSON []byte, digest [sha256.Size]byte) bool {
	return row.ID == revision.ID && row.EffectiveAt.Valid && row.EffectiveAt.Time.Equal(revision.EffectiveAt) &&
		row.RuleID == revision.Policy.Rule.ID && row.RuleVersion == revision.Policy.Rule.Version && row.Mode == string(revision.Policy.Mode) &&
		row.RequiredSeedSeconds == revision.Policy.RequiredSeedSeconds && row.RequiredRatioBasisPoints == revision.Policy.RequiredRatioBasisPoints &&
		row.AssessmentWindowSeconds == revision.Policy.AssessmentWindowSeconds && row.GracePeriodSeconds == revision.Policy.GracePeriodSeconds &&
		row.MaxIntervalCreditSeconds == revision.Policy.MaxIntervalCreditSeconds && row.PolicyJson == string(policyJSON) &&
		bytes.Equal(row.PolicySha256, digest[:]) && sameHNROptionalUUID(row.ScopeUserID, revision.Scope.UserID) &&
		sameHNROptionalInt64(row.ScopeTorrentID, revision.Scope.TorrentID) &&
		sameHNROptionalInt64(row.ScopeTorrentControlSequence, revision.Scope.TorrentControlSequence) &&
		sameHNROptionalInt64(row.ScopeSubjectControlSequence, revision.Scope.SubjectControlSequence)
}

func classifyHNRError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) &&
		((len(postgresError.Code) >= 2 && (postgresError.Code[:2] == "22" || postgresError.Code[:2] == "23")) || postgresError.Code == "P0001") {
		return fmt.Errorf("%w: %s: %v", ErrInvariant, operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func nullableHNRUUID(value *uuid.UUID) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *value, Valid: true}
}

func nullableHNRInt64(value *int64) pgtype.Int8 {
	if value == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *value, Valid: true}
}

func sameHNROptionalUUID(value pgtype.UUID, expected *uuid.UUID) bool {
	if !value.Valid || expected == nil {
		return !value.Valid && expected == nil
	}
	return uuid.UUID(value.Bytes) == *expected
}

func sameHNROptionalInt64(value pgtype.Int8, expected *int64) bool {
	if !value.Valid || expected == nil {
		return !value.Valid && expected == nil
	}
	return value.Int64 == *expected
}

func hnrTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: normalizeHNRTime(value), Valid: true}
}

func maxHNRTime(left, right time.Time) time.Time {
	if right.After(left) {
		return normalizeHNRTime(right)
	}
	return normalizeHNRTime(left)
}

func normalizeHNRTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

var _ TimelineRepository = (*PostgresRepository)(nil)
var _ WorkRepository = (*PostgresRepository)(nil)
var _ WorkReconciler = (*PostgresRepository)(nil)
