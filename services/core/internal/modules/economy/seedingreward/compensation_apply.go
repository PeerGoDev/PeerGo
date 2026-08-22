package seedingreward

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/peergo/peergo/services/core/internal/modules/economy"
	"github.com/peergo/peergo/services/core/internal/modules/progression"
)

var (
	compensationTransactionNamespace = uuid.MustParse("11827521-380b-596a-becb-5b87e64a1741")
	compensationExperienceNamespace  = uuid.MustParse("b14ab468-3302-54b5-bcd8-f734e8a8db2e")
	compensationOperatorPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9:._-]{0,127}$`)
)

type CompensationApplyProgress struct {
	ProcessedRecords int64
	RecordCount      int64
	AppliedRecords   int64
	ReplayedRecords  int64
}

type CompensationApplyResult struct {
	ArtifactSHA256  string `json:"artifact_sha256"`
	RecordCount     int64  `json:"record_count"`
	AppliedRecords  int64  `json:"applied_records"`
	ReplayedRecords int64  `json:"replayed_records"`
	MagicDelta      int64  `json:"magic_delta"`
	ExperienceDelta string `json:"experience_delta"`
	Completed       bool   `json:"completed"`
}

type compensationApproval struct {
	ApprovedAt time.Time
}

type compensationPostingDocument struct {
	SchemaVersion              string `json:"schema_version"`
	ArtifactSHA256             string `json:"artifact_sha256"`
	SourceReference            string `json:"source_reference"`
	WindowStart                string `json:"window_start"`
	UserID                     string `json:"user_id"`
	PolicyRevision             string `json:"policy_revision"`
	BenefitRevision            string `json:"benefit_revision"`
	OriginalCalculationSHA256  string `json:"original_calculation_sha256,omitempty"`
	CorrectedCalculationSHA256 string `json:"corrected_calculation_sha256"`
	CorrectedEvidenceSHA256    string `json:"corrected_evidence_sha256"`
	OriginalReward             int64  `json:"original_reward"`
	CorrectedReward            int64  `json:"corrected_reward"`
	MagicDelta                 int64  `json:"magic_delta"`
	ExperienceDelta            string `json:"experience_delta"`
}

// ApplyHistoricalCompensation records only positive deltas from one exact,
// operator-approved artifact. The ordinary economy and progression kernels
// remain the sole balance writers. Work is committed in bounded batches so a
// crash can resume through source-unique immutable receipts.
func (repository *PostgresSettlementRepository) ApplyHistoricalCompensation(
	ctx context.Context,
	artifact CompensationArtifact,
	artifactSHA256 [sha256.Size]byte,
	artifactSizeBytes int64,
	operatorReference string,
	approvedAt time.Time,
	batchSize int,
	progress func(CompensationApplyProgress),
) (CompensationApplyResult, error) {
	operatorReference = strings.TrimSpace(operatorReference)
	approvedAt = canonicalTime(approvedAt)
	if repository == nil || repository.pool == nil || artifactSHA256 == ([sha256.Size]byte{}) ||
		artifactSizeBytes < 1 || !compensationOperatorPattern.MatchString(operatorReference) ||
		approvedAt.IsZero() || batchSize < 1 || batchSize > 250 {
		return CompensationApplyResult{}, ErrInput
	}
	if err := validateArtifactForApply(artifact); err != nil {
		return CompensationApplyResult{}, err
	}
	lastWindow, _ := time.Parse(time.RFC3339, artifact.Header.LastWindow)
	if approvedAt.Before(lastWindow.Add(WindowDuration)) {
		return CompensationApplyResult{}, ErrInput
	}

	approval, err := repository.ensureCompensationApproval(
		ctx, artifact, artifactSHA256, artifactSizeBytes, operatorReference, approvedAt,
	)
	if err != nil {
		return CompensationApplyResult{}, err
	}
	result := CompensationApplyResult{
		ArtifactSHA256: hex.EncodeToString(artifactSHA256[:]), RecordCount: artifact.RecordCount,
		MagicDelta: artifact.MagicDelta, ExperienceDelta: artifact.ExperienceDelta,
	}
	completed, err := repository.compensationCompletionMatches(ctx, artifact, artifactSHA256)
	if err != nil {
		return CompensationApplyResult{}, err
	}
	if completed {
		result.ReplayedRecords, result.Completed = artifact.RecordCount, true
		return result, nil
	}

	for start := 0; start < len(artifact.Records); start += batchSize {
		end := start + batchSize
		if end > len(artifact.Records) {
			end = len(artifact.Records)
		}
		applied, replayed, err := repository.applyCompensationBatch(
			ctx, artifactSHA256, approval, artifact.Records[start:end], time.Now().UTC(),
		)
		if err != nil {
			return CompensationApplyResult{}, fmt.Errorf("apply compensation records %d-%d: %w", start+1, end, err)
		}
		result.AppliedRecords += applied
		result.ReplayedRecords += replayed
		if progress != nil {
			progress(CompensationApplyProgress{
				ProcessedRecords: int64(end), RecordCount: artifact.RecordCount,
				AppliedRecords: result.AppliedRecords, ReplayedRecords: result.ReplayedRecords,
			})
		}
	}
	if err := repository.completeCompensation(ctx, artifact, artifactSHA256); err != nil {
		return CompensationApplyResult{}, err
	}
	result.Completed = true
	return result, nil
}

func validateArtifactForApply(artifact CompensationArtifact) error {
	if err := validateCompensationHeader(artifact.Header); err != nil {
		return err
	}
	if artifact.RecordCount != int64(len(artifact.Records)) || artifact.RecordCount < 1 ||
		artifact.RecordCount > compensationMaximumArtifactRecords || artifact.MagicDelta < 1 {
		return ErrInvariant
	}
	magicTotal, experienceUnits := int64(0), int64(0)
	seenSources := make(map[string]struct{}, len(artifact.Records))
	seenCalculations := make(map[string]struct{}, len(artifact.Records))
	windowEvidence := make(map[string]string)
	var previousWindow time.Time
	var previousUser uuid.UUID
	for _, record := range artifact.Records {
		window, userID, units, err := validateCompensationRecord(artifact.Header, record)
		if err != nil || (!previousWindow.IsZero() && (window.Before(previousWindow) ||
			(window.Equal(previousWindow) && bytes.Compare(userID[:], previousUser[:]) <= 0))) {
			return ErrInvariant
		}
		previousWindow, previousUser = window, userID
		if _, exists := seenSources[record.SourceReference]; exists {
			return ErrInvariant
		}
		seenSources[record.SourceReference] = struct{}{}
		if _, exists := seenCalculations[record.CorrectedCalculationSHA256]; exists {
			return ErrInvariant
		}
		seenCalculations[record.CorrectedCalculationSHA256] = struct{}{}
		if digest, exists := windowEvidence[record.WindowStart]; exists && digest != record.CorrectedEvidenceSHA256 {
			return ErrInvariant
		}
		windowEvidence[record.WindowStart] = record.CorrectedEvidenceSHA256
		if magicTotal > math.MaxInt64-record.MagicDelta ||
			experienceUnits > math.MaxInt64-units {
			return ErrInvariant
		}
		magicTotal += record.MagicDelta
		experienceUnits += units
	}
	experience, err := basisPointUnits(experienceUnits)
	if err != nil || magicTotal != artifact.MagicDelta || experience != artifact.ExperienceDelta {
		return ErrInvariant
	}
	return nil
}

func (repository *PostgresSettlementRepository) ensureCompensationApproval(
	ctx context.Context,
	artifact CompensationArtifact,
	artifactSHA256 [sha256.Size]byte,
	artifactSizeBytes int64,
	operatorReference string,
	approvedAt time.Time,
) (compensationApproval, error) {
	firstWindow, _ := time.Parse(time.RFC3339, artifact.Header.FirstWindow)
	lastWindow, _ := time.Parse(time.RFC3339, artifact.Header.LastWindow)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return compensationApproval{}, fmt.Errorf("begin compensation approval: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"seeding-compensation:"+hex.EncodeToString(artifactSHA256[:])); err != nil {
		return compensationApproval{}, fmt.Errorf("lock compensation approval: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.seeding_reward_compensation_approvals (
    artifact_sha256, artifact_size_bytes, schema_version,
    tracker_source_stream, tracker_fence_sequence,
    first_window, last_window, record_count, magic_delta,
    experience_delta, operator_reference, approved_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::numeric, $11, $12)
ON CONFLICT (artifact_sha256) DO NOTHING`, artifactSHA256[:], artifactSizeBytes,
		artifact.Header.SchemaVersion, artifact.Header.TrackerSourceStream,
		artifact.Header.TrackerFenceSequence, firstWindow, lastWindow,
		artifact.RecordCount, artifact.MagicDelta, artifact.ExperienceDelta,
		operatorReference, approvedAt); err != nil {
		return compensationApproval{}, classifySettlementDatabaseError("insert compensation approval", err)
	}
	var result compensationApproval
	var matches bool
	if err := tx.QueryRow(ctx, `
SELECT approved_at,
       artifact_size_bytes = $2
       AND schema_version = $3
       AND tracker_source_stream = $4
       AND tracker_fence_sequence = $5
       AND first_window = $6
       AND last_window = $7
       AND record_count = $8
       AND magic_delta = $9
       AND experience_delta = $10::numeric
       AND operator_reference = $11
FROM economy.seeding_reward_compensation_approvals
WHERE artifact_sha256 = $1`, artifactSHA256[:], artifactSizeBytes,
		artifact.Header.SchemaVersion, artifact.Header.TrackerSourceStream,
		artifact.Header.TrackerFenceSequence, firstWindow, lastWindow,
		artifact.RecordCount, artifact.MagicDelta, artifact.ExperienceDelta,
		operatorReference).Scan(&result.ApprovedAt, &matches); err != nil {
		return compensationApproval{}, fmt.Errorf("read compensation approval: %w", err)
	}
	if !matches {
		return compensationApproval{}, ErrEvidenceConflict
	}
	result.ApprovedAt = canonicalTime(result.ApprovedAt)
	if err := tx.Commit(ctx); err != nil {
		return compensationApproval{}, classifySettlementDatabaseError("commit compensation approval", err)
	}
	return result, nil
}

