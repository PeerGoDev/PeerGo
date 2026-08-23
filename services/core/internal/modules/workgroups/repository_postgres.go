package workgroups

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/workgroupbenefitv1"
)

const applicationSelect = `
SELECT application.id, application.group_kind, application.applicant_id,
       users.numeric_id, users.username, users.display_name,
       application.statement, application.status, application.policy_revision,
       application.eligibility_snapshot, application.version,
       application.submitted_at, application.decided_at
FROM workgroups.applications AS application
JOIN identity.users AS users ON users.id = application.applicant_id`

const membershipSelect = `
SELECT membership.id, membership.group_kind, membership.user_id,
       users.numeric_id, users.username, users.display_name,
       membership.status, membership.source, membership.version,
       membership.started_at, membership.ended_at, membership.updated_at,
       reviewer.source_status, reviewer.source_activity_status,
       reviewer.source_total_reviews, reviewer.source_accurate_count,
       reviewer.source_last_activity_at
FROM workgroups.memberships AS membership
JOIN identity.users AS users ON users.id = membership.user_id
LEFT JOIN migration.legacy_reviewer_openings AS reviewer
  ON reviewer.membership_id = membership.id`

const contributionPolicyRevisionSelect = `
SELECT policy.group_kind, policy.revision, policy.metric, policy.period_kind,
       policy.target_value, policy.enforcement_mode, policy.effective_from,
       policy.source_kind, COALESCE(policy.reason, '首版观察目标'),
       policy.issued_by, policy.request_id, policy.created_at
FROM workgroups.contribution_policy_revisions AS policy`

const contributionReminderSelect = `
SELECT reminder.id, reminder.membership_id, reminder.group_kind,
       reminder.recipient_user_id, reminder.metric, reminder.policy_revision,
       reminder.period_starts_at, reminder.period_ends_at,
       reminder.observed_at, reminder.evidence_through,
       reminder.evidence_state, reminder.current_value,
       reminder.target_value, reminder.assessment_state,
       reminder.explanation_code, reminder.full_period_active,
       reminder.reason, reminder.issued_by, reminder.created_at,
       notification.id, notification.read_at
FROM workgroups.contribution_reminders AS reminder
JOIN community.workgroup_contribution_notifications AS notification
  ON notification.reminder_id = reminder.id
 AND notification.recipient_user_id = reminder.recipient_user_id`

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (repository *PostgresRepository) MyOverview(ctx context.Context, userID uuid.UUID, asOf time.Time) (MyOverview, error) {
	definitions, err := repository.definitions(ctx)
	if err != nil {
		return MyOverview{}, err
	}
	memberships, err := repository.membershipsByUser(ctx, userID)
	if err != nil {
		return MyOverview{}, err
	}
	applications, err := repository.latestApplicationsByUser(ctx, userID)
	if err != nil {
		return MyOverview{}, err
	}
	eligibility, err := repository.reviewerEligibility(ctx, userID, asOf)
	if err != nil {
		return MyOverview{}, err
	}

	result := MyOverview{Items: make([]MyGroup, 0, len(definitions))}
	for _, definition := range definitions {
		item := MyGroup{Definition: definition}
		if membership, ok := memberships[definition.Kind]; ok {
			membershipCopy := membership
			item.Membership = &membershipCopy
		}
		if application, ok := applications[definition.Kind]; ok {
			applicationCopy := application
			item.Application = &applicationCopy
		}
		if definition.Kind == GroupReview {
			eligibilityCopy := eligibility
			item.Eligibility = &eligibilityCopy
		}
		result.Items = append(result.Items, item)
	}
	items := make([]Membership, 0, len(result.Items))
	for _, item := range result.Items {
		if item.Membership != nil && item.Membership.Status != MembershipEnded {
			items = append(items, *item.Membership)
		}
	}
	if err := repository.attachContributionProgress(ctx, items, asOf); err != nil {
		return MyOverview{}, err
	}
	progressByID := make(map[uuid.UUID]*ContributionProgress, len(items))
	for index := range items {
		progressByID[items[index].ID] = items[index].Contribution
	}
	for index := range result.Items {
		if result.Items[index].Membership != nil {
			result.Items[index].Membership.Contribution = progressByID[result.Items[index].Membership.ID]
		}
	}
	return result, nil
}

