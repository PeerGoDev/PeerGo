package settler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/settlementtrafficv1"
	"github.com/peergo/peergo/services/settlement/internal/generated/ledgerdb"
	"github.com/peergo/peergo/services/settlement/internal/policy"
	"github.com/peergo/peergo/services/settlement/internal/timeline"
)

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
	row, err := ledgerdb.New(repository.pool).ClaimNextPolicyWork(ctx, ledgerdb.ClaimNextPolicyWorkParams{
		LeaseToken: leaseToken, LeaseUntil: ledgerTimestamp(now.Add(leaseDuration)), ClaimedAt: ledgerTimestamp(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PendingWork{}, false, nil
	}
	if err != nil {
		return PendingWork{}, false, fmt.Errorf("claim policy work: %w", err)
	}
	if _, err := rawFromClaim(row.IntervalEventID, row.UserID, row.TorrentID, row.StartsAt, row.EndsAt, row.RawUploaded, row.RawDownloaded, row.TorrentControlSequence, row.SubjectControlSequence,
		row.NetworkPolicySequence, row.NetworkPolicyRevision, row.NetworkClass, row.NetworkRuleID,
		row.SeedboxUploadFactorBasisPoints, row.SpeedLimitBytesPerSecond); err != nil ||
		!row.LeaseToken.Valid || uuid.UUID(row.LeaseToken.Bytes) != leaseToken || row.Attempts < 1 {
		return PendingWork{}, false, ErrInvariant
	}
	return PendingWork{IntervalEventID: row.IntervalEventID, LeaseToken: leaseToken, Attempts: row.Attempts}, true, nil
}

// Settle resolves policy and commits every final ledger row plus the Core
// outbox event in one transaction. LockPolicyTimeline serializes this with a
// policy append, so a late historical revision cannot race past this result.
func (repository *PostgresRepository) Settle(ctx context.Context, pending PendingWork, settledAt time.Time) error {
	if pending.IntervalEventID == uuid.Nil || pending.LeaseToken == uuid.Nil || pending.Attempts < 1 || settledAt.IsZero() {
		return ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin final traffic settlement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := ledgerdb.New(tx)
	if err := queries.LockPolicyTimeline(ctx); err != nil {
		return classifyDatabaseError("lock policy timeline", err)
	}
	row, err := queries.GetClaimedPolicyWorkForUpdate(ctx, ledgerdb.GetClaimedPolicyWorkForUpdateParams{
		IntervalEventID: pending.IntervalEventID, LeaseToken: pending.LeaseToken,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvariant
	}
	if err != nil {
		return classifyDatabaseError("lock policy work", err)
	}
	raw, err := rawFromClaim(row.IntervalEventID, row.UserID, row.TorrentID, row.StartsAt, row.EndsAt, row.RawUploaded, row.RawDownloaded, row.TorrentControlSequence, row.SubjectControlSequence,
		row.NetworkPolicySequence, row.NetworkPolicyRevision, row.NetworkClass, row.NetworkRuleID,
		row.SeedboxUploadFactorBasisPoints, row.SpeedLimitBytesPerSecond)
	if err != nil || raw.EventID != pending.IntervalEventID || row.Attempts != pending.Attempts {
		return ErrInvariant
	}
	revisions, err := repository.listRevisions(ctx, queries, raw)
	if err != nil {
		return err
	}
	slices, err := timeline.ResolveInterval(timeline.IntervalContext{
		UserID: raw.UserID, TorrentID: raw.TorrentID, TorrentControlSequence: raw.TorrentControlSequence,
		SubjectControlSequence: raw.SubjectControlSequence, StartsAt: raw.StartsAt, EndsAt: raw.EndsAt,
	}, revisions)
	if errors.Is(err, timeline.ErrNoCoverage) {
		return fmt.Errorf("%w: %v", ErrPolicyCoverage, err)
	}
	if err != nil {
		return fmt.Errorf("%w: resolve immutable policy timeline: %v", ErrInvariant, err)
	}
	promotionRules, err := repository.listPromotionRules(ctx, queries, raw)
	if err != nil {
		return err
	}
	slices, err = policy.ApplyPromotionRules(slices, promotionRules)
	if err != nil {
		return fmt.Errorf("%w: apply immutable promotion timeline: %v", ErrInvariant, err)
	}
	benefitTransitions, err := repository.listWorkgroupBenefitTransitions(ctx, queries, raw)
	if err != nil {
		return err
	}
	slices, err = policy.ApplyWorkgroupBenefitTransitions(slices, benefitTransitions)
	if err != nil {
		return fmt.Errorf("%w: apply immutable workgroup benefit timeline: %v", ErrInvariant, err)
	}
	vipTransitions, err := repository.listVIPBenefitTransitions(ctx, queries, raw)
	if err != nil {
		return err
	}
	slices, err = policy.ApplyVIPBenefitTransitions(slices, vipTransitions)
	if err != nil {
		return fmt.Errorf("%w: apply immutable VIP benefit timeline: %v", ErrInvariant, err)
	}
	slices, err = policy.ApplySeedboxEvidence(slices, raw.NetworkEvidence)
	if err != nil {
		return fmt.Errorf("%w: apply Tracker seedbox evidence: %v", ErrInvariant, err)
	}
	result, err := policy.SettleInterval(policy.IntervalRequest{
		StartsAt: raw.StartsAt, EndsAt: raw.EndsAt, RawUploaded: uint64(raw.RawUploaded), RawDownloaded: uint64(raw.RawDownloaded), Slices: slices,
	})
	if err != nil {
		return fmt.Errorf("%w: settle policy interval: %v", ErrInvariant, err)
	}
	observation, err := buildSpeedObservation(raw, slices, settledAt)
	if err != nil {
		return fmt.Errorf("%w: build speed observation: %v", ErrInvariant, err)
	}
	if observation != nil {
		if err := queries.InsertSpeedObservation(ctx, ledgerdb.InsertSpeedObservationParams{
			IntervalEventID:             observation.IntervalEventID,
			IntervalDurationNanoseconds: observation.IntervalDurationNanoseconds,
			RawUploaded:                 observation.RawUploaded,
			AverageUploadBytesPerSecond: observation.AverageBytesPerSecond,
			Outcome:                     observation.Outcome,
			ObservedAt:                  ledgerTimestamp(observation.ObservedAt),
		}); err != nil {
			return classifyDatabaseError("insert immutable speed observation", err)
		}
	}
	final, err := finalize(raw, slices, result, settledAt)
	if err != nil {
		return err
	}
	if err := insertFinalSettlement(ctx, queries, final); err != nil {
		return err
	}
	rows, err := queries.MarkPolicyWorkSettled(ctx, ledgerdb.MarkPolicyWorkSettledParams{
		SettledAt: ledgerTimestamp(final.SettledAt), IntervalEventID: pending.IntervalEventID, LeaseToken: pending.LeaseToken,
	})
	if err != nil {
		return classifyDatabaseError("mark policy work settled", err)
	}
	if rows != 1 {
		return ErrInvariant
	}
	if err := tx.Commit(ctx); err != nil {
		return classifyDatabaseError("commit final traffic settlement", err)
	}
	return nil
}

func (repository *PostgresRepository) listPromotionRules(ctx context.Context, queries *ledgerdb.Queries, raw rawInterval) ([]policy.PromotionRule, error) {
	rows, err := queries.ListPromotionRulesForInterval(ctx, ledgerdb.ListPromotionRulesForIntervalParams{
		IntervalEndsAt: ledgerTimestamp(raw.EndsAt), IntervalStartsAt: ledgerTimestamp(raw.StartsAt), TorrentID: raw.TorrentID,
	})
	if err != nil {
		return nil, classifyDatabaseError("list promotion rules for interval", err)
	}
	rules := make([]policy.PromotionRule, 0, len(rows))
	for _, row := range rows {
		if row.ID == uuid.Nil || !row.StartsAt.Valid || !row.EndsAt.Valid || !row.EndsAt.Time.After(row.StartsAt.Time) {
			return nil, ErrInvariant
		}
		scope := policy.ScopeGlobal
		source := policy.SourceGlobalCampaign
		if row.ScopeType == string(policy.ScopeTorrent) {
			if !row.TorrentID.Valid || row.TorrentID.Int64 != raw.TorrentID {
				return nil, ErrInvariant
			}
			scope = policy.ScopeTorrent
			source = policy.SourceTorrentPromotion
		} else if row.ScopeType != string(policy.ScopeGlobal) || row.TorrentID.Valid {
			return nil, ErrInvariant
		}
		endsAt := row.EndsAt.Time.UTC().Round(0)
		rules = append(rules, policy.PromotionRule{
			Rule: policy.RuleRef{Source: source, ID: row.ID.String(), Version: 1}, Scope: scope,
			Promotion:           policy.PromotionType(row.Promotion),
			Window:              policy.Window{StartsAt: row.StartsAt.Time.UTC().Round(0), EndsAt: &endsAt},
			OverrideLowerScopes: row.OverrideLowerScopes,
		})
	}
	return rules, nil
}

func (repository *PostgresRepository) listWorkgroupBenefitTransitions(ctx context.Context, queries *ledgerdb.Queries, raw rawInterval) ([]policy.WorkgroupBenefitTransition, error) {
	rows, err := queries.ListWorkgroupBenefitTransitionsForInterval(ctx, ledgerdb.ListWorkgroupBenefitTransitionsForIntervalParams{
		UserID: raw.UserID, IntervalEndsAt: ledgerTimestamp(raw.EndsAt),
	})
	if err != nil {
		return nil, classifyDatabaseError("list workgroup benefit transitions for interval", err)
	}
	transitions := make([]policy.WorkgroupBenefitTransition, 0, len(rows))
	for _, row := range rows {
		if row.TransitionID == uuid.Nil || row.UserID != raw.UserID ||
			row.GroupKind != "retention" || row.Entitlement != "traffic.download.charge_exempt" ||
			row.StateVersion < 1 || !row.EffectiveAt.Valid {
			return nil, ErrInvariant
		}
		transitions = append(transitions, policy.WorkgroupBenefitTransition{
			Rule: policy.RuleRef{
				Source: policy.SourceUserGroup, ID: row.TransitionID.String(), Version: uint64(row.StateVersion),
			},
			Active: row.Active, EffectiveAt: row.EffectiveAt.Time.UTC().Round(0),
		})
	}
	return transitions, nil
}

func (repository *PostgresRepository) listVIPBenefitTransitions(ctx context.Context, queries *ledgerdb.Queries, raw rawInterval) ([]policy.VIPBenefitTransition, error) {
	rows, err := queries.ListVIPBenefitTransitionsForInterval(ctx, ledgerdb.ListVIPBenefitTransitionsForIntervalParams{
		UserID: raw.UserID, IntervalEndsAt: ledgerTimestamp(raw.EndsAt),
	})
	if err != nil {
		return nil, classifyDatabaseError("list VIP benefit transitions for interval", err)
	}
	transitions := make([]policy.VIPBenefitTransition, 0, len(rows))
	for _, row := range rows {
		if row.TransitionID == uuid.Nil || row.UserID != raw.UserID ||
			row.Entitlement != "traffic.download.charge_exempt" || row.StateVersion < 1 ||
			!row.EffectiveAt.Valid || (!row.Enabled && row.ActiveUntil.Valid) {
			return nil, ErrInvariant
		}
		transition := policy.VIPBenefitTransition{
			Rule: policy.RuleRef{
				Source: policy.SourceVIP, ID: row.TransitionID.String(), Version: uint64(row.StateVersion),
			},
			Enabled: row.Enabled, EffectiveAt: row.EffectiveAt.Time.UTC().Round(0),
		}
		if row.ActiveUntil.Valid {
			value := row.ActiveUntil.Time.UTC().Round(0)
			transition.ActiveUntil = &value
		}
		transitions = append(transitions, transition)
	}
	return transitions, nil
}

func (repository *PostgresRepository) Release(ctx context.Context, pending PendingWork, availableAt time.Time, errorCode string) error {
	if pending.IntervalEventID == uuid.Nil || pending.LeaseToken == uuid.Nil || availableAt.IsZero() || !validErrorCode(errorCode) {
		return ErrInput
	}
	rows, err := ledgerdb.New(repository.pool).ReleasePolicyWork(ctx, ledgerdb.ReleasePolicyWorkParams{
		AvailableAt: ledgerTimestamp(availableAt), LastErrorCode: errorCode,
		IntervalEventID: pending.IntervalEventID, LeaseToken: pending.LeaseToken,
	})
	if err != nil {
		return fmt.Errorf("release policy work: %w", err)
	}
	if rows != 1 {
		return ErrInvariant
	}
	return nil
}

func (repository *PostgresRepository) AppendRevision(ctx context.Context, revision timeline.Revision, recordedAt time.Time) (bool, error) {
	if recordedAt.IsZero() || timeline.ValidateRevision(revision) != nil {
		return false, ErrInput
	}
	snapshotJSON, err := policy.EncodeSnapshot(revision.Snapshot)
	if err != nil {
		return false, ErrInput
	}
	snapshotDigest := sha256.Sum256(snapshotJSON)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin policy timeline append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := ledgerdb.New(tx)
	if err := queries.LockPolicyTimeline(ctx); err != nil {
		return false, classifyDatabaseError("lock policy timeline", err)
	}
	// PostgreSQL executes BEFORE INSERT triggers before it resolves an
	// ON CONFLICT branch. The ledger's historical-rewrite trigger therefore
	// correctly rejects a new retroactive revision, but would also reject an
	// exact retry after traffic has already settled. The advisory lock closes
	// the concurrent-insert race, so compare an existing immutable ID before
	// attempting INSERT and preserve command-level idempotency without
	// weakening the database trigger for genuinely new history.
	existing, err := queries.GetPolicyTimelineRevision(ctx, revision.ID)
	if err == nil {
		if !sameRevision(existing, revision, snapshotJSON, snapshotDigest) {
			return false, ErrTimelineConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return false, classifyDatabaseError("commit duplicate policy timeline verification", err)
		}
		return false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, classifyDatabaseError("read policy timeline revision before append", err)
	}
	rows, err := queries.AppendPolicyTimelineRevision(ctx, timelineAppendParams(revision, snapshotJSON, snapshotDigest, recordedAt))
	if err != nil {
		return false, classifyDatabaseError("append policy timeline revision", err)
	}
	if rows != 1 {
		return false, ErrInvariant
	}
	if err := tx.Commit(ctx); err != nil {
		return false, classifyDatabaseError("commit policy timeline revision", err)
	}
	return true, nil
}

func (repository *PostgresRepository) listRevisions(ctx context.Context, queries *ledgerdb.Queries, raw rawInterval) ([]timeline.Revision, error) {
	rows, err := queries.ListPolicyTimelineCandidates(ctx, ledgerdb.ListPolicyTimelineCandidatesParams{
		IntervalEndsAt: ledgerTimestamp(raw.EndsAt), UserID: raw.UserID, TorrentID: raw.TorrentID,
		TorrentControlSequence: raw.TorrentControlSequence, SubjectControlSequence: raw.SubjectControlSequence,
	})
	if err != nil {
		return nil, classifyDatabaseError("list policy timeline candidates", err)
	}
	result := make([]timeline.Revision, len(rows))
	for index, row := range rows {
		revision, err := revisionFromRow(row)
		if err != nil {
			return nil, err
		}
		result[index] = revision
	}
	return result, nil
}

func insertFinalSettlement(ctx context.Context, queries *ledgerdb.Queries, final finalizedSettlement) error {
	if err := queries.InsertTrafficSettlement(ctx, ledgerdb.InsertTrafficSettlementParams{
		SettlementID: final.Raw.EventID, UserID: final.Raw.UserID, TorrentID: final.Raw.TorrentID,
		TorrentControlSequence: final.Raw.TorrentControlSequence, SubjectControlSequence: final.Raw.SubjectControlSequence,
		IntervalStartsAt: ledgerTimestamp(final.Raw.StartsAt), IntervalEndsAt: ledgerTimestamp(final.Raw.EndsAt),
		RawUploaded: final.Raw.RawUploaded, RawDownloaded: final.Raw.RawDownloaded,
		CreditedUploaded: final.TrafficEvent.CreditedUploaded, ChargedDownloaded: final.TrafficEvent.ChargedDownloaded,
		SettlementSha256: final.SettlementSHA256[:], SettledAt: ledgerTimestamp(final.SettledAt), CreatedAt: ledgerTimestamp(final.SettledAt),
	}); err != nil {
		return classifyDatabaseError("insert final traffic settlement", err)
	}
	for index, segment := range final.Segments {
		if err := queries.InsertTrafficSettlementSegment(ctx, ledgerdb.InsertTrafficSettlementSegmentParams{
			SettlementID: final.Raw.EventID, SegmentIndex: int32(index), StartsAt: ledgerTimestamp(segment.StartsAt), EndsAt: ledgerTimestamp(segment.EndsAt),
			PolicyRevisionSource: string(segment.PolicyRevision.Source), PolicyRevisionID: segment.PolicyRevision.ID,
			PolicyRevisionVersion: int64(segment.PolicyRevision.Version), PolicyProfile: string(segment.Profile),
			PolicySnapshotSha256: segment.PolicySnapshotSHA256[:], ApplicationsJson: string(segment.ApplicationsJSON), ApplicationsSha256: segment.ApplicationsSHA256[:],
			RawUploaded: segment.RawUploaded, RawDownloaded: segment.RawDownloaded,
			CreditedUploaded: segment.CreditedUploaded, ChargedDownloaded: segment.ChargedDownloaded,
		}); err != nil {
			return classifyDatabaseError("insert final traffic settlement segment", err)
		}
	}
	if err := queries.AppendTrafficOutboxEvent(ctx, ledgerdb.AppendTrafficOutboxEventParams{
		EventID: final.Raw.EventID, SettlementID: final.Raw.EventID, EventType: "settlement.traffic.credited",
		SchemaVersion: settlementtrafficv1.SchemaVersion, OccurredAt: ledgerTimestamp(final.TrafficEvent.OccurredAt),
		PayloadJson: string(final.TrafficPayload), PayloadSha256: final.TrafficSHA256[:],
		AvailableAt: ledgerTimestamp(final.SettledAt), CreatedAt: ledgerTimestamp(final.SettledAt),
	}); err != nil {
		return classifyDatabaseError("append final traffic outbox event", err)
	}
	return nil
}

func rawFromClaim(
	eventID, userID uuid.UUID,
	torrentID int64,
	startsAt, endsAt pgtype.Timestamptz,
	rawUploaded, rawDownloaded, torrentSequence, subjectSequence int64,
	networkSequence pgtype.Int8,
	networkRevision, networkClass, networkRuleID pgtype.Text,
	uploadFactor pgtype.Int4,
	speedLimit pgtype.Int8,
) (rawInterval, error) {
	if eventID == uuid.Nil || userID == uuid.Nil || torrentID < 1 || !startsAt.Valid || !endsAt.Valid ||
		!endsAt.Time.After(startsAt.Time) || rawUploaded < 0 || rawDownloaded < 0 || torrentSequence < 1 || subjectSequence < 1 {
		return rawInterval{}, ErrInvariant
	}
	var networkEvidence *policy.NetworkEvidence
	hasAnyNetworkEvidence := networkSequence.Valid || networkRevision.Valid || networkClass.Valid || networkRuleID.Valid || uploadFactor.Valid || speedLimit.Valid
	if hasAnyNetworkEvidence {
		if !networkSequence.Valid || networkSequence.Int64 < 1 || !networkRevision.Valid || !networkClass.Valid ||
			!uploadFactor.Valid || uploadFactor.Int32 < 0 || uploadFactor.Int32 > 10_000 || !speedLimit.Valid || speedLimit.Int64 < 0 ||
			(networkClass.String == policy.NetworkClassStandard && (networkRuleID.Valid || uploadFactor.Int32 != 10_000)) ||
			(networkClass.String == policy.NetworkClassSeedbox && (!networkRuleID.Valid || networkRuleID.String == "")) {
			return rawInterval{}, ErrInvariant
		}
		networkEvidence = &policy.NetworkEvidence{
			PolicySequence: uint64(networkSequence.Int64), PolicyRevision: networkRevision.String,
			Class: networkClass.String, UploadFactorBasisPoints: policy.BasisPoints(uploadFactor.Int32),
			SpeedLimitBytesPerSecond: speedLimit.Int64,
		}
		if networkRuleID.Valid {
			networkEvidence.RuleID = networkRuleID.String
		}
	}
	return rawInterval{
		EventID: eventID, UserID: userID, TorrentID: torrentID, StartsAt: startsAt.Time.UTC().Round(0), EndsAt: endsAt.Time.UTC().Round(0),
		RawUploaded: rawUploaded, RawDownloaded: rawDownloaded, TorrentControlSequence: torrentSequence, SubjectControlSequence: subjectSequence,
		NetworkEvidence: networkEvidence,
	}, nil
}

func revisionFromRow(row ledgerdb.SettlementPolicyTimelineRevision) (timeline.Revision, error) {
	if row.ID == uuid.Nil || !row.EffectiveAt.Valid || row.RevisionSource != string(policy.SourcePolicyRevision) ||
		row.RevisionID == "" || row.RevisionVersion < 1 || len(row.SnapshotSha256) != sha256.Size {
		return timeline.Revision{}, ErrInvariant
	}
	snapshot, err := policy.DecodeSnapshot([]byte(row.SnapshotJson))
	if err != nil || snapshot.Revision.Source != policy.SourcePolicyRevision || snapshot.Revision.ID != row.RevisionID ||
		snapshot.Revision.Version != uint64(row.RevisionVersion) || string(snapshot.Profile) != row.Profile {
		return timeline.Revision{}, ErrInvariant
	}
	digest, err := policy.SnapshotSHA256(snapshot)
	if err != nil || !bytes.Equal(digest[:], row.SnapshotSha256) {
		return timeline.Revision{}, ErrInvariant
	}
	scope, err := scopeFromRow(row)
	if err != nil {
		return timeline.Revision{}, err
	}
	revision := timeline.Revision{ID: row.ID, Scope: scope, EffectiveAt: row.EffectiveAt.Time.UTC().Round(0), Snapshot: snapshot}
	if timeline.ValidateRevision(revision) != nil {
		return timeline.Revision{}, ErrInvariant
	}
	return revision, nil
}

func scopeFromRow(row ledgerdb.SettlementPolicyTimelineRevision) (timeline.Scope, error) {
	userID, err := optionalUUID(row.ScopeUserID)
	if err != nil {
		return timeline.Scope{}, err
	}
	return timeline.Scope{
		UserID: userID, TorrentID: optionalInt64(row.ScopeTorrentID),
		TorrentControlSequence: optionalInt64(row.ScopeTorrentControlSequence), SubjectControlSequence: optionalInt64(row.ScopeSubjectControlSequence),
	}, nil
}

func timelineAppendParams(revision timeline.Revision, snapshotJSON []byte, digest [sha256.Size]byte, recordedAt time.Time) ledgerdb.AppendPolicyTimelineRevisionParams {
	return ledgerdb.AppendPolicyTimelineRevisionParams{
		ID: revision.ID, ScopeUserID: nullableUUID(revision.Scope.UserID), ScopeTorrentID: nullableInt64(revision.Scope.TorrentID),
		ScopeTorrentControlSequence: nullableInt64(revision.Scope.TorrentControlSequence),
		ScopeSubjectControlSequence: nullableInt64(revision.Scope.SubjectControlSequence),
		EffectiveAt:                 ledgerTimestamp(revision.EffectiveAt), RevisionSource: string(revision.Snapshot.Revision.Source),
		RevisionID: revision.Snapshot.Revision.ID, RevisionVersion: int64(revision.Snapshot.Revision.Version), Profile: string(revision.Snapshot.Profile),
		SnapshotJson: string(snapshotJSON), SnapshotSha256: digest[:], RecordedAt: ledgerTimestamp(recordedAt),
	}
}

func sameRevision(row ledgerdb.SettlementPolicyTimelineRevision, revision timeline.Revision, snapshotJSON []byte, digest [sha256.Size]byte) bool {
	if row.ID != revision.ID || !row.EffectiveAt.Valid || !row.EffectiveAt.Time.Equal(revision.EffectiveAt) ||
		row.RevisionSource != string(revision.Snapshot.Revision.Source) || row.RevisionID != revision.Snapshot.Revision.ID ||
		row.RevisionVersion != int64(revision.Snapshot.Revision.Version) || row.Profile != string(revision.Snapshot.Profile) ||
		row.SnapshotJson != string(snapshotJSON) || !bytes.Equal(row.SnapshotSha256, digest[:]) {
		return false
	}
	return sameOptionalUUID(row.ScopeUserID, revision.Scope.UserID) &&
		sameOptionalInt64(row.ScopeTorrentID, revision.Scope.TorrentID) &&
		sameOptionalInt64(row.ScopeTorrentControlSequence, revision.Scope.TorrentControlSequence) &&
		sameOptionalInt64(row.ScopeSubjectControlSequence, revision.Scope.SubjectControlSequence)
}

func nullableUUID(value *uuid.UUID) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *value, Valid: true}
}

func optionalUUID(value pgtype.UUID) (*uuid.UUID, error) {
	if !value.Valid {
		return nil, nil
	}
	result := uuid.UUID(value.Bytes)
	if result == uuid.Nil {
		return nil, ErrInvariant
	}
	return &result, nil
}

func nullableInt64(value *int64) pgtype.Int8 {
	if value == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *value, Valid: true}
}

func optionalInt64(value pgtype.Int8) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func sameOptionalUUID(value pgtype.UUID, expected *uuid.UUID) bool {
	actual, err := optionalUUID(value)
	if err != nil {
		return false
	}
	if actual == nil || expected == nil {
		return actual == nil && expected == nil
	}
	return *actual == *expected
}

func sameOptionalInt64(value pgtype.Int8, expected *int64) bool {
	actual := optionalInt64(value)
	if actual == nil || expected == nil {
		return actual == nil && expected == nil
	}
	return *actual == *expected
}

func ledgerTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC().Round(0), Valid: true}
}

func classifyDatabaseError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) &&
		(len(postgresError.Code) >= 2 && (postgresError.Code[:2] == "22" || postgresError.Code[:2] == "23") || postgresError.Code == "P0001") {
		return fmt.Errorf("%w: %s: %v", ErrInvariant, operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
