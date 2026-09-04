package workgroups

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const MaximumContributionEnforcementBatch = 5000

type contributionEnforcementCandidate struct {
	MembershipID       uuid.UUID
	UserID             uuid.UUID
	MembershipVersion  int64
	TenureTransitionID uuid.UUID
	PeriodStartsAt     time.Time
	PolicyRevision     int64
	TargetValue        int64
	AllowedMisses      int32
	CurrentValue       int64
}

// EvaluateContributionEnforcement settles only completed UTC calendar months.
// The first partial month of a joined or reactivated tenure is deliberately
// excluded, and publication evidence is counted from its immutable source.
func (repository *PostgresRepository) EvaluateContributionEnforcement(ctx context.Context, now time.Time, batch int) (ContributionEnforcementResult, error) {
	if now.IsZero() || batch < 1 || batch > MaximumContributionEnforcementBatch {
		return ContributionEnforcementResult{}, ErrInput
	}
	now = canonicalTime(now)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ContributionEnforcementResult{}, fmt.Errorf("begin workgroup contribution enforcement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('peergo-workgroup-contribution-enforcement'))`).Scan(&locked); err != nil {
		return ContributionEnforcementResult{}, fmt.Errorf("lock workgroup contribution enforcement: %w", err)
	}
	if !locked {
		return ContributionEnforcementResult{Skipped: true}, nil
	}
	if _, err := tx.Exec(ctx, `
UPDATE workgroups.contribution_enforcement_worker_state
SET last_started_at = $1, last_error_code = NULL
WHERE singleton = true`, now); err != nil {
		return ContributionEnforcementResult{}, fmt.Errorf("start workgroup contribution enforcement heartbeat: %w", err)
	}

	rows, err := tx.Query(ctx, `
WITH bounds AS (
    SELECT (
        date_trunc('month', $1::timestamptz AT TIME ZONE 'UTC')
        AT TIME ZONE 'UTC'
    ) - interval '1 month' AS latest_closed_month
), active_memberships AS (
    SELECT membership.id, membership.user_id, membership.version,
           tenure.id AS tenure_transition_id,
           tenure.occurred_at AS tenure_started_at
    FROM workgroups.memberships AS membership
    JOIN LATERAL (
        SELECT transition.id, transition.to_status, transition.occurred_at
        FROM workgroups.membership_transitions AS transition
        WHERE transition.membership_id = membership.id
        ORDER BY transition.occurred_at DESC,
                 transition.state_version DESC, transition.id DESC
        LIMIT 1
    ) AS tenure ON tenure.to_status = 'active'
    WHERE membership.group_kind = 'reseed'
      AND membership.status = 'active'
), eligible_months AS (
    SELECT membership.*,
           month_start.period_starts_at
    FROM active_memberships AS membership
    CROSS JOIN bounds
    CROSS JOIN LATERAL generate_series(
        CASE
            WHEN membership.tenure_started_at = (
                date_trunc('month', membership.tenure_started_at AT TIME ZONE 'UTC')
                AT TIME ZONE 'UTC'
            ) THEN membership.tenure_started_at
            ELSE (
                date_trunc('month', membership.tenure_started_at AT TIME ZONE 'UTC')
                AT TIME ZONE 'UTC'
            ) + interval '1 month'
        END,
        bounds.latest_closed_month,
        interval '1 month'
    ) AS month_start(period_starts_at)
)
SELECT month.membership_id, month.user_id, month.version,
       month.tenure_transition_id, month.period_starts_at,
       policy.revision, policy.target_value, policy.allowed_misses,
       COALESCE((
           SELECT count(*)::bigint
           FROM review.torrent_decisions AS decision
           JOIN workgroups.membership_transitions AS publish_transition
             ON publish_transition.id = decision.membership_transition_id
           WHERE decision.resolution_source = 'trusted_workgroup'
             AND publish_transition.membership_id = month.membership_id
             AND decision.occurred_at >= month.period_starts_at
             AND decision.occurred_at < month.period_starts_at + interval '1 month'
       ), 0)::bigint AS current_value
FROM eligible_months AS month
JOIN LATERAL (
    SELECT revision, target_value, enforcement_mode, allowed_misses
    FROM workgroups.contribution_policy_revisions
    WHERE group_kind = 'reseed'
      AND effective_from <= month.period_starts_at
    ORDER BY effective_from DESC, revision DESC
    LIMIT 1
) AS policy ON policy.enforcement_mode = 'miss_limit'
WHERE NOT EXISTS (
    SELECT 1
    FROM workgroups.contribution_assessments AS assessment
    WHERE assessment.membership_id = month.membership_id
      AND assessment.period_starts_at = month.period_starts_at
)
ORDER BY month.period_starts_at, month.membership_id
LIMIT $2`, now, batch)
	if err != nil {
		return ContributionEnforcementResult{}, fmt.Errorf("claim workgroup contribution periods: %w", err)
	}
	candidates := make([]contributionEnforcementCandidate, 0, batch)
	for rows.Next() {
		var candidate contributionEnforcementCandidate
		if err := rows.Scan(
			&candidate.MembershipID, &candidate.UserID,
			&candidate.MembershipVersion, &candidate.TenureTransitionID,
			&candidate.PeriodStartsAt, &candidate.PolicyRevision,
			&candidate.TargetValue, &candidate.AllowedMisses,
			&candidate.CurrentValue,
		); err != nil {
			rows.Close()
			return ContributionEnforcementResult{}, fmt.Errorf("scan workgroup contribution period: %w", err)
		}
		candidate.PeriodStartsAt = canonicalTime(candidate.PeriodStartsAt)
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ContributionEnforcementResult{}, fmt.Errorf("iterate workgroup contribution periods: %w", err)
	}
	rows.Close()

	result := ContributionEnforcementResult{}
	for _, candidate := range candidates {
		result.Examined++
		recorded, action, evaluateErr := evaluateContributionCandidate(ctx, tx, candidate, now)
		if evaluateErr != nil {
			return ContributionEnforcementResult{}, evaluateErr
		}
		if !recorded {
			continue
		}
		result.Recorded++
		switch action {
		case ContributionDisciplinaryMarked:
			result.Marked++
		case ContributionDisciplinaryMembershipEnded:
			result.Ended++
		}
	}
	if _, err := tx.Exec(ctx, `
UPDATE workgroups.contribution_enforcement_worker_state
SET last_completed_at = $1, last_error_code = NULL,
    last_examined = $2, last_recorded = $3,
    last_marked = $4, last_ended = $5,
    run_count = run_count + 1
WHERE singleton = true`, now, result.Examined, result.Recorded, result.Marked, result.Ended); err != nil {
		return ContributionEnforcementResult{}, fmt.Errorf("complete workgroup contribution enforcement heartbeat: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ContributionEnforcementResult{}, fmt.Errorf("commit workgroup contribution enforcement: %w", err)
	}
	return result, nil
}

func evaluateContributionCandidate(ctx context.Context, tx pgx.Tx, candidate contributionEnforcementCandidate, now time.Time) (bool, ContributionDisciplinaryAction, error) {
	var status MembershipStatus
	var version int64
	if err := tx.QueryRow(ctx, `
SELECT status, version
FROM workgroups.memberships
WHERE id = $1 AND group_kind = 'reseed'
FOR UPDATE`, candidate.MembershipID).Scan(&status, &version); errors.Is(err, pgx.ErrNoRows) {
		return false, ContributionDisciplinaryNone, nil
	} else if err != nil {
		return false, ContributionDisciplinaryNone, fmt.Errorf("lock reseed membership for contribution assessment: %w", err)
	}
	if status != MembershipActive || version != candidate.MembershipVersion {
		return false, ContributionDisciplinaryNone, nil
	}

	var currentTenureID uuid.UUID
	var currentTenureStatus MembershipStatus
	if err := tx.QueryRow(ctx, `
SELECT id, to_status
FROM workgroups.membership_transitions
WHERE membership_id = $1
ORDER BY occurred_at DESC, state_version DESC, id DESC
LIMIT 1`, candidate.MembershipID).Scan(&currentTenureID, &currentTenureStatus); err != nil {
		return false, ContributionDisciplinaryNone, fmt.Errorf("read current reseed membership tenure: %w", err)
	}
	if currentTenureStatus != MembershipActive || currentTenureID != candidate.TenureTransitionID {
		return false, ContributionDisciplinaryNone, nil
	}

	var previousMisses int32
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(max(miss_count), 0)::integer
FROM workgroups.contribution_assessments
WHERE membership_id = $1 AND tenure_transition_id = $2`,
		candidate.MembershipID, candidate.TenureTransitionID,
	).Scan(&previousMisses); err != nil {
		return false, ContributionDisciplinaryNone, fmt.Errorf("read previous reseed contribution marks: %w", err)
	}

	state := ContributionAssessmentMet
	explanation := ContributionExplanationTargetMet
	missCount := previousMisses
	action := ContributionDisciplinaryNone
	if candidate.CurrentValue < candidate.TargetValue {
		state = ContributionAssessmentNotMet
		explanation = ContributionExplanationBelowTarget
		if candidate.CurrentValue == 0 {
			explanation = ContributionExplanationNoContribution
		}
		missCount, action = contributionDiscipline(previousMisses, candidate.AllowedMisses)
	}

	reason := contributionAssessmentReason(candidate, state, missCount, action)
	var membershipTransitionID *uuid.UUID
	if action == ContributionDisciplinaryMembershipEnded {
		transitionID := uuid.New()
		newVersion := version + 1
		command, err := tx.Exec(ctx, `
UPDATE workgroups.memberships
SET status = 'ended', version = version + 1,
    ended_at = $1, updated_at = $1
WHERE id = $2 AND status = 'active' AND version = $3`, now, candidate.MembershipID, version)
		if err != nil {
			return false, ContributionDisciplinaryNone, fmt.Errorf("end reseed membership after repeated contribution misses: %w", err)
		}
		if command.RowsAffected() != 1 {
			return false, ContributionDisciplinaryNone, nil
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO workgroups.membership_transitions (
    id, membership_id, group_kind, user_id, transition,
    from_status, to_status, actor_id, source, source_application_id,
    reason, authorization_decision_id, state_version, occurred_at
) VALUES (
    $1, $2, 'reseed', $3, 'ended',
    'active', 'ended', NULL, 'automatic_contribution', NULL,
    $4, NULL, $5, $6
)`, transitionID, candidate.MembershipID, candidate.UserID, reason, newVersion, now); err != nil {
			return false, ContributionDisciplinaryNone, fmt.Errorf("append automatic reseed membership transition: %w", err)
		}
		membershipTransitionID = &transitionID
	}

	periodEnd := candidate.PeriodStartsAt.AddDate(0, 1, 0)
	assessmentID := uuid.New()
	if _, err := tx.Exec(ctx, `
INSERT INTO workgroups.contribution_assessments (
    id, membership_id, tenure_transition_id, group_kind,
    recipient_user_id, period_starts_at, period_ends_at,
    metric, policy_revision, observed_at, evidence_through,
    evidence_state, current_value, target_value, assessment_state,
    explanation_code, miss_count, allowed_misses, disciplinary_action,
    membership_transition_id, reason, assessed_at
) VALUES (
    $1, $2, $3, 'reseed',
    $4, $5, $6,
    'trusted_torrents_published', $7, $6, $6,
    'complete', $8, $9, $10,
    $11, $12, $13, $14,
    $15, $16, $17
)`, assessmentID, candidate.MembershipID, candidate.TenureTransitionID,
		candidate.UserID, candidate.PeriodStartsAt, periodEnd,
		candidate.PolicyRevision, candidate.CurrentValue, candidate.TargetValue,
		state, explanation, missCount, candidate.AllowedMisses, action,
		membershipTransitionID, reason, now); err != nil {
		return false, ContributionDisciplinaryNone, fmt.Errorf("record reseed contribution assessment: %w", err)
	}
	return true, action, nil
}