func (repository *PostgresRepository) SubmitApplication(ctx context.Context, command SubmitApplicationCommand) (Application, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Application{}, fmt.Errorf("begin workgroup application: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	existing, err := scanApplication(tx.QueryRow(ctx, applicationSelect+` WHERE application.request_id = $1`, command.RequestID))
	if err == nil {
		if existing.ApplicantID != command.ApplicantID || existing.GroupKind != command.GroupKind || existing.Statement != command.Statement {
			return Application{}, ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return Application{}, fmt.Errorf("commit workgroup application replay: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Application{}, fmt.Errorf("read workgroup application replay: %w", err)
	}

	var enabled bool
	var joinMode string
	if err := tx.QueryRow(ctx, `SELECT enabled, join_mode FROM workgroups.definitions WHERE kind = $1`, command.GroupKind).Scan(&enabled, &joinMode); errors.Is(err, pgx.ErrNoRows) {
		return Application{}, ErrGroupNotFound
	} else if err != nil {
		return Application{}, fmt.Errorf("read workgroup definition: %w", err)
	}
	if !enabled || JoinMode(joinMode) != JoinApplication {
		return Application{}, ErrApplicationNotAllowed
	}

	var activeMembership bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM workgroups.memberships
    WHERE user_id = $1 AND group_kind = $2 AND status = 'active'
)`, command.ApplicantID, command.GroupKind).Scan(&activeMembership); err != nil {
		return Application{}, fmt.Errorf("check active workgroup membership: %w", err)
	}
	if activeMembership {
		return Application{}, ErrMembershipAlreadyActive
	}

	eligibility, err := reviewerEligibilityWithQuerier(ctx, tx, command.ApplicantID, command.OccurredAt)
	if err != nil {
		return Application{}, err
	}
	if command.GroupKind == GroupReview && !eligibility.Eligible {
		return Application{}, ErrApplicationNotEligible
	}
	snapshot, err := json.Marshal(eligibility)
	if err != nil {
		return Application{}, fmt.Errorf("encode reviewer eligibility: %w", err)
	}

	_, err = tx.Exec(ctx, `
INSERT INTO workgroups.applications (
    id, request_id, group_kind, applicant_id, statement, status,
    policy_revision, eligibility_snapshot,
    submission_authorization_decision_id, version,
    submitted_at, updated_at
) VALUES ($1, $2, $3, $4, $5, 'pending', $6, $7, $8, 1, $9, $9)`,
		command.ApplicationID, command.RequestID, command.GroupKind, command.ApplicantID,
		command.Statement, eligibility.PolicyRevision, string(snapshot),
		command.AuthorizationDecisionID, command.OccurredAt)
	if err != nil {
		if constraintName(err) == "workgroup_applications_one_pending_idx" {
			return Application{}, ErrApplicationPending
		}
		return Application{}, fmt.Errorf("insert workgroup application: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO workgroups.application_transitions (
    id, application_id, transition, from_status, to_status,
    actor_id, reason, authorization_decision_id, state_version, occurred_at
) VALUES ($1, $2, 'submitted', NULL, 'pending', $3, 'member_application', $4, 1, $5)`,
		uuid.New(), command.ApplicationID, command.ApplicantID,
		command.AuthorizationDecisionID, command.OccurredAt); err != nil {
		return Application{}, fmt.Errorf("append workgroup application submission: %w", err)
	}
	created, err := scanApplication(tx.QueryRow(ctx, applicationSelect+` WHERE application.id = $1`, command.ApplicationID))
	if err != nil {
		return Application{}, fmt.Errorf("read created workgroup application: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Application{}, fmt.Errorf("commit workgroup application: %w", err)
	}
	return created, nil
}

func (repository *PostgresRepository) AdminOverview(ctx context.Context, asOf time.Time) (AdminOverview, error) {
	definitions, err := repository.definitions(ctx)
	if err != nil {
		return AdminOverview{}, err
	}
	result := AdminOverview{Definitions: definitions, ActiveByKind: make(map[GroupKind]int64, len(definitions))}
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM workgroups.applications WHERE status = 'pending'`).Scan(&result.PendingApplications); err != nil {
		return AdminOverview{}, fmt.Errorf("count pending workgroup applications: %w", err)
	}
	rows, err := repository.pool.Query(ctx, `
SELECT group_kind, count(*) FROM workgroups.memberships
WHERE status = 'active' GROUP BY group_kind`)
	if err != nil {
		return AdminOverview{}, fmt.Errorf("count active workgroup members: %w", err)
	}
	for rows.Next() {
		var kind GroupKind
		var count int64
		if err := rows.Scan(&kind, &count); err != nil {
			return AdminOverview{}, fmt.Errorf("scan active workgroup count: %w", err)
		}
		result.ActiveByKind[kind] = count
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminOverview{}, err
	}
	rows.Close()
	for _, definition := range definitions {
		summary, err := repository.contributionSummary(ctx, definition.Kind, asOf)
		if err != nil {
			return AdminOverview{}, err
		}
		result.ContributionSummaries = append(result.ContributionSummaries, summary)
	}
	return result, nil
}

func (repository *PostgresRepository) ListApplications(ctx context.Context, status ApplicationStatus, limit, offset int) (ApplicationPage, error) {
	page := ApplicationPage{Limit: limit, Offset: offset}
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM workgroups.applications WHERE ($1 = '' OR status = $1)`, status).Scan(&page.Total); err != nil {
		return ApplicationPage{}, fmt.Errorf("count workgroup applications: %w", err)
	}
	rows, err := repository.pool.Query(ctx, applicationSelect+`
WHERE ($1 = '' OR application.status = $1)
ORDER BY CASE application.status WHEN 'pending' THEN 0 ELSE 1 END,
         application.submitted_at DESC, application.id DESC
LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return ApplicationPage{}, fmt.Errorf("list workgroup applications: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanApplication(rows)
		if err != nil {
			return ApplicationPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

func (repository *PostgresRepository) DecideApplication(ctx context.Context, command DecideApplicationCommand) (Application, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Application{}, fmt.Errorf("begin workgroup application decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	application, err := scanApplication(tx.QueryRow(ctx, applicationSelect+` WHERE application.id = $1 FOR UPDATE OF application`, command.ApplicationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Application{}, ErrApplicationNotFound
	}
	if err != nil {
		return Application{}, fmt.Errorf("lock workgroup application: %w", err)
	}
	if application.Status != ApplicationPending || application.Version != command.ExpectedVersion {
		return Application{}, ErrApplicationConflict
	}

	status := ApplicationRejected
	transition := "rejected"
	if command.Approve {
		status = ApplicationApproved
		transition = "approved"
		if _, err := ensureApplicationMembership(ctx, tx, application, command); err != nil {
			return Application{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
UPDATE workgroups.applications
SET status = $1, version = version + 1, decided_at = $2, updated_at = $2
WHERE id = $3 AND status = 'pending' AND version = $4`,
		status, command.OccurredAt, command.ApplicationID, command.ExpectedVersion); err != nil {
		return Application{}, fmt.Errorf("update workgroup application decision: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO workgroups.application_transitions (
    id, application_id, transition, from_status, to_status,
    actor_id, reason, authorization_decision_id, state_version, occurred_at
) VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7, 2, $8)`,
		command.DecisionID, command.ApplicationID, transition, status,
		command.ActorID, command.Reason, command.AuthorizationDecisionID,
		command.OccurredAt); err != nil {
		if constraintName(err) == "application_transitions_pkey" {
			return Application{}, ErrIdempotencyConflict
		}
		return Application{}, fmt.Errorf("append workgroup application decision: %w", err)
	}
	result, err := scanApplication(tx.QueryRow(ctx, applicationSelect+` WHERE application.id = $1`, command.ApplicationID))
	if err != nil {
		return Application{}, fmt.Errorf("read decided workgroup application: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Application{}, fmt.Errorf("commit workgroup application decision: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) ListMemberships(ctx context.Context, kind GroupKind, status MembershipStatus, limit, offset int, asOf time.Time) (MembershipPage, error) {
	page := MembershipPage{Limit: limit, Offset: offset}
	if err := repository.pool.QueryRow(ctx, `
SELECT count(*) FROM workgroups.memberships
WHERE group_kind = $1 AND ($2 = '' OR status = $2)`, kind, status).Scan(&page.Total); err != nil {
		return MembershipPage{}, fmt.Errorf("count workgroup memberships: %w", err)
	}
	rows, err := repository.pool.Query(ctx, membershipSelect+`
WHERE membership.group_kind = $1 AND ($2 = '' OR membership.status = $2)
ORDER BY CASE membership.status WHEN 'active' THEN 0 WHEN 'suspended' THEN 1 ELSE 2 END,
         membership.started_at DESC, membership.id DESC
LIMIT $3 OFFSET $4`, kind, status, limit, offset)
	if err != nil {
		return MembershipPage{}, fmt.Errorf("list workgroup memberships: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanMembership(rows)
		if err != nil {
			return MembershipPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return MembershipPage{}, err
	}
	rows.Close()
	if err := repository.attachContributionProgress(ctx, page.Items, asOf); err != nil {
		return MembershipPage{}, err
	}
	return page, nil
}

func (repository *PostgresRepository) attachContributionProgress(ctx context.Context, memberships []Membership, asOf time.Time) error {
	if len(memberships) == 0 {
		return nil
	}
	asOf = asOf.UTC().Truncate(time.Microsecond)
	periodStart := time.Date(asOf.Year(), asOf.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)
	observedAt := asOf
	if observedAt.After(periodEnd) {
		observedAt = periodEnd
	}

	byKind := make(map[GroupKind][]int)
	for index := range memberships {
		if memberships[index].Status == MembershipEnded {
			continue
		}
		byKind[memberships[index].GroupKind] = append(byKind[memberships[index].GroupKind], index)
	}
	for kind, indexes := range byKind {
		policy, err := repository.contributionPolicy(ctx, kind, periodStart)
		if err != nil {
			return err
		}
		membershipIDs := make([]uuid.UUID, 0, len(indexes))
		for _, index := range indexes {
			membershipIDs = append(membershipIDs, memberships[index].ID)
		}
		values, evidenceThrough, err := repository.contributionValues(ctx, policy.Metric, membershipIDs, periodStart, observedAt)
		if err != nil {
			return err
		}
		for _, index := range indexes {
			value := values[memberships[index].ID]
			memberships[index].Contribution = &ContributionProgress{
				GroupKind: kind, Metric: policy.Metric, PolicyRevision: policy.Revision,
				PeriodKind: policy.PeriodKind, PeriodStartsAt: periodStart, PeriodEndsAt: periodEnd,
				ObservedAt: observedAt, EvidenceThrough: evidenceThrough,
				CurrentValue: value, TargetValue: policy.TargetValue,
				Met: value >= policy.TargetValue, EnforcementMode: policy.EnforcementMode,
			}
		}
	}
	return nil
}

func (repository *PostgresRepository) contributionSummary(ctx context.Context, kind GroupKind, asOf time.Time) (ContributionSummary, error) {
	asOf = asOf.UTC().Truncate(time.Microsecond)
	periodStart := calendarMonthStart(asOf)
	periodEnd := periodStart.AddDate(0, 1, 0)
	policy, err := repository.contributionPolicy(ctx, kind, periodStart)
	if err != nil {
		return ContributionSummary{}, err
	}
	rows, err := repository.pool.Query(ctx, `
SELECT id FROM workgroups.memberships
WHERE group_kind = $1 AND status = 'active'
ORDER BY id`, kind)
	if err != nil {
		return ContributionSummary{}, fmt.Errorf("list active workgroup members for contribution summary: %w", err)
	}
	membershipIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var membershipID uuid.UUID
		if err := rows.Scan(&membershipID); err != nil {
			rows.Close()
			return ContributionSummary{}, fmt.Errorf("scan contribution summary member: %w", err)
		}
		membershipIDs = append(membershipIDs, membershipID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ContributionSummary{}, fmt.Errorf("iterate contribution summary members: %w", err)
	}
	rows.Close()
	values, evidenceThrough, err := repository.contributionValues(ctx, policy.Metric, membershipIDs, periodStart, asOf)
	if err != nil {
		return ContributionSummary{}, err
	}
	summary := ContributionSummary{
		GroupKind: kind, Metric: policy.Metric, PolicyRevision: policy.Revision,
		PeriodStartsAt: periodStart, PeriodEndsAt: periodEnd, ObservedAt: asOf,
		EvidenceThrough: evidenceThrough, ActiveMembers: int64(len(membershipIDs)),
		TargetValue: policy.TargetValue,
	}
	for _, membershipID := range membershipIDs {
		value := values[membershipID]
		if value > 0 {
			summary.ContributingMembers++
		}
		if value >= policy.TargetValue {
			summary.MetMembers++
		}
		if value > math.MaxInt64-summary.TotalValue {
			return ContributionSummary{}, errors.New("workgroup contribution summary overflows int64")
		}
		summary.TotalValue += value
	}
	return summary, nil
}

func (repository *PostgresRepository) ListMyContributionCycles(ctx context.Context, userID uuid.UUID, kind GroupKind, limit int, asOf time.Time) (ContributionCyclePage, error) {
	var membershipID uuid.UUID
	err := repository.pool.QueryRow(ctx, `
SELECT id FROM workgroups.memberships
WHERE user_id = $1 AND group_kind = $2`, userID, kind).Scan(&membershipID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ContributionCyclePage{}, ErrMembershipNotFound
	}
	if err != nil {
		return ContributionCyclePage{}, fmt.Errorf("resolve own workgroup membership: %w", err)
	}
	return repository.contributionCycles(ctx, membershipID, kind, limit, asOf)
}

func (repository *PostgresRepository) ListContributionCycles(ctx context.Context, kind GroupKind, membershipID uuid.UUID, limit int, asOf time.Time) (ContributionCyclePage, error) {
	var exists bool
	if err := repository.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM workgroups.memberships
    WHERE id = $1 AND group_kind = $2
)`, membershipID, kind).Scan(&exists); err != nil {
		return ContributionCyclePage{}, fmt.Errorf("check workgroup membership for contribution history: %w", err)
	}
	if !exists {
		return ContributionCyclePage{}, ErrMembershipNotFound
	}
	return repository.contributionCycles(ctx, membershipID, kind, limit, asOf)
}

