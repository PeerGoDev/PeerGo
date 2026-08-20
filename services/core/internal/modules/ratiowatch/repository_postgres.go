package ratiowatch

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("ratio watch database is required")
	}
	return &PostgresRepository{pool: pool}, nil
}

// MyStatus reads one user-safe snapshot at a caller-supplied service time. An
// active assessment always selects its bound immutable revision; only members
// without an active assessment see the current effective site revision.
func (repository *PostgresRepository) MyStatus(ctx context.Context, userID uuid.UUID, now time.Time) (MyStatus, error) {
	if userID == uuid.Nil || now.IsZero() {
		return MyStatus{}, ErrInput
	}
	result := MyStatus{ObservedAt: now.UTC().Round(0)}
	var accountRestricted bool
	var assessmentStatus pgtype.Text
	var assessmentStarted, assessmentDeadline, assessmentRestrictionStarted, assessmentUpdated pgtype.Timestamptz
	var ruleVersion, threshold, minimumRatio, watchSeconds, restrictionRatio pgtype.Int8
	var policyEnabled, vipExempt, boundToAssessment pgtype.Bool
	var policyEffective pgtype.Timestamptz
	var appealStatus, appealStatement, appealResponse pgtype.Text
	var appealSubmitted, appealResolved pgtype.Timestamptz
	err := repository.pool.QueryRow(ctx, `
SELECT
    COALESCE(totals.credited_uploaded, 0)::bigint,
    COALESCE(totals.charged_downloaded, 0)::bigint,
    COALESCE(access.vip_enabled AND (access.vip_until IS NULL OR access.vip_until > $2), false),
    COALESCE(access.download_restricted, false),
    active.status,
    active.started_at,
    active.deadline_at,
    active.restriction_started_at,
    active.updated_at,
    selected.rule_version,
    selected.enabled,
    selected.download_threshold_bytes,
    selected.minimum_ratio_basis_points,
    selected.watch_period_seconds,
    selected.restriction_ratio_basis_points,
    selected.vip_exempt,
    selected.effective_at,
    (active.status IS NOT NULL),
    CASE WHEN appeal.id IS NULL THEN NULL
         ELSE COALESCE(resolution.outcome, 'pending') END,
    appeal.statement,
    appeal.created_at,
    resolution.created_at,
    resolution.response
FROM identity.users AS users
LEFT JOIN traffic.user_totals AS totals ON totals.user_id = users.id
LEFT JOIN identity.user_access_states AS access ON access.user_id = users.id
LEFT JOIN LATERAL (
    SELECT assessment.id, assessment.status, assessment.started_at, assessment.deadline_at,
           assessment.restriction_started_at, assessment.updated_at,
           assessment.policy_revision_id
    FROM ratio_watch.assessments AS assessment
    WHERE assessment.user_id = users.id
      AND assessment.status IN ('watching', 'warning', 'download_restricted')
    ORDER BY assessment.started_at DESC, assessment.id DESC
    LIMIT 1
) AS active ON true
LEFT JOIN LATERAL (
    SELECT revision.rule_version, revision.enabled,
           revision.download_threshold_bytes, revision.minimum_ratio_basis_points,
           revision.watch_period_seconds, revision.restriction_ratio_basis_points,
           revision.vip_exempt, revision.effective_at
    FROM ratio_watch.policy_revisions AS revision
    WHERE revision.id = active.policy_revision_id
       OR (active.policy_revision_id IS NULL AND revision.effective_at <= $2)
    ORDER BY (revision.id = active.policy_revision_id) DESC,
             revision.effective_at DESC, revision.rule_version DESC
    LIMIT 1
) AS selected ON true
LEFT JOIN LATERAL (
    SELECT request.id, request.assessment_id, request.statement, request.created_at
    FROM ratio_watch.appeals AS request
    WHERE request.user_id = users.id
      AND (active.id IS NULL OR request.assessment_id = active.id)
    ORDER BY request.created_at DESC, request.id DESC
    LIMIT 1
) AS appeal ON true
LEFT JOIN ratio_watch.appeal_resolutions AS resolution
  ON resolution.appeal_id = appeal.id
WHERE users.id = $1`, userID, result.ObservedAt).Scan(
		&result.CreditedUploaded, &result.ChargedDownloaded, &result.VIPActive,
		&accountRestricted, &assessmentStatus, &assessmentStarted, &assessmentDeadline,
		&assessmentRestrictionStarted, &assessmentUpdated, &ruleVersion, &policyEnabled,
		&threshold, &minimumRatio, &watchSeconds, &restrictionRatio, &vipExempt,
		&policyEffective, &boundToAssessment, &appealStatus, &appealStatement,
		&appealSubmitted, &appealResolved, &appealResponse,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MyStatus{}, ErrNotFound
		}
		return MyStatus{}, fmt.Errorf("read my ratio watch status: %w", err)
	}
	if assessmentStatus.Valid {
		status := AssessmentStatus(assessmentStatus.String)
		if !status.Active() || !assessmentStarted.Valid || !assessmentDeadline.Valid || !assessmentUpdated.Valid {
			return MyStatus{}, ErrInvariant
		}
		assessment := MyAssessment{
			Status:     status,
			StartedAt:  assessmentStarted.Time.UTC().Round(0),
			DeadlineAt: assessmentDeadline.Time.UTC().Round(0),
			UpdatedAt:  assessmentUpdated.Time.UTC().Round(0),
		}
		if assessmentRestrictionStarted.Valid {
			value := assessmentRestrictionStarted.Time.UTC().Round(0)
			assessment.RestrictionStartedAt = &value
		}
		result.Assessment = &assessment
	}
	if ruleVersion.Valid {
		if ruleVersion.Int64 < 1 || !policyEnabled.Valid || !threshold.Valid || !minimumRatio.Valid ||
			!watchSeconds.Valid || !restrictionRatio.Valid || !vipExempt.Valid || !policyEffective.Valid || !boundToAssessment.Valid {
			return MyStatus{}, ErrInvariant
		}
		result.Policy = &MyPolicy{
			RuleVersion: ruleVersion.Int64,
			PolicyInput: PolicyInput{
				Enabled:                     policyEnabled.Bool,
				DownloadThresholdBytes:      threshold.Int64,
				MinimumRatioBasisPoints:     minimumRatio.Int64,
				WatchPeriodSeconds:          watchSeconds.Int64,
				RestrictionRatioBasisPoints: restrictionRatio.Int64,
			},
			VIPExempt:         vipExempt.Bool,
			EffectiveAt:       policyEffective.Time.UTC().Round(0),
			BoundToAssessment: boundToAssessment.Bool,
		}
	}
	if appealStatus.Valid {
		if !appealStatement.Valid || !appealSubmitted.Valid {
			return MyStatus{}, ErrInvariant
		}
		appeal := MyAppeal{
			Status: AppealStatus(appealStatus.String), Statement: appealStatement.String,
			SubmittedAt: appealSubmitted.Time.UTC().Round(0),
		}
		if appealResolved.Valid {
			value := appealResolved.Time.UTC().Round(0)
			appeal.ResolvedAt = &value
		}
		if appealResponse.Valid {
			appeal.Response = appealResponse.String
		}
		if !validAppealProjection(appeal) {
			return MyStatus{}, ErrInvariant
		}
		result.Appeal = &appeal
	}
	result.CanAppeal = result.Assessment != nil && result.Appeal == nil
	result.CurrentRatioBasisPoints = ratioBasisPoints(result.CreditedUploaded, result.ChargedDownloaded)
	ratioRestricted := result.Assessment != nil && result.Assessment.Status == AssessmentDownloadRestricted
	result.DownloadRestricted = accountRestricted || ratioRestricted
	switch {
	case accountRestricted && ratioRestricted:
		result.RestrictionSource = RestrictionBoth
	case accountRestricted:
		result.RestrictionSource = RestrictionAccount
	case ratioRestricted:
		result.RestrictionSource = RestrictionRatioWatch
	default:
		result.RestrictionSource = RestrictionNone
	}
	if result.Policy != nil && result.Policy.Enabled {
		result.ThresholdReached = result.ChargedDownloaded >= result.Policy.DownloadThresholdBytes
		result.MinimumRatioReached = ratioAtLeast(
			result.CreditedUploaded, result.ChargedDownloaded, result.Policy.MinimumRatioBasisPoints,
		)
		result.RecoveryUploadedBytes = recoveryUploadBytes(
			result.CreditedUploaded, result.ChargedDownloaded, result.Policy.MinimumRatioBasisPoints,
		)
	}
	return result, nil
}