func (repository *PostgresSettlementRepository) applyCompensationBatch(
	ctx context.Context,
	artifactSHA256 [sha256.Size]byte,
	approval compensationApproval,
	records []CompensationArtifactRecord,
	appliedAt time.Time,
) (int64, int64, error) {
	appliedAt = canonicalTime(appliedAt)
	if appliedAt.Before(approval.ApprovedAt) {
		return 0, 0, fmt.Errorf("compensation batch clock precedes its immutable approval: %w", ErrInvariant)
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("begin compensation batch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var applied, replayed int64
	for _, record := range records {
		isReplay, err := repository.applyCompensationRecord(
			ctx, tx, artifactSHA256, record, appliedAt,
		)
		if err != nil {
			return 0, 0, err
		}
		if isReplay {
			replayed++
		} else {
			applied++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, classifySettlementDatabaseError("commit compensation batch", err)
	}
	return applied, replayed, nil
}

func (repository *PostgresSettlementRepository) applyCompensationRecord(
	ctx context.Context,
	tx pgx.Tx,
	artifactSHA256 [sha256.Size]byte,
	record CompensationArtifactRecord,
	appliedAt time.Time,
) (bool, error) {
	windowStart, _ := time.Parse(time.RFC3339, record.WindowStart)
	userID, _ := uuid.Parse(record.UserID)
	originalDigest, err := decodeOptionalCompensationDigest(record.OriginalCalculationSHA256)
	if err != nil {
		return false, fmt.Errorf("decode original compensation calculation digest: %w", err)
	}
	correctedDigest, _ := hex.DecodeString(record.CorrectedCalculationSHA256)
	correctedEvidence, _ := hex.DecodeString(record.CorrectedEvidenceSHA256)
	transactionID := uuid.NewSHA1(compensationTransactionNamespace, []byte(record.SourceReference))
	experienceEntryID := uuid.Nil
	if record.ExperienceDelta != "0" {
		experienceEntryID = uuid.NewSHA1(compensationExperienceNamespace, []byte(record.SourceReference))
	}

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, record.SourceReference); err != nil {
		return false, fmt.Errorf("lock compensation source: %w", err)
	}
	var replayMatches bool
	err = tx.QueryRow(ctx, `
SELECT artifact_sha256 = $2
   AND window_start = $3
   AND user_id = $4
   AND policy_revision = $5
   AND benefit_revision = $6
   AND original_calculation_sha256 IS NOT DISTINCT FROM $7::bytea
   AND corrected_calculation_sha256 = $8
   AND corrected_evidence_sha256 = $9
   AND original_reward = $10
   AND corrected_reward = $11
   AND magic_delta = $12
   AND experience_delta = $13::numeric
   AND eligible_torrent_count = $14
   AND capped = $15
   AND magic_transaction_id = $16
   AND experience_entry_id IS NOT DISTINCT FROM NULLIF($17::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
FROM economy.seeding_reward_compensation_receipts
WHERE source_reference = $1`, record.SourceReference, artifactSHA256[:], windowStart, userID,
		record.PolicyRevision, record.BenefitRevision, originalDigest, correctedDigest,
		correctedEvidence, record.OriginalReward, record.CorrectedReward, record.MagicDelta,
		record.ExperienceDelta, record.EligibleTorrentCount, record.Capped,
		transactionID, experienceEntryID).Scan(&replayMatches)
	if err == nil {
		if !replayMatches {
			return false, ErrEvidenceConflict
		}
		return true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("read compensation receipt: %w", err)
	}

	policy, found, err := readPolicy(ctx, tx, `
WHERE effective_from <= $1
ORDER BY effective_from DESC
LIMIT 1`, windowStart)
	if err != nil || !found {
		if err != nil {
			return false, err
		}
		return false, ErrPolicyNotFound
	}
	if policy.Policy.Revision != record.PolicyRevision {
		return false, ErrEvidenceConflict
	}
	var windowStatus string
	if err := tx.QueryRow(ctx, `
SELECT status
FROM economy.seeding_reward_evidence_windows
WHERE window_start = $1 AND window_end = $1 + interval '1 hour'`, windowStart).Scan(&windowStatus); err != nil || windowStatus != "complete" {
		if err != nil {
			return false, fmt.Errorf("read compensation Core window: %w", err)
		}
		return false, ErrEvidenceConflict
	}
	if err := verifyCompensationOriginal(ctx, tx, windowStart, userID, record, originalDigest); err != nil {
		return false, fmt.Errorf("verify original compensation calculation: %w", err)
	}
	historicalLevelPolicy, err := verifyCompensationBenefit(ctx, tx, windowStart, userID, record.BenefitRevision)
	if err != nil {
		return false, fmt.Errorf("verify original compensation benefit: %w", err)
	}
	expectedExperience, err := basisPointAmount(record.MagicDelta, policy.Policy.ExperiencePerMagicBPS)
	if err != nil {
		return false, fmt.Errorf("calculate compensation experience: %w", err)
	}
	if expectedExperience != record.ExperienceDelta {
		return false, fmt.Errorf("compensation experience differs from the historical reward policy: %w", ErrEvidenceConflict)
	}
	payloadSHA256, err := compensationPostingSHA256(artifactSHA256, record)
	if err != nil {
		return false, fmt.Errorf("hash compensation posting: %w", err)
	}
	idempotencyKey := record.SourceReference
	_, err = repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
		TransactionID: transactionID, TransactionType: economy.TransactionSeedingReward,
		IdempotencyKey: idempotencyKey, SourceReference: record.SourceReference,
		PolicyRevision: record.PolicyRevision, PayloadSHA256: payloadSHA256,
		OccurredAt: windowStart.Add(WindowDuration), RecordedAt: appliedAt,
		Postings: []economy.PostingInput{
			{AccountID: userID, Amount: record.MagicDelta},
			{AccountID: economy.SeedingMintAccountID(), Amount: -record.MagicDelta},
		},
	})
	if err != nil {
		return false, fmt.Errorf("record compensation magic: %w", err)
	}
	appliedLevelPolicy := historicalLevelPolicy
	if experienceEntryID != uuid.Nil {
		err = tx.QueryRow(ctx, `
SELECT policy_version
FROM progression.user_progress
WHERE user_id = $1
FOR UPDATE`, userID).Scan(&appliedLevelPolicy)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("lock current compensation level policy: %w", err)
		}
		experienceAmount, err := progression.ParseAmount(record.ExperienceDelta)
		if err != nil {
			return false, fmt.Errorf("parse compensation experience: %w", ErrInvariant)
		}
		_, err = repository.progression.RecordInTransaction(ctx, tx, progression.RecordCommand{
			EntryID: experienceEntryID, IdempotencyKey: idempotencyKey,
			UserID: userID, EntryType: progression.EntryEarn, Amount: experienceAmount,
			SourceReference: record.SourceReference, SourceKind: progression.SourceSeedingReward,
			PolicyRevision: record.PolicyRevision, LevelPolicyVersion: appliedLevelPolicy,
			PayloadSHA256: payloadSHA256, MagicTransactionID: transactionID,
			OccurredAt: windowStart.Add(WindowDuration), RecordedAt: appliedAt,
		})
		if err != nil {
			return false, fmt.Errorf("record compensation experience: %w", err)
		}
	}
	_, err = tx.Exec(ctx, `
INSERT INTO economy.seeding_reward_compensation_receipts (
    artifact_sha256, source_reference, window_start, user_id,
    policy_revision, benefit_revision, level_policy_version,
    original_calculation_sha256,
    corrected_calculation_sha256, corrected_evidence_sha256,
    original_reward, corrected_reward, magic_delta, experience_delta,
    eligible_torrent_count, capped, magic_transaction_id,
    experience_entry_id, applied_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14::numeric, $15, $16, $17,
    NULLIF($18::uuid, '00000000-0000-0000-0000-000000000000'::uuid), $19
)`, artifactSHA256[:], record.SourceReference, windowStart, userID,
		record.PolicyRevision, record.BenefitRevision, appliedLevelPolicy, originalDigest,
		correctedDigest, correctedEvidence, record.OriginalReward, record.CorrectedReward,
		record.MagicDelta, record.ExperienceDelta, record.EligibleTorrentCount,
		record.Capped, transactionID, experienceEntryID, appliedAt)
	if err != nil {
		return false, classifySettlementDatabaseError("insert compensation receipt", err)
	}
	return false, nil
}

