package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/services/core/internal/generated/identitydb"
)

type manualDownloadRestrictionMutation string

const (
	manualDownloadRestrictionCreate manualDownloadRestrictionMutation = "create"
	manualDownloadRestrictionUpdate manualDownloadRestrictionMutation = "update"
	manualDownloadRestrictionRevoke manualDownloadRestrictionMutation = "revoke"
)

func (repository *PostgresAccountRestrictionRepository) CreateManualDownloadRestriction(ctx context.Context, command ManualDownloadRestrictionCommand) (ManagedUserDetail, error) {
	return repository.changeManualDownloadRestriction(ctx, command, manualDownloadRestrictionCreate)
}

func (repository *PostgresAccountRestrictionRepository) UpdateManualDownloadRestriction(ctx context.Context, command ManualDownloadRestrictionCommand) (ManagedUserDetail, error) {
	return repository.changeManualDownloadRestriction(ctx, command, manualDownloadRestrictionUpdate)
}

func (repository *PostgresAccountRestrictionRepository) RevokeManualDownloadRestriction(ctx context.Context, command ManualDownloadRestrictionCommand) (ManagedUserDetail, error) {
	return repository.changeManualDownloadRestriction(ctx, command, manualDownloadRestrictionRevoke)
}

// changeManualDownloadRestriction serializes all three commands on the user
// row, then updates the current projection, immutable transition and account
// administration version in one transaction. It intentionally does not touch
// ratio-watch, H&R, login sessions, or the legacy migration receipt.
func (repository *PostgresAccountRestrictionRepository) changeManualDownloadRestriction(ctx context.Context, command ManualDownloadRestrictionCommand, mutation manualDownloadRestrictionMutation) (ManagedUserDetail, error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("begin manual download restriction change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)

	user, err := queries.LockManagedUserForAccountRestriction(ctx, command.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, ErrManagedUserNotFound
	}
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("lock managed user for download restriction: %w", err)
	}
	if err := validateLockedManagedUser(user); err != nil {
		return ManagedUserDetail{}, err
	}
	if mutation != manualDownloadRestrictionRevoke && AccountStatus(user.Status) != AccountStatusActive {
		return ManagedUserDetail{}, ErrManagedUserNotActive
	}
	if user.AdministrationVersion != command.ExpectedUserVersion {
		return ManagedUserDetail{}, ErrManagedUserVersionConflict
	}

	current, stateExists, err := lockManualDownloadRestrictionState(ctx, queries, command.UserID)
	if err != nil {
		return ManagedUserDetail{}, err
	}
	if current.Version != command.ExpectedStateVersion {
		return ManagedUserDetail{}, ErrManualDownloadRestrictionConflict
	}

	var (
		transition   ManualDownloadRestrictionTransitionKind
		toRestricted bool
		stateVersion int64
	)
	switch mutation {
	case manualDownloadRestrictionCreate:
		if current.Active {
			return ManagedUserDetail{}, ErrManualDownloadRestrictionActive
		}
		transition = ManualDownloadRestrictionTransitionRestricted
		toRestricted = true
		stateVersion, err = createManualDownloadRestrictionState(ctx, queries, command, stateExists)
	case manualDownloadRestrictionUpdate:
		if !current.Active || !stateExists {
			return ManagedUserDetail{}, ErrManualDownloadRestrictionInactive
		}
		transition = ManualDownloadRestrictionTransitionUpdated
		toRestricted = true
		stateVersion, err = updateManualDownloadRestrictionState(ctx, queries, command)
	case manualDownloadRestrictionRevoke:
		if !current.Active || !stateExists {
			return ManagedUserDetail{}, ErrManualDownloadRestrictionInactive
		}
		transition = ManualDownloadRestrictionTransitionRevoked
		stateVersion, err = revokeManualDownloadRestrictionState(ctx, queries, command)
	default:
		return ManagedUserDetail{}, errors.New("unknown manual download restriction mutation")
	}
	if err != nil {
		return ManagedUserDetail{}, err
	}
	if stateVersion != command.ExpectedStateVersion+1 {
		return ManagedUserDetail{}, errors.New("manual download restriction returned an invalid state version")
	}

	if err := queries.InsertManualDownloadRestrictionTransition(ctx, identitydb.InsertManualDownloadRestrictionTransitionParams{
		TransitionID: uuid.New(), UserID: command.UserID,
		Transition: string(transition), Origin: string(ManualDownloadRestrictionOriginStaff),
		ReasonCode: command.ReasonCode, Reason: command.Reason,
		ActorID:        pgtype.UUID{Bytes: command.ActorID, Valid: true},
		FromRestricted: current.Active, ToRestricted: toRestricted,
		FromStateVersion: command.ExpectedStateVersion, StateVersion: stateVersion,
		OccurredAt: timestamp(command.OccurredAt),
	}); err != nil {
		return ManagedUserDetail{}, fmt.Errorf("insert manual download restriction transition: %w", err)
	}
	advanced, err := queries.AdvanceUserAdministrationVersion(ctx, identitydb.AdvanceUserAdministrationVersionParams{
		UpdatedAt: timestamp(command.OccurredAt), UserID: command.UserID,
		ExpectedVersion: command.ExpectedUserVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUserDetail{}, ErrManagedUserVersionConflict
	}
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("advance user version after download restriction: %w", err)
	}
	if advanced.AdministrationVersion != command.ExpectedUserVersion+1 {
		return ManagedUserDetail{}, errors.New("manual download restriction returned an invalid user version")
	}

	detail, err := managedUserDetailWithQueries(ctx, queries, command.UserID, command.OccurredAt)
	if err != nil {
		return ManagedUserDetail{}, fmt.Errorf("read changed manual download restriction: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ManagedUserDetail{}, fmt.Errorf("commit manual download restriction change: %w", err)
	}
	return detail, nil
}

func lockManualDownloadRestrictionState(ctx context.Context, queries *identitydb.Queries, userID uuid.UUID) (ManualDownloadRestrictionState, bool, error) {
	row, err := queries.LockManualDownloadRestrictionState(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManualDownloadRestrictionState{}, false, nil
	}
	if err != nil {
		return ManualDownloadRestrictionState{}, false, fmt.Errorf("lock manual download restriction state: %w", err)
	}
	state, err := manualDownloadRestrictionStateFromValues(
		row.DownloadRestricted, row.Version, row.DownloadRestrictionOrigin,
		row.DownloadRestrictionReasonCode, row.DownloadRestrictionReason,
		row.DownloadRestrictionStartedAt,
	)
	return state, true, err
}

func createManualDownloadRestrictionState(ctx context.Context, queries *identitydb.Queries, command ManualDownloadRestrictionCommand, stateExists bool) (int64, error) {
	if !stateExists {
		if command.ExpectedStateVersion != 0 {
			return 0, ErrManualDownloadRestrictionConflict
		}
		row, err := queries.InsertManualDownloadRestrictionState(ctx, identitydb.InsertManualDownloadRestrictionStateParams{
			UserID:     command.UserID,
			ReasonCode: pgtype.Text{String: command.ReasonCode, Valid: true},
			Reason:     pgtype.Text{String: command.Reason, Valid: true},
			OccurredAt: timestamp(command.OccurredAt),
			ActorID:    pgtype.UUID{Bytes: command.ActorID, Valid: true},
		})
		if err != nil {
			return 0, fmt.Errorf("insert manual download restriction state: %w", err)
		}
		return row.Version, nil
	}
	row, err := queries.ActivateManualDownloadRestrictionState(ctx, identitydb.ActivateManualDownloadRestrictionStateParams{
		ReasonCode: pgtype.Text{String: command.ReasonCode, Valid: true},
		Reason:     pgtype.Text{String: command.Reason, Valid: true},
		OccurredAt: timestamp(command.OccurredAt),
		ActorID:    pgtype.UUID{Bytes: command.ActorID, Valid: true},
		UserID:     command.UserID, ExpectedStateVersion: command.ExpectedStateVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrManualDownloadRestrictionConflict
	}
	if err != nil {
		return 0, fmt.Errorf("activate manual download restriction state: %w", err)
	}
	return row.Version, nil
}

func updateManualDownloadRestrictionState(ctx context.Context, queries *identitydb.Queries, command ManualDownloadRestrictionCommand) (int64, error) {
	row, err := queries.UpdateManualDownloadRestrictionState(ctx, identitydb.UpdateManualDownloadRestrictionStateParams{
		ReasonCode: pgtype.Text{String: command.ReasonCode, Valid: true},
		Reason:     pgtype.Text{String: command.Reason, Valid: true},
		OccurredAt: timestamp(command.OccurredAt),
		ActorID:    pgtype.UUID{Bytes: command.ActorID, Valid: true},
		UserID:     command.UserID, ExpectedStateVersion: command.ExpectedStateVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrManualDownloadRestrictionConflict
	}
	if err != nil {
		return 0, fmt.Errorf("update manual download restriction state: %w", err)
	}
	return row.Version, nil
}

func revokeManualDownloadRestrictionState(ctx context.Context, queries *identitydb.Queries, command ManualDownloadRestrictionCommand) (int64, error) {
	row, err := queries.RevokeManualDownloadRestrictionState(ctx, identitydb.RevokeManualDownloadRestrictionStateParams{
		OccurredAt: timestamp(command.OccurredAt), UserID: command.UserID,
		ExpectedStateVersion: command.ExpectedStateVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrManualDownloadRestrictionConflict
	}
	if err != nil {
		return 0, fmt.Errorf("revoke manual download restriction state: %w", err)
	}
	return row.Version, nil
}

func loadManualDownloadRestrictionDetail(ctx context.Context, queries *identitydb.Queries, userID uuid.UUID) (ManualDownloadRestrictionState, []ManualDownloadRestrictionTransition, error) {
	stateRow, err := queries.GetManualDownloadRestrictionState(ctx, userID)
	var state ManualDownloadRestrictionState
	if errors.Is(err, pgx.ErrNoRows) {
		state = ManualDownloadRestrictionState{}
	} else if err != nil {
		return ManualDownloadRestrictionState{}, nil, fmt.Errorf("read manual download restriction state: %w", err)
	} else {
		state, err = manualDownloadRestrictionStateFromValues(
			stateRow.DownloadRestricted, stateRow.Version,
			stateRow.DownloadRestrictionOrigin, stateRow.DownloadRestrictionReasonCode,
			stateRow.DownloadRestrictionReason, stateRow.DownloadRestrictionStartedAt,
		)
		if err != nil {
			return ManualDownloadRestrictionState{}, nil, err
		}
	}

	rows, err := queries.ListManualDownloadRestrictionTransitions(ctx, userID)
	if err != nil {
		return ManualDownloadRestrictionState{}, nil, fmt.Errorf("list manual download restriction history: %w", err)
	}
	history := make([]ManualDownloadRestrictionTransition, 0, len(rows))
	for _, row := range rows {
		transition := ManualDownloadRestrictionTransition{
			Transition: ManualDownloadRestrictionTransitionKind(row.Transition),
			Origin:     ManualDownloadRestrictionOrigin(row.Origin),
			ReasonCode: row.ReasonCode, ReasonSummary: row.Reason,
			StateVersion: row.StateVersion,
		}
		if !row.OccurredAt.Valid || transition.StateVersion < 1 ||
			!validManualDownloadRestrictionTransition(transition.Transition) ||
			!validManualDownloadRestrictionOrigin(transition.Origin) ||
			strings.TrimSpace(transition.ReasonCode) == "" || strings.TrimSpace(transition.ReasonSummary) == "" {
			return ManualDownloadRestrictionState{}, nil, errors.New("manual download restriction history contains invalid fields")
		}
		transition.OccurredAt = row.OccurredAt.Time.UTC()
		if row.ActorNumericID.Valid != row.ActorUsername.Valid {
			return ManualDownloadRestrictionState{}, nil, errors.New("manual download restriction actor projection is incomplete")
		}
		if row.ActorNumericID.Valid {
			numericID := row.ActorNumericID.Int64
			username := row.ActorUsername.String
			if numericID < 1 || strings.TrimSpace(username) == "" {
				return ManualDownloadRestrictionState{}, nil, errors.New("manual download restriction actor projection is invalid")
			}
			transition.ActorNumericID = &numericID
			transition.ActorUsername = &username
		}
		history = append(history, transition)
	}
	return state, history, nil
}

func manualDownloadRestrictionStateFromValues(active bool, version int64, origin, reasonCode, reason pgtype.Text, startedAt pgtype.Timestamptz) (ManualDownloadRestrictionState, error) {
	if version < 1 {
		return ManualDownloadRestrictionState{}, errors.New("manual download restriction state has an invalid version")
	}
	state := ManualDownloadRestrictionState{Active: active, Version: version}
	if !active {
		if origin.Valid || reasonCode.Valid || reason.Valid || startedAt.Valid {
			return ManualDownloadRestrictionState{}, errors.New("inactive manual download restriction retains active metadata")
		}
		return state, nil
	}
	parsedOrigin := ManualDownloadRestrictionOrigin(origin.String)
	if !origin.Valid || !reasonCode.Valid || !reason.Valid || !startedAt.Valid ||
		!validManualDownloadRestrictionOrigin(parsedOrigin) || strings.TrimSpace(reasonCode.String) == "" || strings.TrimSpace(reason.String) == "" {
		return ManualDownloadRestrictionState{}, errors.New("active manual download restriction is missing metadata")
	}
	started := startedAt.Time.UTC()
	code := reasonCode.String
	summary := reason.String
	state.Origin = &parsedOrigin
	state.ReasonCode = &code
	state.ReasonSummary = &summary
	state.StartedAt = &started
	return state, nil
}

func validManualDownloadRestrictionOrigin(origin ManualDownloadRestrictionOrigin) bool {
	switch origin {
	case ManualDownloadRestrictionOriginLegacyMigration, ManualDownloadRestrictionOriginSystemBackfill,
		ManualDownloadRestrictionOriginStaff, ManualDownloadRestrictionOriginAppeal:
		return true
	default:
		return false
	}
}

func validManualDownloadRestrictionTransition(transition ManualDownloadRestrictionTransitionKind) bool {
	switch transition {
	case ManualDownloadRestrictionTransitionRestricted, ManualDownloadRestrictionTransitionUpdated,
		ManualDownloadRestrictionTransitionRevoked:
		return true
	default:
		return false
	}
}