func ratioAtLeast(uploaded, downloaded, targetBasisPoints int64) bool {
	if uploaded < 0 || downloaded < 0 || targetBasisPoints < 0 {
		return false
	}
	left := new(big.Int).Mul(big.NewInt(uploaded), big.NewInt(RatioScale))
	right := new(big.Int).Mul(big.NewInt(downloaded), big.NewInt(targetBasisPoints))
	return left.Cmp(right) >= 0
}

func recoveryUploadBytes(uploaded, downloaded, targetBasisPoints int64) int64 {
	if uploaded < 0 || downloaded <= 0 || targetBasisPoints <= 0 {
		return 0
	}
	numerator := new(big.Int).Mul(big.NewInt(downloaded), big.NewInt(targetBasisPoints))
	// Ceiling division prevents displaying zero while one final credited byte
	// is still required to cross the immutable integer-ratio boundary.
	target := new(big.Int).Add(numerator, big.NewInt(RatioScale-1))
	target.Div(target, big.NewInt(RatioScale))
	target.Sub(target, big.NewInt(uploaded))
	if target.Sign() <= 0 {
		return 0
	}
	if target.Cmp(big.NewInt(math.MaxInt64)) > 0 {
		return math.MaxInt64
	}
	return target.Int64()
}

func (repository *PostgresRepository) Policies(ctx context.Context, limit, offset int, now time.Time) (PolicyPage, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PolicyPage{}, fmt.Errorf("begin ratio policy read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	page := PolicyPage{Limit: limit, Offset: offset, MinimumEffectiveFrom: now.Add(minimumLeadTime)}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ratio_watch.policy_revisions`).Scan(&page.Total); err != nil {
		return PolicyPage{}, fmt.Errorf("count ratio policy revisions: %w", err)
	}
	rows, err := tx.Query(ctx, policyRevisionSelect+`
ORDER BY revision.rule_version DESC
LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return PolicyPage{}, fmt.Errorf("list ratio policy revisions: %w", err)
	}
	for rows.Next() {
		item, scanErr := scanPolicyRevision(rows, now)
		if scanErr != nil {
			rows.Close()
			return PolicyPage{}, scanErr
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return PolicyPage{}, fmt.Errorf("iterate ratio policy revisions: %w", err)
	}
	current, err := scanPolicyRevision(tx.QueryRow(ctx, policyRevisionSelect+`
WHERE revision.effective_at <= $1
ORDER BY revision.effective_at DESC, revision.rule_version DESC
LIMIT 1`, now), now)
	if err == nil {
		page.Current = &current
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return PolicyPage{}, err
	}
	if err := tx.QueryRow(ctx, `
SELECT
    count(*) FILTER (WHERE status = 'watching')::bigint,
    count(*) FILTER (WHERE status = 'warning')::bigint,
    count(*) FILTER (WHERE status = 'download_restricted')::bigint,
    count(*) FILTER (WHERE status = 'satisfied')::bigint,
    count(*) FILTER (WHERE status = 'manually_cleared')::bigint,
    count(*) FILTER (WHERE status = 'vip_exempted')::bigint
FROM ratio_watch.assessments`).Scan(
		&page.Summary.Watching, &page.Summary.Warning, &page.Summary.DownloadRestricted,
		&page.Summary.Satisfied, &page.Summary.ManuallyCleared, &page.Summary.VIPExempted,
	); err != nil {
		return PolicyPage{}, fmt.Errorf("read ratio assessment summary: %w", err)
	}
	if err := scanWorkerState(tx.QueryRow(ctx, `
SELECT last_started_at, last_completed_at, COALESCE(last_error_code, ''),
       last_examined, last_created, last_transitioned, run_count
FROM ratio_watch.worker_state WHERE singleton = true`), &page.Worker); err != nil {
		return PolicyPage{}, fmt.Errorf("read ratio worker state: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PolicyPage{}, fmt.Errorf("commit ratio policy read: %w", err)
	}
	return page, nil
}

func (repository *PostgresRepository) Preview(ctx context.Context, policy PolicyInput, now time.Time) (ImpactPreview, error) {
	result := ImpactPreview{Policy: policy}
	if err := repository.pool.QueryRow(ctx, `
WITH candidates AS (
    SELECT
        users.id,
        COALESCE(totals.credited_uploaded, 0)::bigint AS uploaded,
        COALESCE(totals.charged_downloaded, 0)::bigint AS downloaded,
        COALESCE(access.vip_enabled AND (access.vip_until IS NULL OR access.vip_until > $1), false) AS vip_active,
        COALESCE(access.download_restricted, false) AS legacy_restricted,
        EXISTS (
            SELECT 1 FROM ratio_watch.assessments AS active
            WHERE active.user_id = users.id
              AND active.status IN ('watching', 'warning', 'download_restricted')
        ) AS already_active
    FROM identity.users AS users
    LEFT JOIN traffic.user_totals AS totals ON totals.user_id = users.id
    LEFT JOIN identity.user_access_states AS access ON access.user_id = users.id
    WHERE users.status = 'active'
)
SELECT
    count(*) FILTER (
        WHERE $2::boolean AND NOT vip_active AND NOT legacy_restricted AND NOT already_active
          AND downloaded >= $3::bigint
    )::bigint,
    count(*) FILTER (
        WHERE $2::boolean AND NOT vip_active AND NOT legacy_restricted AND NOT already_active
          AND downloaded >= $3::bigint
          AND uploaded::numeric * 10000 < downloaded::numeric * $4::bigint
    )::bigint,
    count(*) FILTER (
        WHERE $2::boolean AND NOT vip_active AND NOT legacy_restricted AND NOT already_active
          AND downloaded >= $3::bigint
          AND uploaded::numeric * 10000 < downloaded::numeric * $5::bigint
    )::bigint,
    count(*) FILTER (WHERE vip_active)::bigint,
    count(*) FILTER (WHERE legacy_restricted)::bigint
FROM candidates`, now, policy.Enabled, policy.DownloadThresholdBytes, policy.MinimumRatioBasisPoints,
		policy.RestrictionRatioBasisPoints).Scan(
		&result.EligibleUsers, &result.WouldEnterWatch, &result.WouldRestrictAtDeadline,
		&result.VIPExemptUsers, &result.LegacyRestrictedUsers,
	); err != nil {
		return ImpactPreview{}, fmt.Errorf("preview ratio policy impact: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) Issue(ctx context.Context, command IssueCommand) (PolicyRevision, error) {
	if command.RevisionID == uuid.Nil || command.ActorID == uuid.Nil || command.Authorization.ID == uuid.Nil || !command.Authorization.Allow {
		return PolicyRevision{}, ErrInput
	}
	encoded, err := json.Marshal(struct {
		SchemaVersion string      `json:"schema_version"`
		RevisionID    uuid.UUID   `json:"revision_id"`
		EffectiveAt   time.Time   `json:"effective_at"`
		Policy        PolicyInput `json:"policy"`
	}{SchemaVersion: "1.0.0", RevisionID: command.RevisionID, EffectiveAt: command.EffectiveAt, Policy: command.Policy})
	if err != nil {
		return PolicyRevision{}, ErrInput
	}
	digest := sha256.Sum256(encoded)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return PolicyRevision{}, fmt.Errorf("begin ratio policy issue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	existing, err := scanPolicyRevision(tx.QueryRow(ctx, policyRevisionSelect+` WHERE revision.id = $1`, command.RevisionID), command.OccurredAt)
	if err == nil {
		if sameIssue(existing, command) {
			existing.Replayed = true
			if err := tx.Commit(ctx); err != nil {
				return PolicyRevision{}, fmt.Errorf("commit ratio policy replay: %w", err)
			}
			return existing, nil
		}
		return PolicyRevision{}, ErrIdempotencyConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PolicyRevision{}, err
	}
	var latest PolicyRevision
	latest, err = scanPolicyRevision(tx.QueryRow(ctx, policyRevisionSelect+`
ORDER BY revision.rule_version DESC LIMIT 1`), command.OccurredAt)
	version := int64(1)
	if err == nil {
		if !command.EffectiveAt.After(latest.EffectiveAt) {
			return PolicyRevision{}, ErrConflict
		}
		if samePolicy(latest.PolicyInput, command.Policy) {
			return PolicyRevision{}, ErrNoChange
		}
		version = latest.RuleVersion + 1
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return PolicyRevision{}, err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO ratio_watch.policy_revisions (
    id, rule_id, rule_version, enabled, download_threshold_bytes,
    minimum_ratio_basis_points, watch_period_seconds,
    restriction_ratio_basis_points, vip_exempt, effective_at, reason,
    actor_id, authorization_decision_id, command_sha256, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true, $9, $10, $11, $12, $13, $14)`,
		command.RevisionID, DefaultRuleID, version, command.Policy.Enabled,
		command.Policy.DownloadThresholdBytes, command.Policy.MinimumRatioBasisPoints,
		command.Policy.WatchPeriodSeconds, command.Policy.RestrictionRatioBasisPoints,
		command.EffectiveAt, command.Reason, command.ActorID, command.Authorization.ID,
		digest[:], command.OccurredAt)
	if err != nil {
		return PolicyRevision{}, classifyDatabaseError("insert ratio policy revision", err)
	}
	result := PolicyRevision{
		ID: command.RevisionID, RuleID: DefaultRuleID, RuleVersion: version,
		PolicyInput: command.Policy, VIPExempt: true, EffectiveAt: command.EffectiveAt,
		Reason: command.Reason, ActorID: command.ActorID,
		AuthorizationDecisionID: command.Authorization.ID, CommandSHA256: digest,
		CreatedAt: command.OccurredAt, TimelineState: TimelineScheduled,
	}
	if err := tx.Commit(ctx); err != nil {
		return PolicyRevision{}, classifyDatabaseError("commit ratio policy revision", err)
	}
	return result, nil
}

