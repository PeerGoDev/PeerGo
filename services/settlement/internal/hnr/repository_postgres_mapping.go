package hnr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
	"github.com/peergo/peergo/services/settlement/internal/hnrpolicy"
	"github.com/peergo/peergo/services/settlement/internal/timeline"
)

type rawWork struct {
	EventID                   uuid.UUID
	UserID                    uuid.UUID
	TorrentID                 int64
	StartsAt                  time.Time
	EndsAt                    time.Time
	TorrentControlSequence    int64
	SubjectControlSequence    int64
	CurrentUploaded           int64
	CurrentDownloaded         int64
	CompletedTransition       bool
	CompletionID              []byte
	CompletionIdentityVersion int16
}

type completionAssessment struct {
	ID                     uuid.UUID
	CompletionID           []byte
	CompletionEventID      uuid.UUID
	UserID                 uuid.UUID
	TorrentID              int64
	TorrentControlSequence int64
	SubjectControlSequence int64
	CompletedAt            time.Time
	PolicyRevisionID       uuid.UUID
	Policy                 hnrpolicy.Policy
	PolicySHA256           [sha256.Size]byte
	AssessmentDueAt        time.Time
	GraceEndsAt            time.Time
	InitialUploaded        int64
	RawDownloaded          int64
	DecidedAt              time.Time
}