type contributionMembershipTransition struct {
	ToStatus   MembershipStatus
	OccurredAt time.Time
}

func (repository *PostgresRepository) contributionCycles(ctx context.Context, membershipID uuid.UUID, kind GroupKind, limit int, asOf time.Time) (ContributionCyclePage, error) {
	return repository.contributionCyclesWith(ctx, repository.pool, membershipID, kind, limit, asOf)
}

func (repository *PostgresRepository) contributionCyclesWith(ctx context.Context, querier workgroupQuerier, membershipID uuid.UUID, kind GroupKind, limit int, asOf time.Time) (ContributionCyclePage, error) {
	asOf = canonicalTime(asOf)
	rows, err := querier.Query(ctx, `
SELECT to_status, occurred_at
FROM workgroups.membership_transitions
WHERE membership_id = $1
ORDER BY occurred_at, state_version, id`, membershipID)
	if err != nil {
		return ContributionCyclePage{}, fmt.Errorf("list workgroup membership history for contribution cycles: %w", err)
	}
	transitions := make([]contributionMembershipTransition, 0)
	for rows.Next() {
		var transition contributionMembershipTransition
		if err := rows.Scan(&transition.ToStatus, &transition.OccurredAt); err != nil {
			rows.Close()
			return ContributionCyclePage{}, fmt.Errorf("scan workgroup membership history for contribution cycles: %w", err)
		}
		transition.OccurredAt = canonicalTime(transition.OccurredAt)
		transitions = append(transitions, transition)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ContributionCyclePage{}, fmt.Errorf("iterate workgroup membership history for contribution cycles: %w", err)
	}
	rows.Close()
	if len(transitions) == 0 {
		return ContributionCyclePage{}, errors.New("workgroup membership has no immutable transition history")
	}

	page := ContributionCyclePage{Limit: limit}
	periodStart := calendarMonthStart(asOf)
	for len(page.Items) < limit {
		periodEnd := periodStart.AddDate(0, 1, 0)
		if !periodEnd.After(transitions[0].OccurredAt) {
			break
		}
		observedAt := asOf
		if periodEnd.Before(observedAt) {
			observedAt = periodEnd
		}
		policy, err := repository.contributionPolicyWith(ctx, querier, kind, periodStart)
		if err != nil {
			return ContributionCyclePage{}, err
		}
		values, _, err := repository.contributionValuesWith(
			ctx, querier, policy.Metric, []uuid.UUID{membershipID}, periodStart, observedAt,
		)
		if err != nil {
			return ContributionCyclePage{}, err
		}
		evidenceState, evidenceThrough, err := repository.contributionEvidenceStateWith(
			ctx, querier, policy.Metric, periodStart, periodEnd, observedAt,
		)
		if err != nil {
			return ContributionCyclePage{}, err
		}
		// Membership coverage comes exclusively from the append-only transition
		// timeline. The mutable membership row is deliberately not used to
		// reconstruct earlier months.
		activeDuration := contributionActiveDurationTotal(transitions, periodStart, observedAt)
		activeSeconds := int64(activeDuration / time.Second)
		fullPeriodActive := observedAt.After(periodStart) && activeDuration == observedAt.Sub(periodStart)
		cycle := ContributionCycle{
			GroupKind: kind, Metric: policy.Metric, PolicyRevision: policy.Revision,
			PeriodStartsAt: periodStart, PeriodEndsAt: periodEnd, ObservedAt: observedAt,
			EvidenceThrough: evidenceThrough, EvidenceState: evidenceState,
			ActiveSeconds:    activeSeconds,
			FullPeriodActive: fullPeriodActive,
			CurrentValue:     values[membershipID], TargetValue: policy.TargetValue,
			EnforcementMode: policy.EnforcementMode,
		}
		cycle.AssessmentState, cycle.ExplanationCode = contributionAssessment(cycle)
		page.Items = append(page.Items, cycle)
		periodStart = periodStart.AddDate(0, -1, 0)
	}
	if err := attachContributionReminders(ctx, querier, membershipID, page.Items); err != nil {
		return ContributionCyclePage{}, err
	}
	return page, nil
}

func attachContributionReminders(ctx context.Context, querier workgroupQuerier, membershipID uuid.UUID, cycles []ContributionCycle) error {
	if len(cycles) == 0 {
		return nil
	}
	oldestPeriod := cycles[len(cycles)-1].PeriodStartsAt
	rows, err := querier.Query(ctx, contributionReminderSelect+`
WHERE reminder.membership_id = $1
  AND reminder.period_starts_at >= $2
ORDER BY reminder.period_starts_at DESC`, membershipID, oldestPeriod)
	if err != nil {
		return fmt.Errorf("list workgroup contribution reminders: %w", err)
	}
	defer rows.Close()
	byPeriod := make(map[time.Time]ContributionReminder, len(cycles))
	for rows.Next() {
		reminder, scanErr := scanContributionReminder(rows)
		if scanErr != nil {
			return fmt.Errorf("scan workgroup contribution reminder: %w", scanErr)
		}
		byPeriod[reminder.PeriodStartsAt] = reminder
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate workgroup contribution reminders: %w", err)
	}
	for index := range cycles {
		if reminder, ok := byPeriod[cycles[index].PeriodStartsAt]; ok {
			copy := reminder
			cycles[index].Reminder = &copy
		}
	}
	return nil
}

