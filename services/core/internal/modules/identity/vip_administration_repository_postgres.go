package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/services/core/internal/generated/identitydb"
)

// ChangeVIP commits the current access projection, immutable operator history,
// reward-benefit timeline and user administration version together. Keeping
// these writes in one transaction prevents the VIP badge, ratio exemption and
// hourly seeding reward from observing different entitlement histories.
func (repository *PostgresAccountRestrictionRepository) ChangeVIP(ctx context.Context, command ChangeVIPCommand) (ManagedUserDetail, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("begin VIP change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)

	user, err := queries.LockManagedUserForAccountRestriction(ctx, command.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, ErrManagedUserNotFound
	}
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("lock managed user for VIP change: %w", err)
	}
	if err := validateLockedManagedUser(user); err != nil {
		return ManagedUserDetail{}, err
	}
	if user.AdministrationVersion != command.ExpectedUserVersion {
		return ManagedUserDetail{}, ErrManagedUserVersionConflict
	}
	if command.Enabled && AccountStatus(user.Status) != AccountStatusActive {
		return ManagedUserDetail{}, ErrManagedUserNotActive
	}

	current, exists, err := lockVIPState(ctx, queries, command.UserID, command.OccurredAt)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	if current.Version != command.ExpectedStateVersion {
		return ManagedUserDetail{}, ErrVIPStateConflict
	}

	var targetUntil *time.Time
	if command.Enabled && command.DurationDays != nil {
		value := command.OccurredAt.AddDate(0, 0, *command.DurationDays).UTC()
		targetUntil = &value
	}
	if command.Enabled && current.Enabled && current.Until == nil && targetUntil == nil {
		return ManagedUserDetail{}, ErrVIPAlreadyActive
	}
	if !command.Enabled && !current.Enabled {
		return ManagedUserDetail{}, ErrVIPNotActive
	}

	transition := VIPTransitionGranted
	if current.Enabled && command.Enabled {
		transition = VIPTransitionRenewed
	} else if !command.Enabled {
		transition = VIPTransitionRevoked
	}
	transitionID := uuid.New()
	stateVersion, err := writeVIPState(ctx, queries, command, exists, targetUntil)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	if stateVersion != command.ExpectedStateVersion+1 {
		return ManagedUserDetail{}, errors.New("VIP change returned an invalid state version")
	}
	if err := queries.InsertVIPTransition(ctx, identitydb.InsertVIPTransitionParams{
		TransitionID:     transitionID,
		UserID:           command.UserID,
		Transition:       string(transition),
		Reason:           command.Reason,
		ActorID:          pgtype.UUID{Bytes: command.ActorID, Valid: true},
		FromEnabled:      current.Enabled,
		FromUntil:        pgNullableTimestamp(current.Until),
		ToEnabled:        command.Enabled,
		ToUntil:          pgNullableTimestamp(targetUntil),
		FromStateVersion: command.ExpectedStateVersion,
		StateVersion:     stateVersion,
		OccurredAt:       timestamp(command.OccurredAt),
	}); err != nil {
		return ManagedUserDetail{}, fmt.Errorf("insert VIP transition: %w", err)
	}
	if rows, err := insertVIPBenefitOutbox(ctx, tx, transitionID, command.UserID,
		command.Enabled, targetUntil, stateVersion, command.OccurredAt); err != nil {
		return ManagedUserDetail{}, err
	} else if rows != 1 {
		return ManagedUserDetail{}, errors.New("VIP benefit delivery was not enqueued")
	}
	if command.Enabled {
		if err := exemptActiveNewcomerAssessmentForVIP(ctx, tx, command.UserID,
			transitionID, command.OccurredAt); err != nil {
			return ManagedUserDetail{}, err
		}
	}
	rewardRevision, err := queries.InsertVIPRewardBenefitRevision(ctx, identitydb.InsertVIPRewardBenefitRevisionParams{
		UserID: command.UserID, OccurredAt: timestamp(command.OccurredAt),
		VipEnabled: command.Enabled, VipUntil: pgNullableTimestamp(targetUntil),
		TransitionID: transitionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, errors.New("VIP reward benefit opening is missing")
	}
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("append VIP reward benefit revision: %w", err)
	}
	if rewardRevision < 2 {
		return ManagedUserDetail{}, errors.New("VIP reward benefit revision is invalid")
	}
	advanced, err := queries.AdvanceUserAdministrationVersion(ctx, identitydb.AdvanceUserAdministrationVersionParams{
		UpdatedAt: timestamp(command.OccurredAt), UserID: command.UserID,
		ExpectedVersion: command.ExpectedUserVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, ErrManagedUserVersionConflict
	}
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("advance user version after VIP change: %w", err)
	}
	if advanced.AdministrationVersion != command.ExpectedUserVersion+1 {
		return ManagedUserDetail{}, errors.New("VIP change returned an invalid user version")
	}

	detail, err := managedUserDetailWithQueries(ctx, queries, command.UserID, command.OccurredAt)
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("read changed VIP state: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedUserDetail{}, fmt.Errorf("commit VIP change: %w", err)
	}
	return detail, nil
}