func (repository *PostgresRepository) Assessments(ctx context.Context, query AssessmentQuery) (AssessmentPage, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return AssessmentPage{}, fmt.Errorf("begin ratio assessment read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	filterSQL := assessmentFilterSQL(query.Filter)
	search := "%" + strings.ToLower(query.Query) + "%"
	var total int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)
FROM ratio_watch.assessments AS assessment
JOIN identity.users AS users ON users.id = assessment.user_id
WHERE ($1 = '%%' OR lower(users.username) LIKE $1)
  AND `+filterSQL, search).Scan(&total); err != nil {
		return AssessmentPage{}, fmt.Errorf("count ratio assessments: %w", err)
	}
	rows, err := tx.Query(ctx, assessmentSelect+`
WHERE ($1 = '%%' OR lower(users.username) LIKE $1)
  AND `+filterSQL+`
ORDER BY
    CASE WHEN assessment.status IN ('download_restricted', 'warning', 'watching') THEN 0 ELSE 1 END,
    assessment.deadline_at ASC,
    assessment.started_at DESC,
    assessment.id DESC
LIMIT $2 OFFSET $3`, search, query.Limit, query.Offset)
	if err != nil {
		return AssessmentPage{}, fmt.Errorf("list ratio assessments: %w", err)
	}
	items := make([]Assessment, 0, query.Limit)
	for rows.Next() {
		item, scanErr := scanAssessment(rows)
		if scanErr != nil {
			rows.Close()
			return AssessmentPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AssessmentPage{}, fmt.Errorf("iterate ratio assessments: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AssessmentPage{}, fmt.Errorf("commit ratio assessment read: %w", err)
	}
	return AssessmentPage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func (repository *PostgresRepository) SubmitAppeal(ctx context.Context, command SubmitAppealCommand) (Appeal, error) {
	if command.AppealID == uuid.Nil || command.UserID == uuid.Nil || command.OccurredAt.IsZero() ||
		command.Authorization.ID == uuid.Nil || !command.Authorization.Allow {
		return Appeal{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Appeal{}, fmt.Errorf("begin ratio appeal submission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Check the idempotency identifier before current state. A network retry
	// must still recover the original response if the assessment was resolved
	// immediately after the first successful submission.
	existing, err := scanAppeal(tx.QueryRow(ctx, appealSelect+`
WHERE appeal.id = $1`, command.AppealID))
	if err == nil {
		if existing.UserID != command.UserID || existing.Statement != command.Statement {
			return Appeal{}, ErrIdempotencyConflict
		}
		existing.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return Appeal{}, classifyDatabaseError("commit ratio appeal replay", err)
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Appeal{}, err
	}

	var assessmentID uuid.UUID
	if err := tx.QueryRow(ctx, `
SELECT assessment.id
FROM ratio_watch.assessments AS assessment
WHERE assessment.user_id = $1
  AND assessment.status IN ('watching', 'warning', 'download_restricted')
ORDER BY assessment.started_at DESC, assessment.id DESC
LIMIT 1
FOR UPDATE`, command.UserID).Scan(&assessmentID); errors.Is(err, pgx.ErrNoRows) {
		return Appeal{}, ErrNoActiveAssessment
	} else if err != nil {
		return Appeal{}, fmt.Errorf("lock active ratio assessment for appeal: %w", err)
	}
	var alreadyExists bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM ratio_watch.appeals WHERE assessment_id = $1
)`, assessmentID).Scan(&alreadyExists); err != nil {
		return Appeal{}, fmt.Errorf("check existing ratio appeal: %w", err)
	}
	if alreadyExists {
		return Appeal{}, ErrAppealExists
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO ratio_watch.appeals (
    id, assessment_id, user_id, statement,
    authorization_decision_id, created_at
) VALUES ($1, $2, $3, $4, $5, $6)`, command.AppealID, assessmentID,
		command.UserID, command.Statement, command.Authorization.ID, command.OccurredAt); err != nil {
		return Appeal{}, classifyDatabaseError("insert ratio appeal", err)
	}
	result, err := scanAppeal(tx.QueryRow(ctx, appealSelect+`
WHERE appeal.id = $1`, command.AppealID))
	if err != nil {
		return Appeal{}, fmt.Errorf("read submitted ratio appeal: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Appeal{}, classifyDatabaseError("commit ratio appeal submission", err)
	}
	return result, nil
}

func (repository *PostgresRepository) Appeals(ctx context.Context, query AppealQuery) (AppealPage, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return AppealPage{}, fmt.Errorf("begin ratio appeal read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	filterSQL := appealFilterSQL(query.Filter)
	search := "%" + strings.ToLower(query.Query) + "%"
	var total int64
	if err := tx.QueryRow(ctx, `
SELECT count(*)
FROM ratio_watch.appeals AS appeal
JOIN identity.users AS users ON users.id = appeal.user_id
LEFT JOIN ratio_watch.appeal_resolutions AS resolution
  ON resolution.appeal_id = appeal.id
WHERE ($1 = '%%' OR lower(users.username) LIKE $1)
  AND `+filterSQL, search).Scan(&total); err != nil {
		return AppealPage{}, fmt.Errorf("count ratio appeals: %w", err)
	}
	rows, err := tx.Query(ctx, appealSelect+`
WHERE ($1 = '%%' OR lower(users.username) LIKE $1)
  AND `+filterSQL+`
ORDER BY (resolution.id IS NULL) DESC, appeal.created_at ASC, appeal.id ASC
LIMIT $2 OFFSET $3`, search, query.Limit, query.Offset)
	if err != nil {
		return AppealPage{}, fmt.Errorf("list ratio appeals: %w", err)
	}
	items := make([]Appeal, 0, query.Limit)
	for rows.Next() {
		item, scanErr := scanAppeal(rows)
		if scanErr != nil {
			rows.Close()
			return AppealPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AppealPage{}, fmt.Errorf("iterate ratio appeals: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AppealPage{}, fmt.Errorf("commit ratio appeal read: %w", err)
	}
	return AppealPage{Items: items, Total: total, Limit: query.Limit, Offset: query.Offset}, nil
}

func (repository *PostgresRepository) DecideAppeal(ctx context.Context, command DecideAppealCommand) (Appeal, error) {
	if command.AppealID == uuid.Nil || command.ActorID == uuid.Nil || command.OccurredAt.IsZero() ||
		command.Authorization.ID == uuid.Nil || !command.Authorization.Allow {
		return Appeal{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Appeal{}, fmt.Errorf("begin ratio appeal decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := scanAppeal(tx.QueryRow(ctx, appealSelect+`
WHERE appeal.id = $1
FOR UPDATE OF appeal, assessment`, command.AppealID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Appeal{}, ErrNotFound
	}
	if err != nil {
		return Appeal{}, err
	}
	if result.UserID == command.ActorID {
		return Appeal{}, ErrSelfTarget
	}
	if result.Status != AppealPending {
		return Appeal{}, ErrAppealResolved
	}
	if !result.AssessmentStatus.Active() {
		return Appeal{}, ErrNotActive
	}
	if result.AssessmentVersion != command.ExpectedAssessmentVersion {
		return Appeal{}, ErrConflict
	}
	resolutionID := uuid.New()
	if _, err := tx.Exec(ctx, `
INSERT INTO ratio_watch.appeal_resolutions (
    id, appeal_id, outcome, response, actor_id,
    authorization_decision_id, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)`, resolutionID, result.ID,
		command.Decision, command.Response, command.ActorID,
		command.Authorization.ID, command.OccurredAt); err != nil {
		return Appeal{}, classifyDatabaseError("insert ratio appeal resolution", err)
	}
	if command.Decision == AppealDecisionApprove {
		cleared, err := clearAssessmentTx(ctx, tx, ClearCommand{
			ClearInput: ClearInput{
				AssessmentID: result.AssessmentID, ExpectedVersion: result.AssessmentVersion,
				Reason: command.Response,
			},
			ActorID: command.ActorID, OccurredAt: command.OccurredAt,
			Authorization: command.Authorization,
		})
		if err != nil {
			return Appeal{}, err
		}
		result.AssessmentStatus = cleared.Status
		result.AssessmentVersion = cleared.Version
	}
	result.Status = AppealStatus(command.Decision)
	result.Response = command.Response
	result.ResolvedAt = timePointer(command.OccurredAt)
	if err := tx.Commit(ctx); err != nil {
		return Appeal{}, classifyDatabaseError("commit ratio appeal decision", err)
	}
	return result, nil
}

func (repository *PostgresRepository) Clear(ctx context.Context, command ClearCommand) (Assessment, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Assessment{}, fmt.Errorf("begin ratio assessment clear: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	assessment, err := clearAssessmentTx(ctx, tx, command)
	if err != nil {
		return Assessment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Assessment{}, classifyDatabaseError("commit ratio assessment clear", err)
	}
	return assessment, nil
}

// clearAssessmentTx is the one staff-clear transaction primitive used by a
// direct operator exception and an approved appeal. Keeping the projection and
// immutable transition together prevents the two workflows from drifting.
func clearAssessmentTx(ctx context.Context, tx pgx.Tx, command ClearCommand) (Assessment, error) {
	assessment, err := scanAssessment(tx.QueryRow(ctx, assessmentSelect+`
WHERE assessment.id = $1 FOR UPDATE OF assessment`, command.AssessmentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Assessment{}, ErrNotFound
	}
	if err != nil {
		return Assessment{}, err
	}
	if assessment.UserID == command.ActorID {
		return Assessment{}, ErrSelfTarget
	}
	if !assessment.Status.Active() {
		return Assessment{}, ErrNotActive
	}
	if assessment.Version != command.ExpectedVersion {
		return Assessment{}, ErrConflict
	}
	previous := assessment.Status
	row := tx.QueryRow(ctx, `
UPDATE ratio_watch.assessments
SET status = 'manually_cleared', resolved_at = $2, resolution_code = 'staff_cleared',
    resolution_reason = $3, resolved_by = $4,
    resolution_authorization_decision_id = $5,
    version = version + 1, updated_at = $2
WHERE id = $1 AND version = $6
RETURNING version`, command.AssessmentID, command.OccurredAt, command.Reason,
		command.ActorID, command.Authorization.ID, command.ExpectedVersion)
	if err := row.Scan(&assessment.Version); errors.Is(err, pgx.ErrNoRows) {
		return Assessment{}, ErrConflict
	} else if err != nil {
		return Assessment{}, classifyDatabaseError("clear ratio assessment", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO ratio_watch.assessment_transitions (
    assessment_id, from_status, to_status, credited_uploaded,
    charged_downloaded, ratio_basis_points, reason_code, reason,
    actor_id, authorization_decision_id, occurred_at
) VALUES ($1, $2, 'manually_cleared', $3, $4, $5, 'staff_cleared', $6, $7, $8, $9)`,
		assessment.ID, previous, assessment.CurrentCreditedUploaded,
		assessment.CurrentChargedDownloaded, assessment.CurrentRatioBasisPoints,
		command.Reason, command.ActorID, command.Authorization.ID, command.OccurredAt); err != nil {
		return Assessment{}, classifyDatabaseError("append ratio assessment clear transition", err)
	}
	assessment.Status = AssessmentManuallyCleared
	assessment.ResolvedAt = timePointer(command.OccurredAt)
	assessment.ResolutionCode = "staff_cleared"
	assessment.ResolutionReason = command.Reason
	assessment.ResolvedBy = &command.ActorID
	assessment.UpdatedAt = command.OccurredAt
	return assessment, nil
}