func contributionDiscipline(previousMisses, allowedMisses int32) (int32, ContributionDisciplinaryAction) {
	missCount := previousMisses + 1
	if missCount > allowedMisses {
		return missCount, ContributionDisciplinaryMembershipEnded
	}
	return missCount, ContributionDisciplinaryMarked
}

func contributionAssessmentReason(candidate contributionEnforcementCandidate, state ContributionAssessmentState, missCount int32, action ContributionDisciplinaryAction) string {
	month := candidate.PeriodStartsAt.Format("2006-01")
	if state == ContributionAssessmentMet {
		return fmt.Sprintf("%s 转种组月度要求已达标：%d/%d；累计未达标 %d 次，资格继续有效。", month, candidate.CurrentValue, candidate.TargetValue, missCount)
	}
	if action == ContributionDisciplinaryMembershipEnded {
		return fmt.Sprintf("%s 转种组月度要求未达标：%d/%d；这是第 %d 次未达标，已超过允许的 %d 次，系统已自动结束转种组资格。", month, candidate.CurrentValue, candidate.TargetValue, missCount, candidate.AllowedMisses)
	}
	return fmt.Sprintf("%s 转种组月度要求未达标：%d/%d；已记录第 %d 次未达标，允许 %d 次，第 %d 次将自动结束资格。", month, candidate.CurrentValue, candidate.TargetValue, missCount, candidate.AllowedMisses, candidate.AllowedMisses+1)
}

func (repository *PostgresRepository) MarkContributionEnforcementFailure(ctx context.Context, occurredAt time.Time, code string) error {
	if occurredAt.IsZero() || code == "" {
		return ErrInput
	}
	_, err := repository.pool.Exec(ctx, `
UPDATE workgroups.contribution_enforcement_worker_state
SET last_error_code = $1, last_started_at = COALESCE(last_started_at, $2)
WHERE singleton = true`, code, canonicalTime(occurredAt))
	if err != nil {
		return fmt.Errorf("record workgroup contribution enforcement failure: %w", err)
	}
	return nil
}