// exemptActiveNewcomerAssessmentForVIP preserves PtYes's actual VIP exam
// behavior: granting or renewing VIP resolves an already-running newcomer
// assessment once. Expiry or revocation never recreates that assessment.
func exemptActiveNewcomerAssessmentForVIP(ctx context.Context, tx pgx.Tx, userID, transitionID uuid.UUID, occurredAt time.Time) error {
	var assessmentID uuid.UUID
	var fromStatus string
	var creditedUpload, seedingSeconds, version int64
	err := tx.QueryRow(ctx, `
SELECT id, status, current_credited_upload_bytes,
       current_seeding_active_seconds, version
FROM newcomer.assessments
WHERE user_id = $1 AND status IN ('active', 'download_restricted')
FOR UPDATE`, userID).Scan(
		&assessmentID, &fromStatus, &creditedUpload, &seedingSeconds, &version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock newcomer assessment for VIP exemption: %w", err)
	}
	result, err := tx.Exec(ctx, `
UPDATE newcomer.assessments
SET status = 'exempted', resolved_at = $2,
    resolution_code = 'vip_exempted', version = version + 1, updated_at = $2
WHERE id = $1 AND version = $3`, assessmentID, occurredAt, version)
	if err != nil {
		return fmt.Errorf("exempt newcomer assessment for VIP: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("newcomer assessment changed during VIP exemption")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO newcomer.assessment_transitions (
    assessment_id, from_status, to_status, credited_upload_bytes,
    seeding_active_seconds, reason_code, occurred_at,
    source_vip_transition_id
) VALUES ($1, $2, 'exempted', $3, $4, 'vip_exempted', $5, $6)`,
		assessmentID, fromStatus, creditedUpload, seedingSeconds, occurredAt, transitionID); err != nil {
		return fmt.Errorf("append newcomer VIP exemption transition: %w", err)
	}
	return nil
}

func lockVIPState(ctx context.Context, queries *identitydb.Queries, userID uuid.UUID, asOf time.Time) (VIPState, bool, error) {
	row, err := queries.LockVIPState(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return VIPState{}, false, nil
	}
	if err != nil {
		return VIPState{}, false, fmt.Errorf("lock VIP state: %w", err)
	}
	state, err := vipStateFromValues(row.VipEnabled, row.VipUntil, row.Version, asOf)
	return state, true, err
}

func writeVIPState(ctx context.Context, queries *identitydb.Queries, command ChangeVIPCommand, exists bool, until *time.Time) (int64, error) {
	if !exists {
		if !command.Enabled || command.ExpectedStateVersion != 0 {
			return 0, ErrVIPStateConflict
		}
		row, err := queries.InsertVIPState(ctx, identitydb.InsertVIPStateParams{
			UserID: command.UserID, VipUntil: pgNullableTimestamp(until),
			OccurredAt: timestamp(command.OccurredAt),
		})
		if err != nil {
			return 0, fmt.Errorf("insert VIP state: %w", err)
		}
		return row.Version, nil
	}
	row, err := queries.UpdateVIPState(ctx, identitydb.UpdateVIPStateParams{
		VipEnabled: command.Enabled, VipUntil: pgNullableTimestamp(until),
		OccurredAt: timestamp(command.OccurredAt), UserID: command.UserID,
		ExpectedStateVersion: command.ExpectedStateVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrVIPStateConflict
	}
	if err != nil {
		return 0, fmt.Errorf("update VIP state: %w", err)
	}
	return row.Version, nil
}

func loadVIPDetail(ctx context.Context, queries *identitydb.Queries, userID uuid.UUID, asOf time.Time) (VIPState, []VIPTransition, error) {
	row, err := queries.GetVIPState(ctx, userID)
	var state VIPState
	if errors.Is(err, pgx.ErrNoRows) {
		state = VIPState{}
	} else if err != nil {
		return VIPState{}, nil, fmt.Errorf("read VIP state: %w", err)
	} else {
		state, err = vipStateFromValues(row.VipEnabled, row.VipUntil, row.Version, asOf)
		if err != nil {
			return VIPState{}, nil, err
		}
	}

	rows, err := queries.ListVIPTransitions(ctx, userID)
	if err != nil {
		return VIPState{}, nil, fmt.Errorf("list VIP history: %w", err)
	}
	history := make([]VIPTransition, 0, len(rows))
	for _, row := range rows {
		transition := VIPTransition{
			Transition: VIPTransitionKind(row.Transition), Origin: VIPTransitionOrigin(row.Origin),
			ReasonSummary: row.Reason, Enabled: row.ToEnabled,
			StateVersion: row.StateVersion,
		}
		if row.ToUntil.Valid {
			value := row.ToUntil.Time.UTC()
			transition.Until = &value
		}
		if row.ActorNumericID.Valid {
			value := row.ActorNumericID.Int64
			transition.ActorNumericID = &value
		}
		if row.ActorUsername.Valid {
			value := row.ActorUsername.String
			transition.ActorUsername = &value
		}
		if !row.OccurredAt.Valid || transition.StateVersion < 1 ||
			!validVIPTransition(transition.Transition) ||
			!validVIPOrigin(transition.Origin) || strings.TrimSpace(transition.ReasonSummary) == "" {
			return VIPState{}, nil, errors.New("VIP history contains invalid fields")
		}
		transition.OccurredAt = row.OccurredAt.Time.UTC()
		history = append(history, transition)
	}
	return state, history, nil
}

func vipStateFromValues(enabled bool, untilValue pgtype.Timestamptz, version int64, asOf time.Time) (VIPState, error) {
	if version < 1 {
		return VIPState{}, errors.New("VIP state contains an invalid version")
	}
	var until *time.Time
	if untilValue.Valid {
		value := untilValue.Time.UTC()
		until = &value
	}
	return VIPState{
		Enabled: enabled,
		Active:  enabled && (until == nil || until.After(asOf)),
		Until:   until,
		Version: version,
	}, nil
}

func validVIPTransition(value VIPTransitionKind) bool {
	return value == VIPTransitionGranted || value == VIPTransitionRenewed || value == VIPTransitionRevoked
}

func validVIPOrigin(value VIPTransitionOrigin) bool {
	return value == VIPTransitionOriginLegacyMigration || value == VIPTransitionOriginSystemBackfill || value == VIPTransitionOriginStaff
}

func pgNullableTimestamp(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return timestamp(*value)
}