type obligationRecord struct {
	ID                  uuid.UUID
	Assessment          completionAssessment
	SeededSeconds       int64
	RawUploaded         int64
	RawRatioBasisPoints int64
	State               State
	SatisfiedBy         *SatisfiedBy
	SatisfiedAt         *time.Time
	Version             int64
	LastEvidenceAt      time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func rawWorkFromRow(row ledgerdb.GetClaimedHNRWorkForUpdateRow) (rawWork, error) {
	if row.IntervalEventID == uuid.Nil || row.UserID == uuid.Nil || row.TorrentID < 1 ||
		!row.StartsAt.Valid || !row.EndsAt.Valid || !row.EndsAt.Time.After(row.StartsAt.Time) ||
		row.TorrentControlSequence < 1 || row.SubjectControlSequence < 1 ||
		row.CurrentUploaded < 0 || row.CurrentDownloaded < 0 || row.CompletionIdentityVersion < 0 ||
		row.CompletionIdentityVersion > 1 {
		return rawWork{}, ErrInvariant
	}
	eligibleCompletion := false
	if row.CompletionIdentityVersion == 0 {
		if len(row.CompletionID) != 0 {
			return rawWork{}, ErrInvariant
		}
	} else {
		if row.CompletedTransition != (len(row.CompletionID) == sha256.Size) {
			return rawWork{}, ErrInvariant
		}
		eligibleCompletion = row.CompletedTransition
	}
	completionID := append([]byte(nil), row.CompletionID...)
	return rawWork{
		EventID: row.IntervalEventID, UserID: row.UserID, TorrentID: row.TorrentID,
		StartsAt: row.StartsAt.Time.UTC().Round(0), EndsAt: row.EndsAt.Time.UTC().Round(0),
		TorrentControlSequence: row.TorrentControlSequence, SubjectControlSequence: row.SubjectControlSequence,
		CurrentUploaded: row.CurrentUploaded, CurrentDownloaded: row.CurrentDownloaded,
		CompletedTransition: eligibleCompletion, CompletionID: completionID,
		CompletionIdentityVersion: row.CompletionIdentityVersion,
	}, nil
}

func newCompletionAssessment(raw rawWork, revision hnrpolicy.Revision, processedAt time.Time) (completionAssessment, error) {
	if !raw.CompletedTransition || len(raw.CompletionID) != sha256.Size || hnrpolicy.ValidateRevision(revision) != nil || processedAt.IsZero() {
		return completionAssessment{}, ErrInvariant
	}
	id, err := uuid.NewV7()
	if err != nil {
		return completionAssessment{}, fmt.Errorf("generate H&R assessment ID: %w", err)
	}
	digest, err := hnrpolicy.SHA256(revision.Policy)
	if err != nil {
		return completionAssessment{}, ErrInvariant
	}
	completedAt := raw.EndsAt.UTC().Round(0)
	dueAt := completedAt
	graceEndsAt := completedAt
	if revision.Policy.Mode == hnrpolicy.ModeEnforced {
		dueAt = completedAt.Add(time.Duration(revision.Policy.AssessmentWindowSeconds) * time.Second)
		graceEndsAt = dueAt.Add(time.Duration(revision.Policy.GracePeriodSeconds) * time.Second)
	}
	return completionAssessment{
		ID: id, CompletionID: append([]byte(nil), raw.CompletionID...), CompletionEventID: raw.EventID,
		UserID: raw.UserID, TorrentID: raw.TorrentID, TorrentControlSequence: raw.TorrentControlSequence,
		SubjectControlSequence: raw.SubjectControlSequence, CompletedAt: completedAt,
		PolicyRevisionID: revision.ID, Policy: revision.Policy, PolicySHA256: digest,
		AssessmentDueAt: dueAt, GraceEndsAt: graceEndsAt,
		InitialUploaded: raw.CurrentUploaded, RawDownloaded: raw.CurrentDownloaded,
		DecidedAt: maxHNRTime(processedAt, completedAt),
	}, nil
}

func insertCompletionAssessment(ctx context.Context, queries *ledgerdb.Queries, assessment completionAssessment) error {
	if err := queries.InsertHNRCompletionAssessment(ctx, ledgerdb.InsertHNRCompletionAssessmentParams{
		ID: assessment.ID, CompletionID: assessment.CompletionID, CompletionEventID: assessment.CompletionEventID,
		UserID: assessment.UserID, TorrentID: assessment.TorrentID,
		TorrentControlSequence: assessment.TorrentControlSequence, SubjectControlSequence: assessment.SubjectControlSequence,
		CompletedAt: hnrTimestamp(assessment.CompletedAt), PolicyRevisionID: assessment.PolicyRevisionID,
		PolicyRuleID: assessment.Policy.Rule.ID, PolicyRuleVersion: assessment.Policy.Rule.Version,
		PolicySha256: assessment.PolicySHA256[:], PolicyMode: string(assessment.Policy.Mode),
		RequiredSeedSeconds:      assessment.Policy.RequiredSeedSeconds,
		RequiredRatioBasisPoints: assessment.Policy.RequiredRatioBasisPoints,
		MaxIntervalCreditSeconds: assessment.Policy.MaxIntervalCreditSeconds,
		AssessmentDueAt:          hnrTimestamp(assessment.AssessmentDueAt), GraceEndsAt: hnrTimestamp(assessment.GraceEndsAt),
		InitialUploaded: assessment.InitialUploaded, RawDownloaded: assessment.RawDownloaded,
		DecidedAt: hnrTimestamp(assessment.DecidedAt),
	}); err != nil {
		return classifyHNRError("insert H&R completion assessment", err)
	}
	return nil
}

func insertObligation(ctx context.Context, queries *ledgerdb.Queries, record obligationRecord) error {
	if err := queries.InsertHNRObligation(ctx, ledgerdb.InsertHNRObligationParams{
		ID: record.ID, AssessmentID: record.Assessment.ID,
		SeededSeconds: record.SeededSeconds, RawUploaded: record.RawUploaded,
		RawRatioBasisPoints: record.RawRatioBasisPoints, State: string(record.State),
		SatisfiedBy: nullableHNRSatisfiedBy(record.SatisfiedBy), SatisfiedAt: nullableHNRTime(record.SatisfiedAt),
		Version: record.Version, LastEvidenceAt: hnrTimestamp(record.LastEvidenceAt),
		CreatedAt: hnrTimestamp(record.CreatedAt), UpdatedAt: hnrTimestamp(record.UpdatedAt),
	}); err != nil {
		return classifyHNRError("insert H&R obligation", err)
	}
	return nil
}

func hnrRevisionFromRow(row ledgerdb.SettlementHnrPolicyTimelineRevision) (hnrpolicy.Revision, error) {
	if row.ID == uuid.Nil || !row.EffectiveAt.Valid || len(row.PolicySha256) != sha256.Size {
		return hnrpolicy.Revision{}, ErrInvariant
	}
	policy, err := hnrpolicy.Decode([]byte(row.PolicyJson))
	if err != nil || policy.Rule.ID != row.RuleID || policy.Rule.Version != row.RuleVersion ||
		string(policy.Mode) != row.Mode || policy.RequiredSeedSeconds != row.RequiredSeedSeconds ||
		policy.RequiredRatioBasisPoints != row.RequiredRatioBasisPoints ||
		policy.AssessmentWindowSeconds != row.AssessmentWindowSeconds || policy.GracePeriodSeconds != row.GracePeriodSeconds ||
		policy.MaxIntervalCreditSeconds != row.MaxIntervalCreditSeconds {
		return hnrpolicy.Revision{}, ErrInvariant
	}
	digest, err := hnrpolicy.SHA256(policy)
	if err != nil || !bytes.Equal(digest[:], row.PolicySha256) {
		return hnrpolicy.Revision{}, ErrInvariant
	}
	scope, err := hnrScopeFromRow(row)
	if err != nil {
		return hnrpolicy.Revision{}, err
	}
	revision := hnrpolicy.Revision{ID: row.ID, Scope: scope, EffectiveAt: row.EffectiveAt.Time.UTC().Round(0), Policy: policy}
	if hnrpolicy.ValidateRevision(revision) != nil {
		return hnrpolicy.Revision{}, ErrInvariant
	}
	return revision, nil
}

func hnrScopeFromRow(row ledgerdb.SettlementHnrPolicyTimelineRevision) (timeline.Scope, error) {
	userID, err := optionalHNRUUID(row.ScopeUserID)
	if err != nil {
		return timeline.Scope{}, err
	}
	return timeline.Scope{
		UserID: userID, TorrentID: optionalHNRInt64(row.ScopeTorrentID),
		TorrentControlSequence: optionalHNRInt64(row.ScopeTorrentControlSequence),
		SubjectControlSequence: optionalHNRInt64(row.ScopeSubjectControlSequence),
	}, nil
}

func assessmentFromRow(row ledgerdb.LedgerHnrCompletionAssessment) (completionAssessment, error) {
	policy := hnrpolicy.Policy{
		Rule: hnrpolicy.RuleRef{ID: row.PolicyRuleID, Version: row.PolicyRuleVersion}, Mode: hnrpolicy.Mode(row.PolicyMode),
		RequiredSeedSeconds: row.RequiredSeedSeconds, RequiredRatioBasisPoints: row.RequiredRatioBasisPoints,
		MaxIntervalCreditSeconds: row.MaxIntervalCreditSeconds,
	}
	if !row.CompletedAt.Valid || !row.AssessmentDueAt.Valid || !row.GraceEndsAt.Valid || !row.DecidedAt.Valid {
		return completionAssessment{}, ErrInvariant
	}
	if policy.Mode == hnrpolicy.ModeEnforced {
		policy.AssessmentWindowSeconds = int64(row.AssessmentDueAt.Time.Sub(row.CompletedAt.Time) / time.Second)
		policy.GracePeriodSeconds = int64(row.GraceEndsAt.Time.Sub(row.AssessmentDueAt.Time) / time.Second)
	}
	if row.ID == uuid.Nil || len(row.CompletionID) != sha256.Size || row.CompletionEventID == uuid.Nil ||
		row.UserID == uuid.Nil || row.TorrentID < 1 || row.TorrentControlSequence < 1 || row.SubjectControlSequence < 1 ||
		row.PolicyRevisionID == uuid.Nil || len(row.PolicySha256) != sha256.Size || row.InitialUploaded < 0 || row.RawDownloaded < 0 ||
		hnrpolicy.ValidatePolicy(policy) != nil {
		return completionAssessment{}, ErrInvariant
	}
	digest, err := hnrpolicy.SHA256(policy)
	if err != nil || !bytes.Equal(digest[:], row.PolicySha256) {
		return completionAssessment{}, ErrInvariant
	}
	return completionAssessment{
		ID: row.ID, CompletionID: append([]byte(nil), row.CompletionID...), CompletionEventID: row.CompletionEventID,
		UserID: row.UserID, TorrentID: row.TorrentID, TorrentControlSequence: row.TorrentControlSequence,
		SubjectControlSequence: row.SubjectControlSequence, CompletedAt: row.CompletedAt.Time.UTC().Round(0),
		PolicyRevisionID: row.PolicyRevisionID, Policy: policy, PolicySHA256: digest,
		AssessmentDueAt: row.AssessmentDueAt.Time.UTC().Round(0), GraceEndsAt: row.GraceEndsAt.Time.UTC().Round(0),
		InitialUploaded: row.InitialUploaded, RawDownloaded: row.RawDownloaded, DecidedAt: row.DecidedAt.Time.UTC().Round(0),
	}, nil
}

func sameCompletionAssessment(row ledgerdb.LedgerHnrCompletionAssessment, raw rawWork) bool {
	assessment, err := assessmentFromRow(row)
	// completion_id identifies the Swarm completion transition, not one
	// delivery event. A Tracker retry can therefore have a new event UUID,
	// timestamp or control sequence while carrying the same completion_id.
	// Keep the first immutable assessment authoritative and accept the replay
	// only when its cryptographic identity and accounting subject still match.
	return err == nil && bytes.Equal(assessment.CompletionID, raw.CompletionID) &&
		assessment.UserID == raw.UserID && assessment.TorrentID == raw.TorrentID
}

func obligationFromRow(row ledgerdb.ListTrackingHNRObligationsForUpdateRow) (obligationRecord, error) {
	assessment, err := assessmentFromJoinedRow(row)
	if err != nil {
		return obligationRecord{}, err
	}
	if row.ObligationID == uuid.Nil || row.AssessmentID != assessment.ID || row.State != string(StateTracking) ||
		row.Version < 1 || row.SeededSeconds < 0 || row.RawUploaded < assessment.InitialUploaded ||
		row.RawRatioBasisPoints < 0 || !row.LastEvidenceAt.Valid || !row.CreatedAt.Valid || !row.UpdatedAt.Valid ||
		row.SatisfiedBy.Valid || row.SatisfiedAt.Valid {
		return obligationRecord{}, ErrInvariant
	}
	return obligationRecord{
		ID: row.ObligationID, Assessment: assessment, SeededSeconds: row.SeededSeconds,
		RawUploaded: row.RawUploaded, RawRatioBasisPoints: row.RawRatioBasisPoints,
		State: StateTracking, Version: row.Version, LastEvidenceAt: row.LastEvidenceAt.Time.UTC().Round(0),
		CreatedAt: row.CreatedAt.Time.UTC().Round(0), UpdatedAt: row.UpdatedAt.Time.UTC().Round(0),
	}, nil
}

func assessmentFromJoinedRow(row ledgerdb.ListTrackingHNRObligationsForUpdateRow) (completionAssessment, error) {
	return assessmentFromRow(ledgerdb.LedgerHnrCompletionAssessment{
		ID: row.AssessmentID, CompletionID: row.CompletionID, CompletionEventID: row.CompletionEventID,
		UserID: row.UserID, TorrentID: row.TorrentID, TorrentControlSequence: row.TorrentControlSequence,
		SubjectControlSequence: row.SubjectControlSequence, CompletedAt: row.CompletedAt,
		PolicyRevisionID: row.PolicyRevisionID, PolicyRuleID: row.PolicyRuleID, PolicyRuleVersion: row.PolicyRuleVersion,
		PolicySha256: row.PolicySha256, PolicyMode: row.PolicyMode, RequiredSeedSeconds: row.RequiredSeedSeconds,
		RequiredRatioBasisPoints: row.RequiredRatioBasisPoints, MaxIntervalCreditSeconds: row.MaxIntervalCreditSeconds,
		AssessmentDueAt: row.AssessmentDueAt, GraceEndsAt: row.GraceEndsAt,
		InitialUploaded: row.InitialUploaded, RawDownloaded: row.RawDownloaded, DecidedAt: row.DecidedAt,
	})
}

func listRawIntervals(ctx context.Context, queries *ledgerdb.Queries, assessment completionAssessment) ([]RawInterval, error) {
	rows, err := queries.ListHNRRawIntervals(ctx, ledgerdb.ListHNRRawIntervalsParams{
		UserID: assessment.UserID, TorrentID: assessment.TorrentID, CompletedAt: hnrTimestamp(assessment.CompletedAt),
	})
	if err != nil {
		return nil, classifyHNRError("list raw H&R intervals", err)
	}
	result := make([]RawInterval, len(rows))
	for index, row := range rows {
		if row.EventID == uuid.Nil || !row.StartsAt.Valid || !row.EndsAt.Valid || !row.EndsAt.Time.After(row.StartsAt.Time) ||
			row.PreviousLeft < 0 || row.CurrentLeft < 0 || row.RawUploaded < 0 {
			return nil, ErrInvariant
		}
		result[index] = RawInterval{
			EventID: row.EventID, StartsAt: row.StartsAt.Time.UTC().Round(0), EndsAt: row.EndsAt.Time.UTC().Round(0),
			PreviousLeft: row.PreviousLeft, CurrentLeft: row.CurrentLeft, RawUploaded: row.RawUploaded,
		}
	}
	return result, nil
}

func (record obligationRecord) progressInput() Obligation {
	return Obligation{
		ID: record.ID, CompletedAt: record.Assessment.CompletedAt,
		InitialUploaded: record.Assessment.InitialUploaded, RawDownloaded: record.Assessment.RawDownloaded,
		RequiredSeedSeconds:      record.Assessment.Policy.RequiredSeedSeconds,
		RequiredRatioBasisPoints: record.Assessment.Policy.RequiredRatioBasisPoints,
		MaxIntervalCreditSeconds: record.Assessment.Policy.MaxIntervalCreditSeconds,
		State:                    StateTracking, SeededSeconds: record.SeededSeconds, RawUploaded: record.RawUploaded,
		RawRatioBasisPoints: record.RawRatioBasisPoints, LastEvidenceAt: record.LastEvidenceAt,
	}
}

func (record *obligationRecord) applyProgress(progress Progress) {
	record.State = progress.State
	record.SeededSeconds = progress.SeededSeconds
	record.RawUploaded = progress.RawUploaded
	record.RawRatioBasisPoints = progress.RawRatioBasisPoints
	record.SatisfiedBy = progress.SatisfiedBy
	record.SatisfiedAt = progress.SatisfiedAt
	record.LastEvidenceAt = progress.LastEvidenceAt
}

func samePublicProgress(record obligationRecord, progress Progress) bool {
	return record.State == progress.State && record.SeededSeconds == progress.SeededSeconds &&
		record.RawUploaded == progress.RawUploaded && record.RawRatioBasisPoints == progress.RawRatioBasisPoints &&
		sameSatisfiedBy(record.SatisfiedBy, progress.SatisfiedBy) && sameOptionalTime(record.SatisfiedAt, progress.SatisfiedAt)
}

func sameSatisfiedBy(left, right *SatisfiedBy) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func optionalHNRUUID(value pgtype.UUID) (*uuid.UUID, error) {
	if !value.Valid {
		return nil, nil
	}
	result := uuid.UUID(value.Bytes)
	if result == uuid.Nil {
		return nil, ErrInvariant
	}
	return &result, nil
}

func optionalHNRInt64(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func nullableHNRSatisfiedBy(value *SatisfiedBy) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: string(*value), Valid: true}
}

func nullableHNRTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return hnrTimestamp(*value)
}