func verifyCompensationBenefit(
	ctx context.Context,
	tx pgx.Tx,
	windowStart time.Time,
	userID uuid.UUID,
	revision string,
) (string, error) {
	entitlementRevision, expectedLevel, levelPolicy, err := parseCompensationBenefitRevision(revision)
	if err != nil {
		return "", err
	}
	var snapshotRevision string
	err = tx.QueryRow(ctx, `
SELECT benefit_revision
FROM economy.seeding_reward_benefit_snapshots
WHERE window_start = $1 AND user_id = $2`, windowStart, userID).Scan(&snapshotRevision)
	if err == nil && snapshotRevision != revision {
		return "", ErrEvidenceConflict
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("read compensation benefit snapshot: %w", err)
	}
	var storedEntitlement int64
	if err := tx.QueryRow(ctx, `
SELECT revision
FROM identity.user_reward_benefit_revisions
WHERE user_id = $1 AND effective_from <= $2
ORDER BY effective_from DESC, revision DESC
LIMIT 1`, userID, windowStart).Scan(&storedEntitlement); err != nil {
		return "", fmt.Errorf("read original compensation entitlement: %w", err)
	}
	if storedEntitlement != entitlementRevision {
		return "", ErrEvidenceConflict
	}
	var storedPolicy string
	var storedLevel int16
	err = tx.QueryRow(ctx, `
SELECT level_policy_version, level_after
FROM progression.experience_entries
WHERE user_id = $1
  AND occurred_at <= $2
  AND source_reference !~ '^seeding_compensation:v1:'
ORDER BY occurred_at DESC, entry_sequence DESC
LIMIT 1`, userID, windowStart).Scan(&storedPolicy, &storedLevel)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
SELECT policy_version, level
FROM progression.level_definitions
WHERE policy_version = 'rousi-v1'
ORDER BY minimum_experience, level
LIMIT 1`).Scan(&storedPolicy, &storedLevel)
	}
	if err != nil {
		return "", fmt.Errorf("read original compensation level: %w", err)
	}
	if storedPolicy != levelPolicy || storedLevel != expectedLevel {
		return "", ErrEvidenceConflict
	}
	return levelPolicy, nil
}

func parseCompensationBenefitRevision(revision string) (int64, int16, string, error) {
	parts := strings.Split(revision, ".")
	if len(parts) < 4 || parts[0] != "benefit-v1" || len(parts[1]) < 2 || parts[1][0] != 'e' ||
		len(parts[2]) < 2 || parts[2][0] != 'l' {
		return 0, 0, "", ErrInvariant
	}
	entitlement, err := strconv.ParseInt(parts[1][1:], 10, 64)
	if err != nil || entitlement < 1 {
		return 0, 0, "", ErrInvariant
	}
	levelValue, err := strconv.ParseInt(parts[2][1:], 10, 16)
	levelPolicy := strings.Join(parts[3:], ".")
	if err != nil || levelValue < 1 || !compensationRevisionPattern.MatchString(levelPolicy) {
		return 0, 0, "", ErrInvariant
	}
	return entitlement, int16(levelValue), levelPolicy, nil
}

func verifyCompensationOriginal(
	ctx context.Context,
	tx pgx.Tx,
	windowStart time.Time,
	userID uuid.UUID,
	record CompensationArtifactRecord,
	expectedDigest []byte,
) error {
	var policyRevision pgtype.Text
	var digest []byte
	var reward pgtype.Int8
	err := tx.QueryRow(ctx, `
SELECT calculation.policy_revision, calculation.calculation_sha256, calculation.reward
FROM economy.seeding_reward_calculations AS calculation
WHERE calculation.window_start = $1 AND calculation.user_id = $2`, windowStart, userID).
		Scan(&policyRevision, &digest, &reward)
	if errors.Is(err, pgx.ErrNoRows) {
		if record.OriginalReward != 0 || len(expectedDigest) != 0 || record.OriginalCalculationSHA256 != "" {
			return ErrEvidenceConflict
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read original compensation calculation: %w", err)
	}
	if !policyRevision.Valid || policyRevision.String != record.PolicyRevision || !reward.Valid ||
		reward.Int64 != record.OriginalReward || !bytes.Equal(digest, expectedDigest) {
		return ErrEvidenceConflict
	}
	return nil
}

func decodeOptionalCompensationDigest(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return nil, ErrInvariant
	}
	return decoded, nil
}

func compensationPostingSHA256(
	artifactSHA256 [sha256.Size]byte,
	record CompensationArtifactRecord,
) ([sha256.Size]byte, error) {
	document := compensationPostingDocument{
		SchemaVersion:   CompensationPreviewSchemaVersion,
		ArtifactSHA256:  hex.EncodeToString(artifactSHA256[:]),
		SourceReference: record.SourceReference, WindowStart: record.WindowStart,
		UserID: record.UserID, PolicyRevision: record.PolicyRevision,
		BenefitRevision:            record.BenefitRevision,
		OriginalCalculationSHA256:  record.OriginalCalculationSHA256,
		CorrectedCalculationSHA256: record.CorrectedCalculationSHA256,
		CorrectedEvidenceSHA256:    record.CorrectedEvidenceSHA256,
		OriginalReward:             record.OriginalReward, CorrectedReward: record.CorrectedReward,
		MagicDelta: record.MagicDelta, ExperienceDelta: record.ExperienceDelta,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return [sha256.Size]byte{}, ErrInvariant
	}
	return sha256.Sum256(encoded), nil
}

func (repository *PostgresSettlementRepository) compensationCompletionMatches(
	ctx context.Context,
	artifact CompensationArtifact,
	artifactSHA256 [sha256.Size]byte,
) (bool, error) {
	var matches bool
	err := repository.pool.QueryRow(ctx, `
SELECT receipt_count = $2
   AND magic_delta = $3
   AND experience_delta = $4::numeric
FROM economy.seeding_reward_compensation_completions
WHERE artifact_sha256 = $1`, artifactSHA256[:], artifact.RecordCount,
		artifact.MagicDelta, artifact.ExperienceDelta).Scan(&matches)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read compensation completion: %w", err)
	}
	if !matches {
		return false, ErrEvidenceConflict
	}
	return true, nil
}

func (repository *PostgresSettlementRepository) completeCompensation(
	ctx context.Context,
	artifact CompensationArtifact,
	artifactSHA256 [sha256.Size]byte,
) error {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin compensation completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var receiptCount, magicDelta int64
	var experienceMatches bool
	if err := tx.QueryRow(ctx, `
SELECT count(*)::bigint,
       coalesce(sum(magic_delta), 0)::bigint,
       coalesce(sum(experience_delta), 0) = $2::numeric
FROM economy.seeding_reward_compensation_receipts
WHERE artifact_sha256 = $1`, artifactSHA256[:], artifact.ExperienceDelta).
		Scan(&receiptCount, &magicDelta, &experienceMatches); err != nil {
		return fmt.Errorf("reconcile compensation receipts: %w", err)
	}
	if receiptCount != artifact.RecordCount || magicDelta != artifact.MagicDelta || !experienceMatches {
		return ErrEvidenceConflict
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO economy.seeding_reward_compensation_completions (
    artifact_sha256, receipt_count, magic_delta, experience_delta, completed_at
) VALUES ($1, $2, $3, $4::numeric, clock_timestamp())
ON CONFLICT (artifact_sha256) DO NOTHING`, artifactSHA256[:], artifact.RecordCount,
		artifact.MagicDelta, artifact.ExperienceDelta); err != nil {
		return classifySettlementDatabaseError("insert compensation completion", err)
	}
	var matches bool
	if err := tx.QueryRow(ctx, `
SELECT receipt_count = $2 AND magic_delta = $3 AND experience_delta = $4::numeric
FROM economy.seeding_reward_compensation_completions
WHERE artifact_sha256 = $1`, artifactSHA256[:], artifact.RecordCount,
		artifact.MagicDelta, artifact.ExperienceDelta).Scan(&matches); err != nil {
		return fmt.Errorf("verify compensation completion: %w", err)
	}
	if !matches {
		return ErrEvidenceConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return classifySettlementDatabaseError("commit compensation completion", err)
	}
	return nil
}
