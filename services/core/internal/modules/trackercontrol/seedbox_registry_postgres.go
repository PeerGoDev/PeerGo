package trackercontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
)

const seedboxReportSelect = `
SELECT report.id, report.user_id, users.numeric_id, users.username,
       host(report.network), report.provider, report.bandwidth_mbps,
       report.statement, report.status, report.version, report.submitted_at,
       report.decided_at, COALESCE(report.decision_reason, ''), report.policy_sequence
FROM tracker_control.seedbox_reports AS report
JOIN identity.users AS users ON users.id = report.user_id`

func (repository *PostgresRuntimePolicyRepository) ListMySeedboxReports(ctx context.Context, userID uuid.UUID, limit, offset int) (SeedboxReportPage, error) {
	return repository.listSeedboxReports(ctx, ` WHERE report.user_id = $1 ORDER BY report.submitted_at DESC, report.id DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
}

func (repository *PostgresRuntimePolicyRepository) ListSeedboxReports(ctx context.Context, status SeedboxReportStatus, limit, offset int) (SeedboxReportPage, error) {
	if status == "" {
		return repository.listSeedboxReports(ctx, ` ORDER BY (report.status = 'pending') DESC, report.submitted_at DESC, report.id DESC LIMIT $1 OFFSET $2`, limit, offset)
	}
	return repository.listSeedboxReports(ctx, ` WHERE report.status = $1 ORDER BY report.submitted_at DESC, report.id DESC LIMIT $2 OFFSET $3`, status, limit, offset)
}

func (repository *PostgresRuntimePolicyRepository) listSeedboxReports(ctx context.Context, suffix string, arguments ...any) (SeedboxReportPage, error) {
	var total int64
	countQuery := `SELECT count(*) FROM tracker_control.seedbox_reports AS report`
	countArgs := []any{}
	if strings.HasPrefix(suffix, ` WHERE report.user_id`) {
		countQuery += ` WHERE report.user_id = $1`
		countArgs = append(countArgs, arguments[0])
	} else if strings.HasPrefix(suffix, ` WHERE report.status`) {
		countQuery += ` WHERE report.status = $1`
		countArgs = append(countArgs, arguments[0])
	}
	if err := repository.pool.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return SeedboxReportPage{}, fmt.Errorf("count Seedbox reports: %w", err)
	}
	rows, err := repository.pool.Query(ctx, seedboxReportSelect+suffix, arguments...)
	if err != nil {
		return SeedboxReportPage{}, fmt.Errorf("list Seedbox reports: %w", err)
	}
	defer rows.Close()
	items := make([]SeedboxReport, 0)
	for rows.Next() {
		item, scanErr := scanSeedboxReport(rows)
		if scanErr != nil {
			return SeedboxReportPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return SeedboxReportPage{}, fmt.Errorf("iterate Seedbox reports: %w", err)
	}
	limit, offset := arguments[len(arguments)-2].(int), arguments[len(arguments)-1].(int)
	return SeedboxReportPage{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

func (repository *PostgresRuntimePolicyRepository) SubmitSeedboxReport(ctx context.Context, command submitSeedboxReportCommand) (SeedboxReport, error) {
	existing, err := scanSeedboxReport(repository.pool.QueryRow(ctx, seedboxReportSelect+` WHERE report.request_id = $1`, command.RequestID))
	if err == nil {
		if existing.UserID != command.UserID || existing.Address != command.Address || existing.Provider != command.Provider ||
			existing.BandwidthMbps != command.BandwidthMbps || existing.Statement != command.Statement {
			return SeedboxReport{}, ErrSeedboxDecisionConflict
		}
		return existing, nil
	}
	if !errors.Is(err, ErrSeedboxReportNotFound) {
		return SeedboxReport{}, err
	}
	row := repository.pool.QueryRow(ctx, `
INSERT INTO tracker_control.seedbox_reports (
    id, request_id, user_id, network, provider, bandwidth_mbps, statement,
    status, version, authorization_decision_id, submitted_at
) VALUES ($1,$2,$3,$4::cidr,$5,$6,$7,'pending',1,$8,$9)
RETURNING id`, command.ReportID, command.RequestID, command.UserID, command.Network,
		command.Provider, command.BandwidthMbps, command.Statement, command.AuthorizationDecisionID, command.OccurredAt)
	var id uuid.UUID
	if err := row.Scan(&id); err != nil {
		if constraint := postgresConstraint(err); constraint == "seedbox_report_one_pending_per_user_idx" {
			return SeedboxReport{}, ErrSeedboxReportPending
		} else if constraint == "seedbox_report_one_approved_binding_idx" {
			return SeedboxReport{}, ErrSeedboxReportApproved
		}
		return SeedboxReport{}, fmt.Errorf("insert Seedbox report: %w", err)
	}
	return scanSeedboxReport(repository.pool.QueryRow(ctx, seedboxReportSelect+` WHERE report.id = $1`, id))
}

func (repository *PostgresRuntimePolicyRepository) DecideSeedboxReport(ctx context.Context, command decideSeedboxReportCommand) (SeedboxReport, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return SeedboxReport{}, fmt.Errorf("begin Seedbox decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var replayReportID uuid.UUID
	var replayDecision string
	var replayReason string
	err = tx.QueryRow(ctx, `SELECT report_id, decision, reason FROM tracker_control.seedbox_report_decisions WHERE request_id = $1`, command.RequestID).Scan(&replayReportID, &replayDecision, &replayReason)
	if err == nil {
		if replayReportID != command.ReportID || SeedboxDecision(replayDecision) != command.Decision || replayReason != command.Reason {
			return SeedboxReport{}, ErrSeedboxDecisionConflict
		}
		report, readErr := scanSeedboxReport(tx.QueryRow(ctx, seedboxReportSelect+` WHERE report.id = $1`, command.ReportID))
		if readErr != nil {
			return SeedboxReport{}, readErr
		}
		if err := tx.Commit(ctx); err != nil {
			return SeedboxReport{}, fmt.Errorf("commit Seedbox decision replay: %w", err)
		}
		return report, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return SeedboxReport{}, fmt.Errorf("read Seedbox decision replay: %w", err)
	}

	report, err := scanSeedboxReport(tx.QueryRow(ctx, seedboxReportSelect+` WHERE report.id = $1 FOR UPDATE OF report`, command.ReportID))
	if err != nil {
		return SeedboxReport{}, err
	}
	if report.Status != SeedboxReportPending || report.Version != command.ExpectedVersion {
		return SeedboxReport{}, ErrSeedboxReportConflict
	}

	status := SeedboxReportRejected
	var policySequence *int64
	if command.Decision == SeedboxDecisionApprove {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('peergo-tracker-runtime-policy-v1', 0))`); err != nil {
			return SeedboxReport{}, fmt.Errorf("lock Tracker runtime policy timeline: %w", err)
		}
		latest, err := readRuntimePolicyRow(tx.QueryRow(ctx, runtimePolicySelect+` ORDER BY sequence DESC LIMIT 1`))
		if err != nil {
			return SeedboxReport{}, err
		}
		for _, rule := range latest.Policy.Seedbox.Rules {
			if rule.UserNumericID == report.UserNumericID && rule.CIDR == hostPrefix(report.Address) {
				return SeedboxReport{}, ErrSeedboxReportApproved
			}
		}
		policy := latest.Policy
		policy.Seedbox.Rules = append([]trackerruntimepolicyv1.SeedboxRule(nil), policy.Seedbox.Rules...)
		policy.Revision = "tracker-runtime-seedbox-" + strings.ReplaceAll(command.RequestID.String(), "-", "")
		policy.Seedbox.Rules = append(policy.Seedbox.Rules, trackerruntimepolicyv1.SeedboxRule{
			ID:   fmt.Sprintf("sb-u%d-%s", report.UserNumericID, strings.ReplaceAll(report.ID.String(), "-", "")[:8]),
			CIDR: hostPrefix(report.Address), UserNumericID: report.UserNumericID,
		})
		policy, err = trackerruntimepolicyv1.NormalizePolicy(policy)
		if err != nil {
			return SeedboxReport{}, ErrRuntimePolicyInput
		}
		issued, err := issueRuntimePolicyTx(ctx, tx, issueRuntimePolicyCommand{IssueRuntimePolicyInput: IssueRuntimePolicyInput{
			RequestID: command.RequestID, ExpectedSequence: latest.Sequence, Policy: policy,
			Reason: "批准盒子申报：" + command.Reason,
		}, ActorID: command.ActorID, OccurredAt: command.OccurredAt,
			Authorization: authz.Decision{ID: command.AuthorizationDecisionID}})
		if err != nil {
			return SeedboxReport{}, err
		}
		status = SeedboxReportApproved
		policySequence = &issued.Sequence
	}

	result := tx.QueryRow(ctx, `
UPDATE tracker_control.seedbox_reports
SET status=$2, version=version+1, decided_at=$3, decided_by=$4,
    decision_reason=$5, policy_sequence=$6
WHERE id=$1 AND status='pending' AND version=$7
RETURNING id`, report.ID, status, command.OccurredAt, command.ActorID, command.Reason, policySequence, command.ExpectedVersion)
	var updatedID uuid.UUID
	if err := result.Scan(&updatedID); errors.Is(err, pgx.ErrNoRows) {
		return SeedboxReport{}, ErrSeedboxReportConflict
	} else if err != nil {
		return SeedboxReport{}, fmt.Errorf("update Seedbox report: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO tracker_control.seedbox_report_decisions (
    id, request_id, report_id, decision, expected_version, reason,
    actor_id, authorization_decision_id, policy_sequence, decided_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, command.DecisionID, command.RequestID,
		command.ReportID, command.Decision, command.ExpectedVersion, command.Reason, command.ActorID,
		command.AuthorizationDecisionID, policySequence, command.OccurredAt)
	if err != nil {
		return SeedboxReport{}, fmt.Errorf("insert Seedbox decision: %w", err)
	}
	updated, err := scanSeedboxReport(tx.QueryRow(ctx, seedboxReportSelect+` WHERE report.id = $1`, updatedID))
	if err != nil {
		return SeedboxReport{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SeedboxReport{}, fmt.Errorf("commit Seedbox decision: %w", err)
	}
	return updated, nil
}

type seedboxReportRow interface{ Scan(...any) error }

func scanSeedboxReport(row seedboxReportRow) (SeedboxReport, error) {
	var report SeedboxReport
	var status string
	err := row.Scan(&report.ID, &report.UserID, &report.UserNumericID, &report.Username,
		&report.Address, &report.Provider, &report.BandwidthMbps, &report.Statement,
		&status, &report.Version, &report.SubmittedAt, &report.DecidedAt,
		&report.DecisionReason, &report.PolicySequence)
	if errors.Is(err, pgx.ErrNoRows) {
		return SeedboxReport{}, ErrSeedboxReportNotFound
	}
	if err != nil {
		return SeedboxReport{}, fmt.Errorf("read Seedbox report: %w", err)
	}
	report.Status = SeedboxReportStatus(status)
	if report.ID == uuid.Nil || report.UserID == uuid.Nil || report.UserNumericID < 1 || !validSeedboxReportStatus(report.Status) || report.Version < 1 || report.SubmittedAt.IsZero() {
		return SeedboxReport{}, errors.New("persisted Seedbox report is invalid")
	}
	report.SubmittedAt = report.SubmittedAt.UTC()
	if report.DecidedAt != nil {
		value := report.DecidedAt.UTC()
		report.DecidedAt = &value
	}
	return report, nil
}

func hostPrefix(address string) string {
	if strings.Contains(address, ":") {
		return address + "/128"
	}
	return address + "/32"
}

func postgresConstraint(err error) string {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		return postgresError.ConstraintName
	}
	return ""
}

var _ SeedboxRegistryRepository = (*PostgresRuntimePolicyRepository)(nil)