func (repository *PostgresRepository) Evaluate(ctx context.Context, now time.Time, batch int) (EvaluationResult, error) {
	if now.IsZero() || batch < 1 || batch > MaximumWorkerBatch {
		return EvaluationResult{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return EvaluationResult{}, fmt.Errorf("begin ratio evaluation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('peergo-ratio-watch-evaluator'))`).Scan(&locked); err != nil {
		return EvaluationResult{}, fmt.Errorf("lock ratio evaluator: %w", err)
	}
	if !locked {
		return EvaluationResult{Skipped: true}, nil
	}
	if _, err := tx.Exec(ctx, `
UPDATE ratio_watch.worker_state
SET last_started_at = $1, last_error_code = NULL
WHERE singleton = true`, now); err != nil {
		return EvaluationResult{}, fmt.Errorf("start ratio worker heartbeat: %w", err)
	}
	result := EvaluationResult{}
	activeRows, err := tx.Query(ctx, `
SELECT
    assessment.id, assessment.user_id, assessment.status,
    assessment.current_credited_uploaded, assessment.current_charged_downloaded,
    assessment.current_ratio_basis_points, assessment.restriction_started_at,
    assessment.version, assessment.deadline_at,
    revision.minimum_ratio_basis_points, revision.restriction_ratio_basis_points,
    users.status,
    COALESCE(access.vip_enabled AND (access.vip_until IS NULL OR access.vip_until > $1), false),
    GREATEST(assessment.current_credited_uploaded, COALESCE(totals.credited_uploaded, 0)),
    GREATEST(assessment.current_charged_downloaded, COALESCE(totals.charged_downloaded, 0))
FROM ratio_watch.assessments AS assessment
JOIN ratio_watch.policy_revisions AS revision ON revision.id = assessment.policy_revision_id
JOIN identity.users AS users ON users.id = assessment.user_id
LEFT JOIN identity.user_access_states AS access ON access.user_id = assessment.user_id
LEFT JOIN traffic.user_totals AS totals ON totals.user_id = assessment.user_id
WHERE assessment.status IN ('watching', 'warning', 'download_restricted')
ORDER BY assessment.deadline_at, assessment.id
LIMIT $2
FOR UPDATE OF assessment SKIP LOCKED`, now, batch)
	if err != nil {
		return EvaluationResult{}, fmt.Errorf("claim active ratio assessments: %w", err)
	}
	active := make([]evaluationRow, 0, batch)
	for activeRows.Next() {
		var row evaluationRow
		var restrictionAt pgtype.Timestamptz
		if err := activeRows.Scan(
			&row.ID, &row.UserID, &row.Status, &row.PreviousUploaded, &row.PreviousDownloaded,
			&row.PreviousRatio, &restrictionAt, &row.Version, &row.DeadlineAt,
			&row.MinimumRatio, &row.RestrictionRatio, &row.UserStatus,
			&row.VIPActive, &row.Uploaded, &row.Downloaded,
		); err != nil {
			activeRows.Close()
			return EvaluationResult{}, fmt.Errorf("scan active ratio assessment: %w", err)
		}
		if restrictionAt.Valid {
			value := restrictionAt.Time.UTC().Round(0)
			row.RestrictionStartedAt = &value
		}
		active = append(active, row)
	}
	if err := activeRows.Err(); err != nil {
		return EvaluationResult{}, fmt.Errorf("iterate active ratio assessments: %w", err)
	}
	activeRows.Close()
	for _, row := range active {
		result.Examined++
		transitioned, updateErr := evaluateActiveAssessment(ctx, tx, row, now)
		if updateErr != nil {
			return EvaluationResult{}, updateErr
		}
		if transitioned {
			result.Transitioned++
		}
	}
	var current PolicyRevision
	current, err = scanPolicyRevision(tx.QueryRow(ctx, policyRevisionSelect+`
WHERE revision.effective_at <= $1
ORDER BY revision.effective_at DESC, revision.rule_version DESC
LIMIT 1`, now), now)
	if err == nil && current.Enabled {
		candidateRows, queryErr := tx.Query(ctx, `
SELECT users.id, totals.credited_uploaded, totals.charged_downloaded
FROM identity.users AS users
JOIN traffic.user_totals AS totals ON totals.user_id = users.id
LEFT JOIN identity.user_access_states AS access ON access.user_id = users.id
WHERE users.status = 'active'
  AND totals.charged_downloaded >= $1
  AND totals.credited_uploaded::numeric * 10000
      < totals.charged_downloaded::numeric * $2
  AND NOT COALESCE(access.vip_enabled AND (access.vip_until IS NULL OR access.vip_until > $3), false)
  AND NOT COALESCE(access.download_restricted, false)
  AND NOT EXISTS (
      SELECT 1 FROM ratio_watch.assessments AS active
      WHERE active.user_id = users.id
        AND active.status IN ('watching', 'warning', 'download_restricted')
  )
  AND NOT EXISTS (
      SELECT 1 FROM ratio_watch.assessments AS cleared
      WHERE cleared.user_id = users.id
        AND cleared.policy_revision_id = $4
        AND cleared.status = 'manually_cleared'
  )
ORDER BY users.numeric_id, users.id
LIMIT $5
FOR UPDATE OF users SKIP LOCKED`, current.DownloadThresholdBytes, current.MinimumRatioBasisPoints,
			now, current.ID, batch)
		if queryErr != nil {
			return EvaluationResult{}, fmt.Errorf("claim ratio watch candidates: %w", queryErr)
		}
		type candidate struct {
			UserID               uuid.UUID
			Uploaded, Downloaded int64
		}
		candidates := make([]candidate, 0, batch)
		for candidateRows.Next() {
			var userID uuid.UUID
			var uploaded, downloaded int64
			if err := candidateRows.Scan(&userID, &uploaded, &downloaded); err != nil {
				candidateRows.Close()
				return EvaluationResult{}, fmt.Errorf("scan ratio watch candidate: %w", err)
			}
			candidates = append(candidates, candidate{UserID: userID, Uploaded: uploaded, Downloaded: downloaded})
		}
		if err := candidateRows.Err(); err != nil {
			return EvaluationResult{}, fmt.Errorf("iterate ratio watch candidates: %w", err)
		}
		candidateRows.Close()
		for _, candidate := range candidates {
			ratio := ratioBasisPoints(candidate.Uploaded, candidate.Downloaded)
			assessmentID := uuid.New()
			deadline := now.Add(time.Duration(current.WatchPeriodSeconds) * time.Second)
			if _, err := tx.Exec(ctx, `
INSERT INTO ratio_watch.assessments (
    id, user_id, policy_revision_id, status, started_at, deadline_at,
    opening_credited_uploaded, opening_charged_downloaded, opening_ratio_basis_points,
    current_credited_uploaded, current_charged_downloaded, current_ratio_basis_points,
    version, updated_at
) VALUES ($1, $2, $3, 'watching', $4, $5, $6, $7, $8, $6, $7, $8, 1, $4)`,
				assessmentID, candidate.UserID, current.ID, now, deadline,
				candidate.Uploaded, candidate.Downloaded, ratio); err != nil {
				return EvaluationResult{}, classifyDatabaseError("create ratio assessment", err)
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO ratio_watch.assessment_transitions (
    assessment_id, from_status, to_status, credited_uploaded,
    charged_downloaded, ratio_basis_points, reason_code, occurred_at
) VALUES ($1, NULL, 'watching', $2, $3, $4, 'entered_watch', $5)`,
				assessmentID, candidate.Uploaded, candidate.Downloaded, ratio, now); err != nil {
				return EvaluationResult{}, classifyDatabaseError("append ratio assessment opening", err)
			}
			result.Created++
			result.Transitioned++
		}
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return EvaluationResult{}, err
	}
	if _, err := tx.Exec(ctx, `
UPDATE ratio_watch.worker_state
SET last_completed_at = $1, last_error_code = NULL,
    last_examined = $2, last_created = $3, last_transitioned = $4,
    run_count = run_count + 1
WHERE singleton = true`, now, result.Examined, result.Created, result.Transitioned); err != nil {
		return EvaluationResult{}, fmt.Errorf("complete ratio worker heartbeat: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return EvaluationResult{}, classifyDatabaseError("commit ratio evaluation", err)
	}
	return result, nil
}

func (repository *PostgresRepository) MarkWorkerFailure(ctx context.Context, occurredAt time.Time, code string) error {
	if occurredAt.IsZero() || code == "" {
		return ErrInput
	}
	_, err := repository.pool.Exec(ctx, `
UPDATE ratio_watch.worker_state
SET last_error_code = $1, last_started_at = COALESCE(last_started_at, $2)
WHERE singleton = true`, code, occurredAt)
	if err != nil {
		return fmt.Errorf("record ratio worker failure: %w", err)
	}
	return nil
}

type evaluationRow struct {
	ID, UserID                           uuid.UUID
	Status                               AssessmentStatus
	PreviousUploaded, PreviousDownloaded int64
	PreviousRatio                        int64
	RestrictionStartedAt                 *time.Time
	Version                              int64
	DeadlineAt                           time.Time
	MinimumRatio, RestrictionRatio       int64
	UserStatus                           string
	VIPActive                            bool
	Uploaded, Downloaded                 int64
}

func evaluateActiveAssessment(ctx context.Context, tx pgx.Tx, row evaluationRow, now time.Time) (bool, error) {
	ratio := ratioBasisPoints(row.Uploaded, row.Downloaded)
	next := row.Status
	reasonCode := ""
	resolutionCode := ""
	var resolvedAt *time.Time
	restrictionAt := row.RestrictionStartedAt
	switch {
	case row.UserStatus != "active":
		next, reasonCode, resolutionCode = AssessmentIneligible, "account_ineligible", "account_ineligible"
		resolvedAt = timePointer(now)
	case row.VIPActive:
		next, reasonCode, resolutionCode = AssessmentVIPExempted, "vip_became_active", "vip_became_active"
		resolvedAt = timePointer(now)
	case ratio >= row.MinimumRatio:
		next, reasonCode, resolutionCode = AssessmentSatisfied, "ratio_recovered", "ratio_recovered"
		resolvedAt = timePointer(now)
	case row.Status == AssessmentWatching && !now.Before(row.DeadlineAt) && ratio < row.RestrictionRatio:
		next, reasonCode = AssessmentDownloadRestricted, "deadline_restricted"
		restrictionAt = timePointer(now)
	case row.Status == AssessmentWatching && !now.Before(row.DeadlineAt):
		next, reasonCode = AssessmentWarning, "deadline_warning"
	case row.Status == AssessmentWarning && ratio < row.RestrictionRatio:
		next, reasonCode = AssessmentDownloadRestricted, "warning_restricted"
		restrictionAt = timePointer(now)
	}
	if next == row.Status && row.Uploaded == row.PreviousUploaded && row.Downloaded == row.PreviousDownloaded && ratio == row.PreviousRatio {
		return false, nil
	}
	_, err := tx.Exec(ctx, `
UPDATE ratio_watch.assessments
SET status = $2, current_credited_uploaded = $3,
    current_charged_downloaded = $4, current_ratio_basis_points = $5,
    restriction_started_at = $6, resolved_at = $7, resolution_code = NULLIF($8, ''),
    version = version + 1, updated_at = $9
WHERE id = $1 AND version = $10`, row.ID, next, row.Uploaded, row.Downloaded, ratio,
		restrictionAt, resolvedAt, resolutionCode, now, row.Version)
	if err != nil {
		return false, classifyDatabaseError("advance ratio assessment", err)
	}
	if next != row.Status {
		if _, err := tx.Exec(ctx, `
INSERT INTO ratio_watch.assessment_transitions (
    assessment_id, from_status, to_status, credited_uploaded,
    charged_downloaded, ratio_basis_points, reason_code, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, row.ID, row.Status, next,
			row.Uploaded, row.Downloaded, ratio, reasonCode, now); err != nil {
			return false, classifyDatabaseError("append ratio assessment transition", err)
		}
		return true, nil
	}
	return false, nil
}

const policyRevisionSelect = `
SELECT
    revision.id, revision.rule_id, revision.rule_version, revision.enabled,
    revision.download_threshold_bytes, revision.minimum_ratio_basis_points,
    revision.watch_period_seconds, revision.restriction_ratio_basis_points,
    revision.vip_exempt, revision.effective_at, revision.reason,
    revision.actor_id, revision.authorization_decision_id,
    revision.command_sha256, revision.created_at
FROM ratio_watch.policy_revisions AS revision`

func scanPolicyRevision(scanner interface{ Scan(...any) error }, now time.Time) (PolicyRevision, error) {
	var result PolicyRevision
	var digest []byte
	if err := scanner.Scan(
		&result.ID, &result.RuleID, &result.RuleVersion, &result.Enabled,
		&result.DownloadThresholdBytes, &result.MinimumRatioBasisPoints,
		&result.WatchPeriodSeconds, &result.RestrictionRatioBasisPoints,
		&result.VIPExempt, &result.EffectiveAt, &result.Reason, &result.ActorID,
		&result.AuthorizationDecisionID, &digest, &result.CreatedAt,
	); err != nil {
		return PolicyRevision{}, err
	}
	if result.ID == uuid.Nil || result.ActorID == uuid.Nil || result.AuthorizationDecisionID == uuid.Nil ||
		len(digest) != sha256.Size || result.RuleID != DefaultRuleID {
		return PolicyRevision{}, ErrInvariant
	}
	copy(result.CommandSHA256[:], digest)
	result.TimelineState = TimelineScheduled
	if !now.Before(result.EffectiveAt) {
		result.TimelineState = TimelineActive
	}
	return result, nil
}

const assessmentSelect = `
SELECT
    assessment.id, assessment.user_id, users.numeric_id, users.username,
    assessment.policy_revision_id, revision.rule_version, assessment.status,
    assessment.started_at, assessment.deadline_at,
    assessment.opening_credited_uploaded, assessment.opening_charged_downloaded,
    assessment.opening_ratio_basis_points,
    assessment.current_credited_uploaded, assessment.current_charged_downloaded,
    assessment.current_ratio_basis_points, assessment.restriction_started_at,
    assessment.resolved_at, COALESCE(assessment.resolution_code, ''),
    COALESCE(assessment.resolution_reason, ''), assessment.resolved_by,
    assessment.version, assessment.updated_at,
    COALESCE(access.download_restricted, false)
FROM ratio_watch.assessments AS assessment
JOIN ratio_watch.policy_revisions AS revision ON revision.id = assessment.policy_revision_id
JOIN identity.users AS users ON users.id = assessment.user_id
LEFT JOIN identity.user_access_states AS access ON access.user_id = assessment.user_id`

const appealSelect = `
SELECT
    appeal.id, appeal.assessment_id, appeal.user_id,
    users.numeric_id, users.username, appeal.statement, appeal.created_at,
    COALESCE(resolution.outcome, 'pending'), resolution.response,
    resolution.created_at,
    assessment.status, assessment.version,
    assessment.current_credited_uploaded,
    assessment.current_charged_downloaded,
    assessment.current_ratio_basis_points,
    assessment.deadline_at, assessment.restriction_started_at,
    COALESCE(access.download_restricted, false)
FROM ratio_watch.appeals AS appeal
JOIN identity.users AS users ON users.id = appeal.user_id
JOIN ratio_watch.assessments AS assessment
  ON assessment.id = appeal.assessment_id
 AND assessment.user_id = appeal.user_id
LEFT JOIN ratio_watch.appeal_resolutions AS resolution
  ON resolution.appeal_id = appeal.id
LEFT JOIN identity.user_access_states AS access
  ON access.user_id = appeal.user_id`

func scanAssessment(scanner interface{ Scan(...any) error }) (Assessment, error) {
	var result Assessment
	var restrictionAt, resolvedAt pgtype.Timestamptz
	var resolvedBy pgtype.UUID
	if err := scanner.Scan(
		&result.ID, &result.UserID, &result.UserNumericID, &result.Username,
		&result.PolicyRevisionID, &result.PolicyVersion, &result.Status,
		&result.StartedAt, &result.DeadlineAt,
		&result.OpeningCreditedUploaded, &result.OpeningChargedDownloaded,
		&result.OpeningRatioBasisPoints, &result.CurrentCreditedUploaded,
		&result.CurrentChargedDownloaded, &result.CurrentRatioBasisPoints,
		&restrictionAt, &resolvedAt, &result.ResolutionCode, &result.ResolutionReason,
		&resolvedBy, &result.Version, &result.UpdatedAt, &result.LegacyDownloadRestricted,
	); err != nil {
		return Assessment{}, err
	}
	if restrictionAt.Valid {
		value := restrictionAt.Time.UTC().Round(0)
		result.RestrictionStartedAt = &value
	}
	if resolvedAt.Valid {
		value := resolvedAt.Time.UTC().Round(0)
		result.ResolvedAt = &value
	}
	if resolvedBy.Valid {
		value := uuid.UUID(resolvedBy.Bytes)
		result.ResolvedBy = &value
	}
	if result.ID == uuid.Nil || result.UserID == uuid.Nil || result.UserNumericID < 1 ||
		strings.TrimSpace(result.Username) == "" || result.PolicyVersion < 1 || result.Version < 1 {
		return Assessment{}, ErrInvariant
	}
	return result, nil
}

func scanAppeal(scanner interface{ Scan(...any) error }) (Appeal, error) {
	var result Appeal
	var response pgtype.Text
	var resolvedAt, restrictionAt pgtype.Timestamptz
	if err := scanner.Scan(
		&result.ID, &result.AssessmentID, &result.UserID,
		&result.UserNumericID, &result.Username, &result.Statement, &result.CreatedAt,
		&result.Status, &response, &resolvedAt,
		&result.AssessmentStatus, &result.AssessmentVersion,
		&result.CurrentCreditedUploaded, &result.CurrentChargedDownloaded,
		&result.CurrentRatioBasisPoints, &result.DeadlineAt, &restrictionAt,
		&result.LegacyDownloadRestricted,
	); err != nil {
		return Appeal{}, err
	}
	if response.Valid {
		result.Response = response.String
	}
	if resolvedAt.Valid {
		value := resolvedAt.Time.UTC().Round(0)
		result.ResolvedAt = &value
	}
	if restrictionAt.Valid {
		value := restrictionAt.Time.UTC().Round(0)
		result.RestrictionStartedAt = &value
	}
	if result.ID == uuid.Nil || result.AssessmentID == uuid.Nil || result.UserID == uuid.Nil ||
		result.UserNumericID < 1 || strings.TrimSpace(result.Username) == "" ||
		!validAppealStatement(result.Statement) || result.AssessmentVersion < 1 ||
		!validAppealProjection(MyAppeal{
			Status: result.Status, Statement: result.Statement, SubmittedAt: result.CreatedAt,
			ResolvedAt: result.ResolvedAt, Response: result.Response,
		}) {
		return Appeal{}, ErrInvariant
	}
	return result, nil
}

func scanWorkerState(scanner interface{ Scan(...any) error }, result *WorkerState) error {
	var started, completed pgtype.Timestamptz
	if err := scanner.Scan(&started, &completed, &result.LastErrorCode, &result.LastExamined,
		&result.LastCreated, &result.LastTransitioned, &result.RunCount); err != nil {
		return err
	}
	if started.Valid {
		value := started.Time.UTC().Round(0)
		result.LastStartedAt = &value
	}
	if completed.Valid {
		value := completed.Time.UTC().Round(0)
		result.LastCompletedAt = &value
	}
	return nil
}

func assessmentFilterSQL(filter AssessmentFilter) string {
	switch filter {
	case AssessmentFilterActive:
		return `assessment.status IN ('watching', 'warning', 'download_restricted')`
	case AssessmentFilterWatching:
		return `assessment.status = 'watching'`
	case AssessmentFilterWarning:
		return `assessment.status = 'warning'`
	case AssessmentFilterRestricted:
		return `assessment.status = 'download_restricted'`
	case AssessmentFilterResolved:
		return `assessment.status IN ('satisfied', 'manually_cleared', 'vip_exempted', 'ineligible')`
	default:
		return `true`
	}
}

func appealFilterSQL(filter AppealFilter) string {
	switch filter {
	case AppealFilterPending:
		return `resolution.id IS NULL`
	case AppealFilterResolved:
		return `resolution.id IS NOT NULL`
	default:
		return `true`
	}
}

func validAppealProjection(appeal MyAppeal) bool {
	if !validAppealStatement(appeal.Statement) || appeal.SubmittedAt.IsZero() {
		return false
	}
	switch appeal.Status {
	case AppealPending:
		return appeal.ResolvedAt == nil && appeal.Response == ""
	case AppealApproved, AppealRejected:
		return appeal.ResolvedAt != nil && validReason(appeal.Response)
	case AppealAssessmentResolved:
		return appeal.ResolvedAt != nil && appeal.Response == ""
	default:
		return false
	}
}

func ratioBasisPoints(uploaded, downloaded int64) int64 {
	if downloaded <= 0 || uploaded <= 0 {
		return 0
	}
	numerator := new(big.Int).Mul(big.NewInt(uploaded), big.NewInt(RatioScale))
	numerator.Div(numerator, big.NewInt(downloaded))
	if numerator.Cmp(big.NewInt(MaximumRatioBPS)) > 0 {
		return MaximumRatioBPS
	}
	return numerator.Int64()
}

func samePolicy(left, right PolicyInput) bool {
	return left.Enabled == right.Enabled &&
		left.DownloadThresholdBytes == right.DownloadThresholdBytes &&
		left.MinimumRatioBasisPoints == right.MinimumRatioBasisPoints &&
		left.WatchPeriodSeconds == right.WatchPeriodSeconds &&
		left.RestrictionRatioBasisPoints == right.RestrictionRatioBasisPoints
}

func sameIssue(existing PolicyRevision, command IssueCommand) bool {
	return existing.ID == command.RevisionID && existing.ActorID == command.ActorID &&
		existing.EffectiveAt.Equal(command.EffectiveAt) && existing.Reason == command.Reason &&
		samePolicy(existing.PolicyInput, command.Policy)
}

func timePointer(value time.Time) *time.Time {
	value = value.UTC().Round(0)
	return &value
}

func classifyDatabaseError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505", "40001", "40P01", "P0001":
			return fmt.Errorf("%w: %s: %v", ErrConflict, operation, err)
		case "23503", "23514":
			return fmt.Errorf("%w: %s: %v", ErrInvariant, operation, err)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ Repository = (*PostgresRepository)(nil)
var _ Evaluator = (*PostgresRepository)(nil)