func (repository *PostgresRepository) IssueContributionReminder(ctx context.Context, command IssueContributionReminderCommand) (ContributionReminder, error) {
	if command.RequestID == uuid.Nil || command.MembershipID == uuid.Nil ||
		command.ActorID == uuid.Nil || command.AuthorizationDecisionID == uuid.Nil ||
		!validGroupKind(command.GroupKind) || command.OccurredAt.IsZero() ||
		!command.PeriodStartsAt.Equal(calendarMonthStart(command.PeriodStartsAt)) ||
		!validReason(command.Reason) {
		return ContributionReminder{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return ContributionReminder{}, fmt.Errorf("begin workgroup contribution reminder: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	existing, err := scanContributionReminder(tx.QueryRow(ctx, contributionReminderSelect+`
WHERE reminder.id = $1`, command.RequestID))
	if err == nil {
		if !sameContributionReminderIssue(existing, command) {
			return ContributionReminder{}, ErrIdempotencyConflict
		}
		existing.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return ContributionReminder{}, fmt.Errorf("commit workgroup contribution reminder replay: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ContributionReminder{}, fmt.Errorf("read workgroup contribution reminder replay: %w", err)
	}

	var membershipExists bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM workgroups.memberships
    WHERE id = $1 AND group_kind = $2
)`, command.MembershipID, command.GroupKind).Scan(&membershipExists); err != nil {
		return ContributionReminder{}, fmt.Errorf("check workgroup membership for contribution reminder: %w", err)
	}
	if !membershipExists {
		return ContributionReminder{}, ErrMembershipNotFound
	}

	page, err := repository.contributionCyclesWith(
		ctx, tx, command.MembershipID, command.GroupKind, MaximumCycleLimit, command.OccurredAt,
	)
	if err != nil {
		return ContributionReminder{}, err
	}
	var selected *ContributionCycle
	for index := range page.Items {
		if page.Items[index].PeriodStartsAt.Equal(command.PeriodStartsAt) {
			selected = &page.Items[index]
			break
		}
	}
	if selected == nil || selected.Reminder != nil || !contributionReminderAllowed(*selected) {
		if selected != nil && selected.Reminder != nil {
			return ContributionReminder{}, ErrContributionReminderExists
		}
		return ContributionReminder{}, ErrContributionReminderDenied
	}

	var recipientUserID uuid.UUID
	if err := tx.QueryRow(ctx, `
SELECT user_id FROM workgroups.memberships WHERE id = $1`, command.MembershipID).Scan(&recipientUserID); err != nil {
		return ContributionReminder{}, fmt.Errorf("resolve workgroup contribution reminder recipient: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO workgroups.contribution_reminders (
    id, membership_id, group_kind, recipient_user_id, metric,
    policy_revision, period_starts_at, period_ends_at, observed_at,
    evidence_through, evidence_state, current_value, target_value,
    assessment_state, explanation_code, full_period_active, reason,
    issued_by, authorization_decision_id, created_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9,
    $10, $11, $12, $13,
    $14, $15, $16, $17,
    $18, $19, $20
)`, command.RequestID, command.MembershipID, command.GroupKind,
		recipientUserID, selected.Metric, selected.PolicyRevision,
		selected.PeriodStartsAt, selected.PeriodEndsAt, selected.ObservedAt,
		selected.EvidenceThrough, selected.EvidenceState, selected.CurrentValue,
		selected.TargetValue, selected.AssessmentState, selected.ExplanationCode,
		selected.FullPeriodActive, command.Reason, command.ActorID,
		command.AuthorizationDecisionID, command.OccurredAt)
	if err != nil {
		switch constraintName(err) {
		case "contribution_reminders_membership_id_period_starts_at_key":
			return ContributionReminder{}, ErrContributionReminderExists
		case "contribution_reminders_pkey":
			return ContributionReminder{}, ErrIdempotencyConflict
		default:
			return ContributionReminder{}, fmt.Errorf("insert workgroup contribution reminder: %w", err)
		}
	}
	reminder, err := scanContributionReminder(tx.QueryRow(ctx, contributionReminderSelect+`
WHERE reminder.id = $1`, command.RequestID))
	if err != nil {
		return ContributionReminder{}, fmt.Errorf("read issued workgroup contribution reminder: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ContributionReminder{}, fmt.Errorf("commit workgroup contribution reminder: %w", err)
	}
	return reminder, nil
}

func contributionReminderAllowed(cycle ContributionCycle) bool {
	return cycle.FullPeriodActive && cycle.CurrentValue < cycle.TargetValue &&
		(cycle.EvidenceState == ContributionEvidenceCollecting ||
			cycle.EvidenceState == ContributionEvidenceComplete) &&
		(cycle.AssessmentState == ContributionAssessmentInProgress ||
			cycle.AssessmentState == ContributionAssessmentNotMet)
}

func (repository *PostgresRepository) contributionEvidenceState(ctx context.Context, metric ContributionMetric, periodStart, periodEnd, observedAt time.Time) (ContributionEvidenceState, *time.Time, error) {
	return repository.contributionEvidenceStateWith(ctx, repository.pool, metric, periodStart, periodEnd, observedAt)
}

func (repository *PostgresRepository) contributionEvidenceStateWith(ctx context.Context, querier workgroupQuerier, metric ContributionMetric, periodStart, periodEnd, observedAt time.Time) (ContributionEvidenceState, *time.Time, error) {
	// Publish and review evidence is written in the same transaction as the
	// business fact, so a closed query interval is complete by construction.
	// Retention is derived from hourly evidence windows and must prove a
	// continuous sequence before a closed month may be assessed.
	if metric != MetricSeedingActiveSeconds {
		through := observedAt
		if observedAt.Before(periodEnd) {
			return ContributionEvidenceCollecting, &through, nil
		}
		return ContributionEvidenceComplete, &through, nil
	}

	expectedThrough := periodEnd
	current := observedAt.Before(periodEnd)
	if current {
		expectedThrough = observedAt.Truncate(time.Hour)
	}
	expectedHours := int64(expectedThrough.Sub(periodStart) / time.Hour)
	if expectedHours <= 0 {
		return ContributionEvidenceCollecting, nil, nil
	}
	var totalWindows, completeWindows int64
	var firstComplete, lastComplete *time.Time
	if err := querier.QueryRow(ctx, `
SELECT count(*)::bigint,
       count(*) FILTER (WHERE status = 'complete')::bigint,
       min(window_start) FILTER (WHERE status = 'complete'),
       max(window_end) FILTER (WHERE status = 'complete')
FROM economy.seeding_reward_evidence_windows
WHERE window_start >= $1 AND window_end <= $2`,
		periodStart, expectedThrough,
	).Scan(&totalWindows, &completeWindows, &firstComplete, &lastComplete); err != nil {
		return "", nil, fmt.Errorf("read workgroup seeding evidence coverage: %w", err)
	}
	var evidenceThrough *time.Time
	if lastComplete != nil {
		value := canonicalTime(*lastComplete)
		evidenceThrough = &value
	}
	continuous := completeWindows == expectedHours &&
		firstComplete != nil && firstComplete.Equal(periodStart) &&
		lastComplete != nil && lastComplete.Equal(expectedThrough)
	if continuous {
		if current {
			return ContributionEvidenceCollecting, evidenceThrough, nil
		}
		return ContributionEvidenceComplete, evidenceThrough, nil
	}
	if totalWindows == 0 {
		return ContributionEvidenceUnavailable, evidenceThrough, nil
	}
	return ContributionEvidenceIncomplete, evidenceThrough, nil
}

func contributionActiveSeconds(transitions []contributionMembershipTransition, startsAt, endsAt time.Time) int64 {
	return int64(contributionActiveDurationTotal(transitions, startsAt, endsAt) / time.Second)
}

func contributionActiveDurationTotal(transitions []contributionMembershipTransition, startsAt, endsAt time.Time) time.Duration {
	if !endsAt.After(startsAt) {
		return 0
	}
	state := MembershipStatus("")
	cursor := startsAt
	var active time.Duration
	for _, transition := range transitions {
		switch {
		case !transition.OccurredAt.After(startsAt):
			state = transition.ToStatus
			continue
		case !transition.OccurredAt.Before(endsAt):
			return active + contributionActiveDuration(state, cursor, endsAt)
		}
		active += contributionActiveDuration(state, cursor, transition.OccurredAt)
		cursor = transition.OccurredAt
		state = transition.ToStatus
	}
	active += contributionActiveDuration(state, cursor, endsAt)
	return active
}

func contributionActiveDuration(state MembershipStatus, startsAt, endsAt time.Time) time.Duration {
	if state != MembershipActive || !endsAt.After(startsAt) {
		return 0
	}
	return endsAt.Sub(startsAt)
}

func contributionAssessment(cycle ContributionCycle) (ContributionAssessmentState, ContributionExplanationCode) {
	// Precedence is safety-sensitive: missing membership coverage or evidence
	// can never be flattened into a failure. Only a closed, fully covered,
	// evidence-complete period is eligible for a not_met result.
	switch {
	case cycle.ActiveSeconds == 0:
		return ContributionAssessmentNotAssessable, ContributionExplanationMembershipInactive
	case !cycle.FullPeriodActive:
		return ContributionAssessmentNotAssessable, ContributionExplanationPartialMembership
	case cycle.EvidenceState == ContributionEvidenceUnavailable:
		return ContributionAssessmentIndeterminate, ContributionExplanationEvidenceUnavailable
	case cycle.EvidenceState == ContributionEvidenceIncomplete:
		return ContributionAssessmentIndeterminate, ContributionExplanationEvidenceIncomplete
	case cycle.CurrentValue >= cycle.TargetValue:
		return ContributionAssessmentMet, ContributionExplanationTargetMet
	case cycle.ObservedAt.Before(cycle.PeriodEndsAt):
		return ContributionAssessmentInProgress, ContributionExplanationPeriodInProgress
	case cycle.CurrentValue == 0:
		return ContributionAssessmentNotMet, ContributionExplanationNoContribution
	default:
		return ContributionAssessmentNotMet, ContributionExplanationBelowTarget
	}
}

func (repository *PostgresRepository) contributionPolicy(ctx context.Context, kind GroupKind, periodStart time.Time) (ContributionPolicy, error) {
	return repository.contributionPolicyWith(ctx, repository.pool, kind, periodStart)
}

func (repository *PostgresRepository) contributionPolicyWith(ctx context.Context, querier workgroupQuerier, kind GroupKind, periodStart time.Time) (ContributionPolicy, error) {
	var policy ContributionPolicy
	if err := querier.QueryRow(ctx, `
SELECT group_kind, revision, metric, period_kind, target_value,
       enforcement_mode
FROM workgroups.contribution_policy_revisions
WHERE group_kind = $1 AND effective_from <= $2
ORDER BY effective_from DESC, revision DESC
	LIMIT 1`, kind, periodStart).Scan(
		&policy.GroupKind, &policy.Revision, &policy.Metric, &policy.PeriodKind,
		&policy.TargetValue, &policy.EnforcementMode,
	); errors.Is(err, pgx.ErrNoRows) {
		return ContributionPolicy{}, fmt.Errorf("workgroup contribution policy is missing for %s", kind)
	} else if err != nil {
		return ContributionPolicy{}, fmt.Errorf("read workgroup contribution policy: %w", err)
	}
	if policy.Revision < 1 || policy.TargetValue < 1 || policy.PeriodKind != "calendar_month" ||
		policy.EnforcementMode != "observe" || !validContributionMetric(kind, policy.Metric) {
		return ContributionPolicy{}, errors.New("workgroup contribution policy is invalid")
	}
	return policy, nil
}

func (repository *PostgresRepository) ListContributionPolicies(ctx context.Context, kind GroupKind, limit, offset int, asOf time.Time) (ContributionPolicyPage, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ContributionPolicyPage{}, fmt.Errorf("begin workgroup contribution policy read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	page := ContributionPolicyPage{
		Limit: limit, Offset: offset,
		MinimumEffectiveFrom: calendarMonthStart(asOf).AddDate(0, 1, 0),
	}
	if err := tx.QueryRow(ctx, `
SELECT count(*) FROM workgroups.contribution_policy_revisions
WHERE group_kind = $1`, kind).Scan(&page.Total); err != nil {
		return ContributionPolicyPage{}, fmt.Errorf("count workgroup contribution policies: %w", err)
	}
	rows, err := tx.Query(ctx, contributionPolicyRevisionSelect+`
WHERE policy.group_kind = $1
ORDER BY policy.revision DESC
LIMIT $2 OFFSET $3`, kind, limit, offset)
	if err != nil {
		return ContributionPolicyPage{}, fmt.Errorf("list workgroup contribution policies: %w", err)
	}
	for rows.Next() {
		policy, scanErr := scanContributionPolicy(rows, asOf)
		if scanErr != nil {
			rows.Close()
			return ContributionPolicyPage{}, scanErr
		}
		page.Items = append(page.Items, policy)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ContributionPolicyPage{}, fmt.Errorf("iterate workgroup contribution policies: %w", err)
	}
	rows.Close()
	current, err := scanContributionPolicy(tx.QueryRow(ctx, contributionPolicyRevisionSelect+`
WHERE policy.group_kind = $1 AND policy.effective_from <= $2
ORDER BY policy.effective_from DESC, policy.revision DESC
LIMIT 1`, kind, asOf), asOf)
	if err == nil {
		page.Current = &current
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ContributionPolicyPage{}, err
	}
	var latestFinite pgtype.Timestamptz
	if err := tx.QueryRow(ctx, `
SELECT max(effective_from) FILTER (
    WHERE effective_from <> '-infinity'::timestamptz
)
FROM workgroups.contribution_policy_revisions
WHERE group_kind = $1`, kind).Scan(&latestFinite); err != nil {
		return ContributionPolicyPage{}, fmt.Errorf("read latest workgroup contribution policy: %w", err)
	}
	if latestFinite.Valid && !latestFinite.Time.Before(page.MinimumEffectiveFrom) {
		page.MinimumEffectiveFrom = calendarMonthStart(latestFinite.Time).AddDate(0, 1, 0)
	}
	if err := tx.Commit(ctx); err != nil {
		return ContributionPolicyPage{}, fmt.Errorf("commit workgroup contribution policy read: %w", err)
	}
	return page, nil
}

func (repository *PostgresRepository) IssueContributionPolicy(ctx context.Context, command IssueContributionPolicyCommand) (ContributionPolicy, error) {
	if command.RequestID == uuid.Nil || command.ActorID == uuid.Nil || command.AuthorizationDecisionID == uuid.Nil {
		return ContributionPolicy{}, ErrInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ContributionPolicy{}, fmt.Errorf("begin workgroup contribution policy issue: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	existing, err := scanContributionPolicy(tx.QueryRow(ctx, contributionPolicyRevisionSelect+`
WHERE policy.request_id = $1`, command.RequestID), command.OccurredAt)
	if err == nil {
		if !sameContributionPolicyIssue(existing, command) {
			return ContributionPolicy{}, ErrIdempotencyConflict
		}
		existing.Replayed = true
		if err := tx.Commit(ctx); err != nil {
			return ContributionPolicy{}, fmt.Errorf("commit workgroup contribution policy replay: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ContributionPolicy{}, fmt.Errorf("read workgroup contribution policy replay: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(
        'peergo-workgroup-contribution-policy:' || $1, 0
    ))`, command.GroupKind); err != nil {
		return ContributionPolicy{}, fmt.Errorf("lock workgroup contribution policy timeline: %w", err)
	}
	latest, err := scanContributionPolicy(tx.QueryRow(ctx, contributionPolicyRevisionSelect+`
WHERE policy.group_kind = $1
ORDER BY policy.revision DESC
LIMIT 1`, command.GroupKind), command.OccurredAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ContributionPolicy{}, ErrGroupNotFound
		}
		return ContributionPolicy{}, fmt.Errorf("read latest workgroup contribution policy: %w", err)
	}
	if latest.EffectiveFrom != nil && !command.EffectiveFrom.After(*latest.EffectiveFrom) {
		return ContributionPolicy{}, ErrContributionPolicyConflict
	}
	if latest.TargetValue == command.TargetValue {
		return ContributionPolicy{}, ErrContributionPolicyNoChange
	}
	metric, ok := contributionMetricFor(command.GroupKind)
	if !ok {
		return ContributionPolicy{}, ErrInput
	}
	revision := latest.Revision + 1
	_, err = tx.Exec(ctx, `
INSERT INTO workgroups.contribution_policy_revisions (
    group_kind, revision, metric, period_kind, target_value,
    enforcement_mode, effective_from, source_kind, source_reference,
    authorization_decision_id, created_at, request_id, issued_by, reason
) VALUES ($1, $2, $3, 'calendar_month', $4, 'observe', $5, 'staff', $6,
          $7, $8, $9, $10, $11)`,
		command.GroupKind, revision, metric, command.TargetValue,
		command.EffectiveFrom, "staff:"+command.RequestID.String(),
		command.AuthorizationDecisionID, command.OccurredAt, command.RequestID,
		command.ActorID, command.Reason)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && (postgresError.Code == "23505" || postgresError.Code == "40001" || postgresError.Code == "40P01" || postgresError.Code == "P0001") {
			return ContributionPolicy{}, fmt.Errorf("%w: insert workgroup contribution policy: %v", ErrContributionPolicyConflict, err)
		}
		return ContributionPolicy{}, fmt.Errorf("insert workgroup contribution policy: %w", err)
	}
	effectiveFrom := command.EffectiveFrom
	issuedBy := command.ActorID
	requestID := command.RequestID
	policy := ContributionPolicy{
		GroupKind: command.GroupKind, Revision: revision, Metric: metric,
		PeriodKind: "calendar_month", TargetValue: command.TargetValue,
		EnforcementMode: "observe", EffectiveFrom: &effectiveFrom,
		Reason: command.Reason, IssuedBy: &issuedBy, RequestID: &requestID,
		CreatedAt: command.OccurredAt, TimelineState: ContributionPolicyScheduled,
	}
	if err := tx.Commit(ctx); err != nil {
		return ContributionPolicy{}, fmt.Errorf("commit workgroup contribution policy: %w", err)
	}
	return policy, nil
}

func (repository *PostgresRepository) contributionValues(ctx context.Context, metric ContributionMetric, membershipIDs []uuid.UUID, startsAt, endsAt time.Time) (map[uuid.UUID]int64, *time.Time, error) {
	return repository.contributionValuesWith(ctx, repository.pool, metric, membershipIDs, startsAt, endsAt)
}

func (repository *PostgresRepository) contributionValuesWith(ctx context.Context, querier workgroupQuerier, metric ContributionMetric, membershipIDs []uuid.UUID, startsAt, endsAt time.Time) (map[uuid.UUID]int64, *time.Time, error) {
	values := make(map[uuid.UUID]int64, len(membershipIDs))
	var rows pgx.Rows
	var err error
	switch metric {
	case MetricTrustedTorrentsPublished:
		rows, err = querier.Query(ctx, `
SELECT transition.membership_id, count(*)::bigint
FROM review.torrent_decisions AS decision
JOIN workgroups.membership_transitions AS transition
  ON transition.id = decision.membership_transition_id
WHERE decision.resolution_source = 'trusted_workgroup'
  AND transition.membership_id = ANY($1::uuid[])
  AND decision.occurred_at >= $2 AND decision.occurred_at < $3
GROUP BY transition.membership_id`, membershipIDs, startsAt, endsAt)
	case MetricTorrentReviewVotes:
		rows, err = querier.Query(ctx, `
SELECT transition.membership_id, count(*)::bigint
FROM review.torrent_review_votes AS vote
JOIN workgroups.membership_transitions AS transition
  ON transition.id = vote.membership_transition_id
WHERE transition.membership_id = ANY($1::uuid[])
  AND vote.occurred_at >= $2 AND vote.occurred_at < $3
GROUP BY transition.membership_id`, membershipIDs, startsAt, endsAt)
	case MetricSeedingActiveSeconds:
		rows, err = querier.Query(ctx, `
SELECT active_membership.membership_id,
       COALESCE(sum(item.active_seconds), 0)::bigint
FROM economy.seeding_reward_evidence_items AS item
JOIN economy.seeding_reward_evidence_windows AS evidence_window
  ON evidence_window.window_start = item.window_start
 AND evidence_window.status = 'complete'
JOIN LATERAL (
    SELECT transition.membership_id, transition.to_status
    FROM workgroups.membership_transitions AS transition
    WHERE transition.user_id = item.user_id
      AND transition.group_kind = 'retention'
      AND transition.occurred_at <= evidence_window.window_start
    ORDER BY transition.occurred_at DESC, transition.state_version DESC, transition.id DESC
    LIMIT 1
) AS active_membership ON active_membership.to_status = 'active'
WHERE active_membership.membership_id = ANY($1::uuid[])
  AND evidence_window.window_start >= $2
  AND evidence_window.window_end <= $3
GROUP BY active_membership.membership_id`, membershipIDs, startsAt, endsAt)
	default:
		return nil, nil, errors.New("unknown workgroup contribution metric")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("query workgroup contribution values: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var membershipID uuid.UUID
		var value int64
		if err := rows.Scan(&membershipID, &value); err != nil || membershipID == uuid.Nil || value < 0 {
			if err != nil {
				return nil, nil, fmt.Errorf("scan workgroup contribution value: %w", err)
			}
			return nil, nil, errors.New("workgroup contribution value is invalid")
		}
		values[membershipID] = value
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("finish workgroup contribution values: %w", err)
	}
	if metric != MetricSeedingActiveSeconds {
		return values, nil, nil
	}
	var evidenceThrough *time.Time
	var latest *time.Time
	if err := querier.QueryRow(ctx, `
SELECT max(window_end)
FROM economy.seeding_reward_evidence_windows
WHERE status = 'complete' AND window_end <= $1`, endsAt).Scan(&latest); err != nil {
		return nil, nil, fmt.Errorf("read seeding evidence watermark: %w", err)
	}
	if latest != nil {
		canonical := latest.UTC().Truncate(time.Microsecond)
		evidenceThrough = &canonical
	}
	return values, evidenceThrough, nil
}

func validContributionMetric(kind GroupKind, metric ContributionMetric) bool {
	return (kind == GroupReseed && metric == MetricTrustedTorrentsPublished) ||
		(kind == GroupReview && metric == MetricTorrentReviewVotes) ||
		(kind == GroupRetention && metric == MetricSeedingActiveSeconds)
}

func (repository *PostgresRepository) GrantMembership(ctx context.Context, command GrantMembershipCommand) (Membership, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Membership{}, fmt.Errorf("begin workgroup membership grant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM identity.users WHERE numeric_id = $1`, command.UserNumericID).Scan(&userID); errors.Is(err, pgx.ErrNoRows) {
		return Membership{}, ErrUserNotFound
	} else if err != nil {
		return Membership{}, fmt.Errorf("resolve workgroup member: %w", err)
	}
	membership, err := scanMembership(tx.QueryRow(ctx, membershipSelect+` WHERE membership.group_kind = $1 AND membership.user_id = $2 FOR UPDATE OF membership`, command.GroupKind, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		membershipID := uuid.New()
		if _, err := tx.Exec(ctx, `
INSERT INTO workgroups.memberships (
    id, group_kind, user_id, status, source, version,
    started_at, updated_at
) VALUES ($1, $2, $3, 'active', 'staff', 1, $4, $4)`,
			membershipID, command.GroupKind, userID, command.OccurredAt); err != nil {
			return Membership{}, fmt.Errorf("insert workgroup membership: %w", err)
		}
		if err := appendMembershipTransition(ctx, tx, command.TransitionID, membershipID, command.GroupKind, userID, "joined", nil, MembershipActive, command.ActorID, "staff", nil, command.Reason, command.AuthorizationDecisionID, 1, command.OccurredAt); err != nil {
			return Membership{}, err
		}
		membership, err = scanMembership(tx.QueryRow(ctx, membershipSelect+` WHERE membership.id = $1`, membershipID))
	} else if err != nil {
		return Membership{}, fmt.Errorf("lock workgroup membership: %w", err)
	} else {
		if membership.Status == MembershipActive {
			return Membership{}, ErrMembershipAlreadyActive
		}
		from := membership.Status
		newVersion := membership.Version + 1
		if _, err := tx.Exec(ctx, `
UPDATE workgroups.memberships
SET status = 'active', version = version + 1, ended_at = NULL, updated_at = $1
WHERE id = $2 AND version = $3`, command.OccurredAt, membership.ID, membership.Version); err != nil {
			return Membership{}, fmt.Errorf("reactivate workgroup membership: %w", err)
		}
		if err := appendMembershipTransition(ctx, tx, command.TransitionID, membership.ID, command.GroupKind, userID, "reactivated", &from, MembershipActive, command.ActorID, "staff", nil, command.Reason, command.AuthorizationDecisionID, newVersion, command.OccurredAt); err != nil {
			return Membership{}, err
		}
		membership, err = scanMembership(tx.QueryRow(ctx, membershipSelect+` WHERE membership.id = $1`, membership.ID))
	}
	if err != nil {
		return Membership{}, fmt.Errorf("read granted workgroup membership: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Membership{}, fmt.Errorf("commit workgroup membership grant: %w", err)
	}
	return membership, nil
}

func (repository *PostgresRepository) ChangeMembership(ctx context.Context, command ChangeMembershipCommand) (Membership, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Membership{}, fmt.Errorf("begin workgroup membership transition: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	membership, err := scanMembership(tx.QueryRow(ctx, membershipSelect+` WHERE membership.id = $1 AND membership.group_kind = $2 FOR UPDATE OF membership`, command.MembershipID, command.GroupKind))
	if errors.Is(err, pgx.ErrNoRows) {
		return Membership{}, ErrMembershipNotFound
	}
	if err != nil {
		return Membership{}, fmt.Errorf("lock workgroup membership: %w", err)
	}
	if membership.Version != command.ExpectedVersion {
		return Membership{}, ErrMembershipConflict
	}
	from := membership.Status
	var to MembershipStatus
	switch command.Transition {
	case TransitionSuspend:
		if from != MembershipActive {
			return Membership{}, ErrMembershipTransition
		}
		to = MembershipSuspended
	case TransitionReactivate:
		if from != MembershipSuspended && from != MembershipEnded {
			return Membership{}, ErrMembershipTransition
		}
		to = MembershipActive
	case TransitionEnd:
		if from != MembershipActive && from != MembershipSuspended {
			return Membership{}, ErrMembershipTransition
		}
		to = MembershipEnded
	default:
		return Membership{}, ErrMembershipTransition
	}
	var endedAt any
	if to == MembershipEnded {
		endedAt = command.OccurredAt
	}
	if _, err := tx.Exec(ctx, `
UPDATE workgroups.memberships
SET status = $1, version = version + 1, ended_at = $2, updated_at = $3
WHERE id = $4 AND version = $5`, to, endedAt, command.OccurredAt, membership.ID, membership.Version); err != nil {
		return Membership{}, fmt.Errorf("update workgroup membership: %w", err)
	}
	if err := appendMembershipTransition(ctx, tx, command.TransitionID, membership.ID, membership.GroupKind, membership.UserID, string(command.Transition), &from, to, command.ActorID, "staff", nil, command.Reason, command.AuthorizationDecisionID, membership.Version+1, command.OccurredAt); err != nil {
		return Membership{}, err
	}
	result, err := scanMembership(tx.QueryRow(ctx, membershipSelect+` WHERE membership.id = $1`, membership.ID))
	if err != nil {
		return Membership{}, fmt.Errorf("read changed workgroup membership: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Membership{}, fmt.Errorf("commit workgroup membership transition: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) HasEntitlementAt(ctx context.Context, userID uuid.UUID, entitlement Entitlement, at time.Time) (bool, error) {
	var kind GroupKind
	switch entitlement {
	case EntitlementTrustedTorrentPublish:
		kind = GroupReseed
	case EntitlementTorrentReviewVote:
		kind = GroupReview
	case EntitlementDownloadChargeExempt:
		kind = GroupRetention
	default:
		return false, ErrInput
	}
	var active bool
	if err := repository.pool.QueryRow(ctx, `
SELECT COALESCE((
    SELECT transition.to_status = 'active'
    FROM workgroups.membership_transitions AS transition
    WHERE transition.user_id = $1 AND transition.group_kind = $2
      AND transition.occurred_at <= $3
    ORDER BY transition.occurred_at DESC, transition.state_version DESC, transition.id DESC
    LIMIT 1
), false)`, userID, kind, at).Scan(&active); err != nil {
		return false, fmt.Errorf("resolve workgroup entitlement timeline: %w", err)
	}
	return active, nil
}

func (repository *PostgresRepository) definitions(ctx context.Context) ([]Definition, error) {
	rows, err := repository.pool.Query(ctx, `
SELECT kind, display_name, description, join_mode, enabled, sort_order, version
FROM workgroups.definitions ORDER BY sort_order`)
	if err != nil {
		return nil, fmt.Errorf("list workgroup definitions: %w", err)
	}
	defer rows.Close()
	var definitions []Definition
	for rows.Next() {
		var definition Definition
		if err := rows.Scan(&definition.Kind, &definition.DisplayName, &definition.Description, &definition.JoinMode, &definition.Enabled, &definition.SortOrder, &definition.Version); err != nil {
			return nil, fmt.Errorf("scan workgroup definition: %w", err)
		}
		entitlement, ok := EntitlementFor(definition.Kind)
		if !ok {
			return nil, errors.New("workgroup definition contains unknown kind")
		}
		definition.Entitlement = entitlement
		definitions = append(definitions, definition)
	}
	return definitions, rows.Err()
}

func (repository *PostgresRepository) membershipsByUser(ctx context.Context, userID uuid.UUID) (map[GroupKind]Membership, error) {
	rows, err := repository.pool.Query(ctx, membershipSelect+` WHERE membership.user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("list member workgroups: %w", err)
	}
	defer rows.Close()
	result := make(map[GroupKind]Membership)
	for rows.Next() {
		membership, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		result[membership.GroupKind] = membership
	}
	return result, rows.Err()
}

func (repository *PostgresRepository) latestApplicationsByUser(ctx context.Context, userID uuid.UUID) (map[GroupKind]Application, error) {
	rows, err := repository.pool.Query(ctx, applicationSelect+`
WHERE application.applicant_id = $1
ORDER BY application.group_kind, application.submitted_at DESC, application.id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list member workgroup applications: %w", err)
	}
	defer rows.Close()
	result := make(map[GroupKind]Application)
	for rows.Next() {
		application, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		if _, exists := result[application.GroupKind]; !exists {
			result[application.GroupKind] = application
		}
	}
	return result, rows.Err()
}

func (repository *PostgresRepository) reviewerEligibility(ctx context.Context, userID uuid.UUID, asOf time.Time) (ReviewerEligibility, error) {
	return reviewerEligibilityWithQuerier(ctx, repository.pool, userID, asOf)
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func reviewerEligibilityWithQuerier(ctx context.Context, querier rowQuerier, userID uuid.UUID, asOf time.Time) (ReviewerEligibility, error) {
	var eligibility ReviewerEligibility
	var createdAt time.Time
	if err := querier.QueryRow(ctx, `
SELECT policy.revision, policy.minimum_level, policy.minimum_credited_uploaded,
       policy.minimum_account_age_days, policy.require_verified_email,
       policy.require_unrestricted_download,
       COALESCE(progress.level, 1), COALESCE(traffic.credited_uploaded, 0),
       users.created_at, users.email_verified_at IS NOT NULL,
       COALESCE(access.download_restricted, false), users.status = 'active'
FROM identity.users AS users
CROSS JOIN LATERAL (
    SELECT * FROM workgroups.review_application_policy_revisions
    WHERE effective_from <= $2 ORDER BY effective_from DESC LIMIT 1
) AS policy
LEFT JOIN progression.user_progress AS progress ON progress.user_id = users.id
LEFT JOIN traffic.user_totals AS traffic ON traffic.user_id = users.id
LEFT JOIN identity.user_access_states AS access ON access.user_id = users.id
WHERE users.id = $1`, userID, asOf).Scan(
		&eligibility.PolicyRevision, &eligibility.MinimumLevel,
		&eligibility.MinimumCreditedUploaded, &eligibility.MinimumAccountAgeDays,
		&eligibility.RequireVerifiedEmail, &eligibility.RequireUnrestrictedDownload,
		&eligibility.Level, &eligibility.CreditedUploaded, &createdAt,
		&eligibility.EmailVerified, &eligibility.DownloadRestricted,
		&eligibility.AccountActive,
	); errors.Is(err, pgx.ErrNoRows) {
		return ReviewerEligibility{}, ErrUserNotFound
	} else if err != nil {
		return ReviewerEligibility{}, fmt.Errorf("read reviewer eligibility: %w", err)
	}
	if asOf.After(createdAt) {
		eligibility.AccountAgeDays = int32(asOf.Sub(createdAt) / (24 * time.Hour))
	}
	eligibility.Eligible = eligibility.AccountActive &&
		eligibility.Level >= eligibility.MinimumLevel &&
		eligibility.CreditedUploaded >= eligibility.MinimumCreditedUploaded &&
		eligibility.AccountAgeDays >= eligibility.MinimumAccountAgeDays &&
		(!eligibility.RequireVerifiedEmail || eligibility.EmailVerified) &&
		(!eligibility.RequireUnrestrictedDownload || !eligibility.DownloadRestricted)
	return eligibility, nil
}

func ensureApplicationMembership(ctx context.Context, tx pgx.Tx, application Application, command DecideApplicationCommand) (Membership, error) {
	membership, err := scanMembership(tx.QueryRow(ctx, membershipSelect+` WHERE membership.group_kind = $1 AND membership.user_id = $2 FOR UPDATE OF membership`, application.GroupKind, application.ApplicantID))
	if errors.Is(err, pgx.ErrNoRows) {
		membershipID := uuid.New()
		if _, err := tx.Exec(ctx, `
INSERT INTO workgroups.memberships (
    id, group_kind, user_id, status, source, source_application_id,
    version, started_at, updated_at
) VALUES ($1, $2, $3, 'active', 'application', $4, 1, $5, $5)`,
			membershipID, application.GroupKind, application.ApplicantID,
			application.ID, command.OccurredAt); err != nil {
			return Membership{}, fmt.Errorf("insert approved workgroup membership: %w", err)
		}
		if err := appendMembershipTransition(ctx, tx, uuid.New(), membershipID, application.GroupKind, application.ApplicantID, "joined", nil, MembershipActive, command.ActorID, "application", &application.ID, command.Reason, command.AuthorizationDecisionID, 1, command.OccurredAt); err != nil {
			return Membership{}, err
		}
		return scanMembership(tx.QueryRow(ctx, membershipSelect+` WHERE membership.id = $1`, membershipID))
	}
	if err != nil {
		return Membership{}, fmt.Errorf("lock approved workgroup membership: %w", err)
	}
	if membership.Status == MembershipActive {
		return Membership{}, ErrMembershipAlreadyActive
	}
	from := membership.Status
	if _, err := tx.Exec(ctx, `
UPDATE workgroups.memberships
SET status = 'active', source = 'application', source_application_id = $1,
    version = version + 1, ended_at = NULL, updated_at = $2
WHERE id = $3 AND version = $4`, application.ID, command.OccurredAt, membership.ID, membership.Version); err != nil {
		return Membership{}, fmt.Errorf("reactivate approved workgroup membership: %w", err)
	}
	if err := appendMembershipTransition(ctx, tx, uuid.New(), membership.ID, application.GroupKind, application.ApplicantID, "reactivated", &from, MembershipActive, command.ActorID, "application", &application.ID, command.Reason, command.AuthorizationDecisionID, membership.Version+1, command.OccurredAt); err != nil {
		return Membership{}, err
	}
	return scanMembership(tx.QueryRow(ctx, membershipSelect+` WHERE membership.id = $1`, membership.ID))
}

func appendMembershipTransition(ctx context.Context, tx pgx.Tx, id, membershipID uuid.UUID, kind GroupKind, userID uuid.UUID, transition string, from *MembershipStatus, to MembershipStatus, actorID uuid.UUID, source string, applicationID *uuid.UUID, reason string, decisionID uuid.UUID, version int64, occurredAt time.Time) error {
	var fromValue any
	if from != nil {
		fromValue = *from
	}
	_, err := tx.Exec(ctx, `
INSERT INTO workgroups.membership_transitions (
    id, membership_id, group_kind, user_id, transition,
    from_status, to_status, actor_id, source, source_application_id,
    reason, authorization_decision_id, state_version, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		id, membershipID, kind, userID, transition, fromValue, to, actorID,
		source, applicationID, reason, decisionID, version, occurredAt)
	if err != nil {
		if constraintName(err) == "membership_transitions_pkey" {
			return ErrIdempotencyConflict
		}
		return fmt.Errorf("append workgroup membership transition: %w", err)
	}
	if kind == GroupRetention {
		benefitCommand := workgroupbenefitv1.Command{
			SchemaVersion: workgroupbenefitv1.SchemaVersion,
			TransitionID:  id.String(), UserID: userID.String(),
			GroupKind:   workgroupbenefitv1.GroupRetention,
			Entitlement: workgroupbenefitv1.EntitlementDownloadChargeExempt,
			Active:      to == MembershipActive, StateVersion: version,
			EffectiveAt: occurredAt.UTC().Round(0),
		}
		encoded, encodeErr := workgroupbenefitv1.Encode(benefitCommand)
		if encodeErr != nil {
			return fmt.Errorf("encode retention workgroup benefit: %w", encodeErr)
		}
		digest, digestErr := workgroupbenefitv1.SHA256(encoded)
		if digestErr != nil {
			return fmt.Errorf("digest retention workgroup benefit: %w", digestErr)
		}
		if _, insertErr := tx.Exec(ctx, `
INSERT INTO workgroups.settlement_benefit_outbox (
    transition_id, user_id, state_version, effective_at,
    command_json, command_sha256, available_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $4, $4)`,
			id, userID, version, benefitCommand.EffectiveAt, string(encoded), digest[:]); insertErr != nil {
			return fmt.Errorf("enqueue retention workgroup benefit: %w", insertErr)
		}
	}
	return nil
}

type scanner interface {
	Scan(...any) error
}

type workgroupQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func scanApplication(row scanner) (Application, error) {
	var application Application
	var policyRevision *int64
	var snapshot []byte
	if err := row.Scan(
		&application.ID, &application.GroupKind, &application.ApplicantID,
		&application.ApplicantNumericID, &application.ApplicantUsername,
		&application.ApplicantDisplayName, &application.Statement,
		&application.Status, &policyRevision, &snapshot, &application.Version,
		&application.SubmittedAt, &application.DecidedAt,
	); err != nil {
		return Application{}, err
	}
	application.PolicyRevision = policyRevision
	if err := json.Unmarshal(snapshot, &application.Eligibility); err != nil {
		return Application{}, fmt.Errorf("decode reviewer eligibility snapshot: %w", err)
	}
	application.SubmittedAt = application.SubmittedAt.UTC()
	if application.DecidedAt != nil {
		decidedAt := application.DecidedAt.UTC()
		application.DecidedAt = &decidedAt
	}
	return application, nil
}

func scanMembership(row scanner) (Membership, error) {
	var membership Membership
	var legacyStatus, legacyActivityStatus pgtype.Text
	var legacyTotalReviews, legacyAccurateCount pgtype.Int8
	var legacyLastActivityAt pgtype.Timestamptz
	if err := row.Scan(
		&membership.ID, &membership.GroupKind, &membership.UserID,
		&membership.UserNumericID, &membership.Username, &membership.DisplayName,
		&membership.Status, &membership.Source, &membership.Version,
		&membership.StartedAt, &membership.EndedAt, &membership.UpdatedAt,
		&legacyStatus, &legacyActivityStatus, &legacyTotalReviews,
		&legacyAccurateCount, &legacyLastActivityAt,
	); err != nil {
		return Membership{}, err
	}
	membership.StartedAt = membership.StartedAt.UTC()
	membership.UpdatedAt = membership.UpdatedAt.UTC()
	if membership.EndedAt != nil {
		endedAt := membership.EndedAt.UTC()
		membership.EndedAt = &endedAt
	}
	if legacyStatus.Valid {
		if !legacyActivityStatus.Valid || !legacyTotalReviews.Valid || !legacyAccurateCount.Valid ||
			legacyTotalReviews.Int64 < 0 || legacyAccurateCount.Int64 < 0 || legacyAccurateCount.Int64 > legacyTotalReviews.Int64 {
			return Membership{}, errors.New("legacy reviewer membership evidence is invalid")
		}
		membership.LegacyReviewer = &LegacyReviewerEvidence{
			Status: legacyStatus.String, ActivityStatus: legacyActivityStatus.String,
			TotalReviews: legacyTotalReviews.Int64, AccurateCount: legacyAccurateCount.Int64,
		}
		if legacyLastActivityAt.Valid {
			value := legacyLastActivityAt.Time.UTC()
			membership.LegacyReviewer.LastActivityAt = &value
		}
	}
	return membership, nil
}

func scanContributionPolicy(row scanner, asOf time.Time) (ContributionPolicy, error) {
	var policy ContributionPolicy
	var effectiveFrom pgtype.Timestamptz
	var sourceKind string
	var issuedBy pgtype.UUID
	var requestID pgtype.UUID
	if err := row.Scan(
		&policy.GroupKind, &policy.Revision, &policy.Metric, &policy.PeriodKind,
		&policy.TargetValue, &policy.EnforcementMode, &effectiveFrom,
		&sourceKind, &policy.Reason, &issuedBy, &requestID, &policy.CreatedAt,
	); err != nil {
		return ContributionPolicy{}, err
	}
	switch {
	case effectiveFrom.InfinityModifier == pgtype.NegativeInfinity:
		policy.Opening = true
	case effectiveFrom.Valid && effectiveFrom.InfinityModifier == pgtype.Finite:
		value := effectiveFrom.Time.UTC().Truncate(time.Microsecond)
		policy.EffectiveFrom = &value
	default:
		return ContributionPolicy{}, errors.New("workgroup contribution policy effective time is invalid")
	}
	if (sourceKind == "cutover_opening") != policy.Opening {
		return ContributionPolicy{}, errors.New("workgroup contribution policy source is invalid")
	}
	if issuedBy.Valid {
		value := uuid.UUID(issuedBy.Bytes)
		policy.IssuedBy = &value
	}
	if requestID.Valid {
		value := uuid.UUID(requestID.Bytes)
		policy.RequestID = &value
	}
	policy.CreatedAt = policy.CreatedAt.UTC().Truncate(time.Microsecond)
	policy.TimelineState = ContributionPolicyActive
	if policy.EffectiveFrom != nil && policy.EffectiveFrom.After(asOf) {
		policy.TimelineState = ContributionPolicyScheduled
	}
	return policy, nil
}

func scanContributionReminder(row scanner) (ContributionReminder, error) {
	var reminder ContributionReminder
	var evidenceThrough pgtype.Timestamptz
	var notificationReadAt pgtype.Timestamptz
	if err := row.Scan(
		&reminder.ID, &reminder.MembershipID, &reminder.GroupKind,
		&reminder.RecipientUserID, &reminder.Metric, &reminder.PolicyRevision,
		&reminder.PeriodStartsAt, &reminder.PeriodEndsAt,
		&reminder.ObservedAt, &evidenceThrough, &reminder.EvidenceState,
		&reminder.CurrentValue, &reminder.TargetValue,
		&reminder.AssessmentState, &reminder.ExplanationCode,
		&reminder.FullPeriodActive, &reminder.Reason, &reminder.IssuedBy,
		&reminder.CreatedAt, &reminder.NotificationID, &notificationReadAt,
	); err != nil {
		return ContributionReminder{}, err
	}
	if reminder.ID == uuid.Nil || reminder.MembershipID == uuid.Nil ||
		reminder.RecipientUserID == uuid.Nil || reminder.IssuedBy == uuid.Nil ||
		reminder.NotificationID == uuid.Nil || reminder.PolicyRevision < 1 ||
		reminder.CurrentValue < 0 || reminder.TargetValue < 1 ||
		!reminder.FullPeriodActive || !validContributionMetric(reminder.GroupKind, reminder.Metric) ||
		!contributionReminderAllowed(ContributionCycle{
			EvidenceState: reminder.EvidenceState, CurrentValue: reminder.CurrentValue,
			TargetValue: reminder.TargetValue, AssessmentState: reminder.AssessmentState,
			FullPeriodActive: reminder.FullPeriodActive,
		}) {
		return ContributionReminder{}, errors.New("workgroup contribution reminder is invalid")
	}
	reminder.PeriodStartsAt = canonicalTime(reminder.PeriodStartsAt)
	reminder.PeriodEndsAt = canonicalTime(reminder.PeriodEndsAt)
	reminder.ObservedAt = canonicalTime(reminder.ObservedAt)
	reminder.CreatedAt = canonicalTime(reminder.CreatedAt)
	if evidenceThrough.Valid {
		value := canonicalTime(evidenceThrough.Time)
		reminder.EvidenceThrough = &value
	}
	if notificationReadAt.Valid {
		value := canonicalTime(notificationReadAt.Time)
		reminder.NotificationReadAt = &value
	}
	return reminder, nil
}

func contributionMetricFor(kind GroupKind) (ContributionMetric, bool) {
	switch kind {
	case GroupReseed:
		return MetricTrustedTorrentsPublished, true
	case GroupReview:
		return MetricTorrentReviewVotes, true
	case GroupRetention:
		return MetricSeedingActiveSeconds, true
	default:
		return "", false
	}
}

func sameContributionPolicyIssue(existing ContributionPolicy, command IssueContributionPolicyCommand) bool {
	return existing.RequestID != nil && *existing.RequestID == command.RequestID &&
		existing.IssuedBy != nil && *existing.IssuedBy == command.ActorID &&
		existing.GroupKind == command.GroupKind && existing.TargetValue == command.TargetValue &&
		existing.EffectiveFrom != nil && existing.EffectiveFrom.Equal(command.EffectiveFrom) &&
		existing.Reason == command.Reason
}

func sameContributionReminderIssue(existing ContributionReminder, command IssueContributionReminderCommand) bool {
	return existing.ID == command.RequestID && existing.MembershipID == command.MembershipID &&
		existing.GroupKind == command.GroupKind && existing.PeriodStartsAt.Equal(command.PeriodStartsAt) &&
		existing.IssuedBy == command.ActorID && existing.Reason == command.Reason
}

func constraintName(err error) string {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		return postgresError.ConstraintName
	}
	return ""
}
