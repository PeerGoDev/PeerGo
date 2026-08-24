// Package legacyinvites imports the PtYes per-user invitation balance and
// invitation-code inventory that predate PeerGo's native invitation ledger.
package legacyinvites

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

const (
	balanceFingerprintDomain = "peergo:migration:ptyes-invitation-balance:v1\x00"
	codeFingerprintDomain    = "peergo:migration:ptyes-invitation-code:v1\x00"
	evidenceDomain           = "peergo:migration:ptyes-invitation-inventory:v1\x00"
	maximumLegacyInvites     = 1_000_000
)

var invitationNamespace = uuid.MustParse("e5f22863-4042-5ef4-a039-c14d3d4f355d")

type Config struct {
	RunID          uuid.UUID
	SnapshotSHA256 [sha256.Size]byte
	MappingVersion string
	ImportedAt     time.Time
}

type Progress struct {
	Phase     string
	Processed int64
	Expected  int64
}

type Result struct {
	RunID                 uuid.UUID
	ObservedAt            time.Time
	BalanceSourceRows     int64
	BalanceTotal          int64
	PositiveBalanceUsers  int64
	InvitationSourceRows  int64
	ClaimedInvitationRows int64
	ActiveInvitationRows  int64
	ImportedActiveTokens  int64
	Duplicate             bool
}

type balanceRow struct {
	LegacyUserID    int64
	Remaining       int
	SourceUpdatedAt time.Time
	Fingerprint     [sha256.Size]byte
}

type codeRow struct {
	LegacyInvitationID int64
	InvitationID       uuid.UUID
	LegacyInviterID    int64
	LegacyInviteeID    *int64
	Claimed            bool
	Role               string
	TokenSHA256        [sha256.Size]byte
	Active             bool
	CreatedAt          time.Time
	ValidUntil         time.Time
	ClaimedAt          *time.Time
	Fingerprint        [sha256.Size]byte
}

type sourceState struct {
	ObservedAt time.Time
	Balances   []balanceRow
	Codes      []codeRow
	Evidence   [sha256.Size]byte
}

func Import(
	ctx context.Context,
	source, core *pgxpool.Pool,
	config Config,
	progress func(Progress),
) (Result, error) {
	config = normalizeConfig(config)
	if err := validateConfig(source, core, config); err != nil {
		return Result{}, err
	}
	if progress == nil {
		progress = func(Progress) {}
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return Result{}, err
	}
	if err := requireRun(ctx, core, config); err != nil {
		return Result{}, err
	}

	existingObservedAt, receiptExists, err := receiptObservedAt(ctx, core, config.RunID)
	if err != nil {
		return Result{}, err
	}
	state, err := readSource(ctx, source, existingObservedAt)
	if err != nil {
		return Result{}, err
	}
	want := sourceResult(config.RunID, state)
	if receiptExists {
		got, verifyErr := verifyState(ctx, core, config, state, want)
		if verifyErr != nil {
			return Result{}, verifyErr
		}
		got.Duplicate = true
		return got, nil
	}

	tx, err := core.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("begin legacy invitation inventory import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := requireMappings(ctx, tx, int64(len(state.Balances))); err != nil {
		return Result{}, err
	}
	if err := createStages(ctx, tx); err != nil {
		return Result{}, err
	}
	if err := stageBalances(ctx, tx, state.Balances, progress); err != nil {
		return Result{}, err
	}
	if err := stageCodes(ctx, tx, state.Codes, progress); err != nil {
		return Result{}, err
	}

	importedAt := config.ImportedAt
	if importedAt.Before(state.ObservedAt) {
		importedAt = state.ObservedAt
	}
	if err := insertBalances(ctx, tx, config, importedAt); err != nil {
		return Result{}, err
	}
	activeTokens, err := insertCodes(ctx, tx, config, state.ObservedAt, importedAt)
	if err != nil {
		return Result{}, err
	}
	want.ImportedActiveTokens = activeTokens
	if err := insertReceipt(ctx, tx, state, want, importedAt); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit legacy invitation inventory import: %w", err)
	}

	got, err := verifyState(ctx, core, config, state, want)
	if err != nil {
		return Result{}, err
	}
	return got, nil
}

func Verify(ctx context.Context, source, core *pgxpool.Pool, config Config) (Result, error) {
	config = normalizeConfig(config)
	if err := validateConfig(source, core, config); err != nil {
		return Result{}, err
	}
	if err := platformpostgres.RequireCurrentMigration(ctx, core); err != nil {
		return Result{}, err
	}
	if err := requireRun(ctx, core, config); err != nil {
		return Result{}, err
	}
	observedAt, exists, err := receiptObservedAt(ctx, core, config.RunID)
	if err != nil {
		return Result{}, err
	}
	if !exists {
		return Result{}, errors.New("legacy invitation inventory receipt is missing")
	}
	state, err := readSource(ctx, source, observedAt)
	if err != nil {
		return Result{}, err
	}
	result, err := verifyState(ctx, core, config, state, sourceResult(config.RunID, state))
	if err != nil {
		return Result{}, err
	}
	result.Duplicate = true
	return result, nil
}

func normalizeConfig(config Config) Config {
	config.MappingVersion = strings.TrimSpace(config.MappingVersion)
	config.ImportedAt = config.ImportedAt.UTC().Truncate(time.Microsecond)
	return config
}

func validateConfig(source, core *pgxpool.Pool, config Config) error {
	if source == nil || core == nil || config.RunID == uuid.Nil ||
		config.SnapshotSHA256 == ([sha256.Size]byte{}) ||
		config.MappingVersion == "" || config.ImportedAt.IsZero() {
		return errors.New("legacy invitation inventory configuration is invalid")
	}
	return nil
}

func requireRun(ctx context.Context, core *pgxpool.Pool, config Config) error {
	var snapshot []byte
	var mappingVersion, state string
	err := core.QueryRow(ctx, `
SELECT source_snapshot_sha256, mapping_version, state
FROM migration.runs WHERE id = $1`, config.RunID).Scan(&snapshot, &mappingVersion, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("legacy invitation inventory migration run was not found")
	}
	if err != nil {
		return fmt.Errorf("read legacy invitation inventory migration run: %w", err)
	}
	if !bytes.Equal(snapshot, config.SnapshotSHA256[:]) || mappingVersion != config.MappingVersion {
		return errors.New("legacy invitation inventory run identity does not match the snapshot")
	}
	if state != "importing" && state != "imported" && state != "reconciled" {
		return fmt.Errorf("legacy invitation inventory run state %q cannot accept an import", state)
	}
	return nil
}

func receiptObservedAt(ctx context.Context, core *pgxpool.Pool, runID uuid.UUID) (*time.Time, bool, error) {
	var observedAt time.Time
	err := core.QueryRow(ctx, `
SELECT observed_at
FROM migration.legacy_invitation_inventory_imports
WHERE run_id = $1`, runID).Scan(&observedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read legacy invitation inventory receipt timestamp: %w", err)
	}
	observedAt = observedAt.UTC().Truncate(time.Microsecond)
	return &observedAt, true, nil
}

func readSource(ctx context.Context, source *pgxpool.Pool, fixedObservedAt *time.Time) (sourceState, error) {
	tx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return sourceState{}, fmt.Errorf("begin PtYes invitation inventory snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var observedAt time.Time
	if fixedObservedAt == nil {
		if err := tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&observedAt); err != nil {
			return sourceState{}, fmt.Errorf("read PtYes invitation inventory timestamp: %w", err)
		}
	} else {
		observedAt = *fixedObservedAt
	}
	observedAt = observedAt.UTC().Truncate(time.Microsecond)
	balances, err := readBalances(ctx, tx, observedAt)
	if err != nil {
		return sourceState{}, err
	}
	codes, err := readCodes(ctx, tx, observedAt)
	if err != nil {
		return sourceState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sourceState{}, fmt.Errorf("commit PtYes invitation inventory snapshot: %w", err)
	}

	hash := sha256.New()
	_, _ = hash.Write([]byte(evidenceDomain))
	writeString(hash, observedAt.Format(time.RFC3339Nano))
	for _, row := range balances {
		_, _ = hash.Write(row.Fingerprint[:])
	}
	for _, row := range codes {
		_, _ = hash.Write(row.Fingerprint[:])
	}
	var evidence [sha256.Size]byte
	copy(evidence[:], hash.Sum(nil))
	return sourceState{ObservedAt: observedAt, Balances: balances, Codes: codes, Evidence: evidence}, nil
}

func readBalances(ctx context.Context, tx pgx.Tx, observedAt time.Time) ([]balanceRow, error) {
	rows, err := tx.Query(ctx, `
SELECT id::bigint, COALESCE(remaining_invites, 0)::bigint, updated_at
FROM public.users
ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes invitation balances: %w", err)
	}
	defer rows.Close()
	result := make([]balanceRow, 0, 16_000)
	for rows.Next() {
		var row balanceRow
		var remaining int64
		if err := rows.Scan(&row.LegacyUserID, &remaining, &row.SourceUpdatedAt); err != nil {
			return nil, fmt.Errorf("scan PtYes invitation balance: %w", err)
		}
		row.SourceUpdatedAt = row.SourceUpdatedAt.UTC().Truncate(time.Microsecond)
		if row.LegacyUserID < 1 || remaining < 0 || remaining > maximumLegacyInvites ||
			row.SourceUpdatedAt.IsZero() || row.SourceUpdatedAt.After(observedAt) {
			return nil, fmt.Errorf("PtYes invitation balance for user %d is invalid", row.LegacyUserID)
		}
		row.Remaining = int(remaining)
		row.Fingerprint = balanceFingerprint(row)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish PtYes invitation balance query: %w", err)
	}
	return result, nil
}

func readCodes(ctx context.Context, tx pgx.Tx, observedAt time.Time) ([]codeRow, error) {
	rows, err := tx.Query(ctx, `
SELECT id::bigint, inviting_user::bigint, token,
       COALESCE(NULLIF(btrim(role), ''), 'user'), COALESCE(claimed, false),
       valid_until, claimed_at, claimed_by_uid::bigint, created_at
FROM public.invites
ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query PtYes invitation codes: %w", err)
	}
	defer rows.Close()
	result := make([]codeRow, 0, 512)
	for rows.Next() {
		var row codeRow
		var token string
		var claimedAt pgtype.Timestamptz
		var invitee pgtype.Int8
		if err := rows.Scan(
			&row.LegacyInvitationID, &row.LegacyInviterID, &token, &row.Role,
			&row.Claimed, &row.ValidUntil, &claimedAt, &invitee, &row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan PtYes invitation code: %w", err)
		}
		row.Role = strings.TrimSpace(row.Role)
		row.CreatedAt = row.CreatedAt.UTC().Truncate(time.Microsecond)
		row.ValidUntil = row.ValidUntil.UTC().Truncate(time.Microsecond)
		if claimedAt.Valid {
			value := claimedAt.Time.UTC().Truncate(time.Microsecond)
			row.ClaimedAt = &value
		}
		if invitee.Valid {
			value := invitee.Int64
			row.LegacyInviteeID = &value
		}
		if err := validateCode(row, token, observedAt); err != nil {
			return nil, err
		}
		row.InvitationID = uuid.NewSHA1(invitationNamespace, []byte(strconv.FormatInt(row.LegacyInvitationID, 10)))
		row.TokenSHA256 = sha256.Sum256([]byte(token))
		row.Active = !row.Claimed && row.ValidUntil.After(observedAt)
		row.Fingerprint = codeFingerprint(row)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish PtYes invitation code query: %w", err)
	}
	return result, nil
}

func validateCode(row codeRow, token string, observedAt time.Time) error {
	claimedFieldsMatch := (row.Claimed && row.ClaimedAt != nil && row.LegacyInviteeID != nil) ||
		(!row.Claimed && row.ClaimedAt == nil && row.LegacyInviteeID == nil)
	if row.LegacyInvitationID < 1 || row.LegacyInviterID < 0 || len(token) != 64 ||
		!isLowerHex(token) || row.Role == "" || len(row.Role) > 32 ||
		row.CreatedAt.IsZero() || !row.ValidUntil.After(row.CreatedAt) ||
		row.CreatedAt.After(observedAt) || !claimedFieldsMatch ||
		(row.ClaimedAt != nil && row.ClaimedAt.Before(row.CreatedAt)) ||
		(row.LegacyInviteeID != nil && *row.LegacyInviteeID < 1) {
		return fmt.Errorf("PtYes invitation code %d is invalid", row.LegacyInvitationID)
	}
	return nil
}

func isLowerHex(value string) bool {
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func requireMappings(ctx context.Context, tx pgx.Tx, expected int64) error {
	var mappings, identities int64
	if err := tx.QueryRow(ctx, `
SELECT
    count(*)::bigint,
    count(users.id)::bigint
FROM migration.user_id_map AS mapping
LEFT JOIN identity.users AS users ON users.id = mapping.user_id
WHERE mapping.source_system = 'ptyes'`).Scan(&mappings, &identities); err != nil {
		return fmt.Errorf("verify legacy invitation user mappings: %w", err)
	}
	if mappings != expected || identities != expected {
		return fmt.Errorf("legacy invitation mappings are incomplete: mappings=%d identities=%d expected=%d", mappings, identities, expected)
	}
	return nil
}

func createStages(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `
CREATE TEMP TABLE legacy_invitation_balance_stage (
    legacy_user_id bigint PRIMARY KEY,
    remaining_invites integer NOT NULL,
    source_updated_at timestamptz NOT NULL,
    source_fingerprint bytea NOT NULL
) ON COMMIT DROP;

CREATE TEMP TABLE legacy_invitation_code_stage (
    legacy_invitation_id bigint PRIMARY KEY,
    invitation_id uuid NOT NULL UNIQUE,
    legacy_inviter_id bigint NOT NULL,
    legacy_invitee_id bigint,
    source_claimed boolean NOT NULL,
    source_role text NOT NULL,
    source_token_sha256 bytea NOT NULL,
    source_active boolean NOT NULL,
    source_fingerprint bytea NOT NULL,
    source_created_at timestamptz NOT NULL,
    source_valid_until timestamptz NOT NULL,
    source_claimed_at timestamptz
) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("create legacy invitation inventory stages: %w", err)
	}
	return nil
}

func stageBalances(ctx context.Context, tx pgx.Tx, rows []balanceRow, progress func(Progress)) error {
	count, err := tx.CopyFrom(ctx, pgx.Identifier{"legacy_invitation_balance_stage"}, []string{
		"legacy_user_id", "remaining_invites", "source_updated_at", "source_fingerprint",
	}, pgx.CopyFromSlice(len(rows), func(index int) ([]any, error) {
		row := rows[index]
		if (index+1)%1000 == 0 || index+1 == len(rows) {
			progress(Progress{Phase: "invitation_balances", Processed: int64(index + 1), Expected: int64(len(rows))})
		}
		return []any{row.LegacyUserID, row.Remaining, row.SourceUpdatedAt, row.Fingerprint[:]}, nil
	}))
	if err != nil {
		return fmt.Errorf("stage PtYes invitation balances: %w", err)
	}
	if count != int64(len(rows)) {
		return fmt.Errorf("staged %d PtYes invitation balances, expected %d", count, len(rows))
	}
	return nil
}

func stageCodes(ctx context.Context, tx pgx.Tx, rows []codeRow, progress func(Progress)) error {
	count, err := tx.CopyFrom(ctx, pgx.Identifier{"legacy_invitation_code_stage"}, []string{
		"legacy_invitation_id", "invitation_id", "legacy_inviter_id", "legacy_invitee_id",
		"source_claimed", "source_role", "source_token_sha256", "source_active",
		"source_fingerprint", "source_created_at", "source_valid_until", "source_claimed_at",
	}, pgx.CopyFromSlice(len(rows), func(index int) ([]any, error) {
		row := rows[index]
		if (index+1)%100 == 0 || index+1 == len(rows) {
			progress(Progress{Phase: "invitation_codes", Processed: int64(index + 1), Expected: int64(len(rows))})
		}
		return []any{
			row.LegacyInvitationID, row.InvitationID, row.LegacyInviterID, row.LegacyInviteeID,
			row.Claimed, row.Role, row.TokenSHA256[:], row.Active, row.Fingerprint[:],
			row.CreatedAt, row.ValidUntil, row.ClaimedAt,
		}, nil
	}))
	if err != nil {
		return fmt.Errorf("stage PtYes invitation codes: %w", err)
	}
	if count != int64(len(rows)) {
		return fmt.Errorf("staged %d PtYes invitation codes, expected %d", count, len(rows))
	}
	return nil
}

func insertBalances(ctx context.Context, tx pgx.Tx, config Config, importedAt time.Time) error {
	command, err := tx.Exec(ctx, `
INSERT INTO migration.legacy_invitation_balance_openings (
    source_system, legacy_user_id, user_id, remaining_invites,
    source_updated_at, source_fingerprint, first_run_id, imported_at
)
SELECT
    'ptyes', stage.legacy_user_id, mapping.user_id, stage.remaining_invites,
    stage.source_updated_at, stage.source_fingerprint, $1, $2
FROM legacy_invitation_balance_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_user_id`, config.RunID, importedAt)
	if err != nil {
		return fmt.Errorf("insert legacy invitation balance openings: %w", err)
	}
	if command.RowsAffected() == 0 {
		return errors.New("legacy invitation balance opening import inserted no rows")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.invitation_accounts (
    user_id, remaining_invites, version, updated_at
)
SELECT mapping.user_id, stage.remaining_invites, 1, $1
FROM legacy_invitation_balance_stage AS stage
JOIN migration.user_id_map AS mapping
  ON mapping.source_system = 'ptyes'
 AND mapping.legacy_user_id = stage.legacy_user_id`, importedAt); err != nil {
		return fmt.Errorf("seed invitation account balances: %w", err)
	}
	return nil
}

func insertCodes(ctx context.Context, tx pgx.Tx, config Config, observedAt, importedAt time.Time) (int64, error) {
	active, err := tx.Exec(ctx, `
INSERT INTO identity.registration_invitations (
    id, token_sha256, note, expires_at, issuer_user_id, source_kind,
    issued_authorization_decision_id, created_at
)
SELECT
    stage.invitation_id, stage.source_token_sha256, '', stage.source_valid_until,
    inviter.user_id, 'legacy', NULL, stage.source_created_at
FROM legacy_invitation_code_stage AS stage
JOIN migration.user_id_map AS inviter
  ON inviter.source_system = 'ptyes'
 AND inviter.legacy_user_id = stage.legacy_inviter_id
WHERE stage.source_active`)
	if err != nil {
		return 0, fmt.Errorf("insert active legacy registration invitations: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO migration.legacy_invitation_code_openings (
    legacy_invitation_id, invitation_id, first_run_id,
    legacy_inviter_id, inviter_user_id, legacy_invitee_id, invitee_user_id,
    source_claimed, source_role, source_token_sha256,
    registration_invitation_id, source_fingerprint,
    source_created_at, source_valid_until, source_claimed_at,
    observed_at, imported_at
)
SELECT
    stage.legacy_invitation_id, stage.invitation_id, $1,
    stage.legacy_inviter_id, inviter.user_id,
    stage.legacy_invitee_id, invitee.user_id,
    stage.source_claimed, stage.source_role,
    CASE WHEN native_invitation.id IS NOT NULL THEN stage.source_token_sha256 END,
    native_invitation.id, stage.source_fingerprint,
    stage.source_created_at, stage.source_valid_until, stage.source_claimed_at,
    $2, $3
FROM legacy_invitation_code_stage AS stage
LEFT JOIN migration.user_id_map AS inviter
  ON inviter.source_system = 'ptyes'
 AND inviter.legacy_user_id = stage.legacy_inviter_id
LEFT JOIN migration.user_id_map AS invitee
  ON invitee.source_system = 'ptyes'
 AND invitee.legacy_user_id = stage.legacy_invitee_id
LEFT JOIN identity.registration_invitations AS native_invitation
  ON native_invitation.id = stage.invitation_id`, config.RunID, observedAt, importedAt); err != nil {
		return 0, fmt.Errorf("insert legacy invitation code openings: %w", err)
	}
	return active.RowsAffected(), nil
}

func insertReceipt(ctx context.Context, tx pgx.Tx, state sourceState, result Result, importedAt time.Time) error {
	_, err := tx.Exec(ctx, `
INSERT INTO migration.legacy_invitation_inventory_imports (
    run_id, source_evidence_sha256, observed_at,
    balance_source_rows, balance_total, positive_balance_users,
    invitation_source_rows, claimed_invitation_rows, active_invitation_rows,
    imported_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		result.RunID, state.Evidence[:], state.ObservedAt,
		result.BalanceSourceRows, result.BalanceTotal, result.PositiveBalanceUsers,
		result.InvitationSourceRows, result.ClaimedInvitationRows,
		result.ActiveInvitationRows, importedAt,
	)
	if err != nil {
		return fmt.Errorf("insert legacy invitation inventory receipt: %w", err)
	}
	return nil
}

func verifyState(ctx context.Context, core *pgxpool.Pool, config Config, state sourceState, want Result) (Result, error) {
	var evidence []byte
	var got Result
	got.RunID = config.RunID
	err := core.QueryRow(ctx, `
SELECT source_evidence_sha256, observed_at,
       balance_source_rows, balance_total, positive_balance_users,
       invitation_source_rows, claimed_invitation_rows, active_invitation_rows
FROM migration.legacy_invitation_inventory_imports
WHERE run_id = $1`, config.RunID).Scan(
		&evidence, &got.ObservedAt,
		&got.BalanceSourceRows, &got.BalanceTotal, &got.PositiveBalanceUsers,
		&got.InvitationSourceRows, &got.ClaimedInvitationRows, &got.ActiveInvitationRows,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, errors.New("legacy invitation inventory receipt is missing")
	}
	if err != nil {
		return Result{}, fmt.Errorf("read legacy invitation inventory receipt: %w", err)
	}
	got.ObservedAt = got.ObservedAt.UTC().Truncate(time.Microsecond)
	if !bytes.Equal(evidence, state.Evidence[:]) || !got.ObservedAt.Equal(want.ObservedAt) ||
		got.BalanceSourceRows != want.BalanceSourceRows || got.BalanceTotal != want.BalanceTotal ||
		got.PositiveBalanceUsers != want.PositiveBalanceUsers ||
		got.InvitationSourceRows != want.InvitationSourceRows ||
		got.ClaimedInvitationRows != want.ClaimedInvitationRows ||
		got.ActiveInvitationRows != want.ActiveInvitationRows {
		return Result{}, errors.New("legacy invitation inventory receipt conflicts with the source snapshot")
	}

	var balanceOpenings, accounts, balanceConflicts int64
	if err := core.QueryRow(ctx, `
WITH event_totals AS (
    SELECT user_id, COALESCE(sum(delta), 0)::bigint AS delta
    FROM identity.invitation_balance_events
    GROUP BY user_id
)
SELECT
    count(opening.legacy_user_id)::bigint,
    count(account.user_id)::bigint,
    count(*) FILTER (WHERE
        account.user_id IS NULL
        OR account.remaining_invites::bigint
           <> opening.remaining_invites::bigint + COALESCE(event_totals.delta, 0)
    )::bigint
FROM migration.legacy_invitation_balance_openings AS opening
LEFT JOIN identity.invitation_accounts AS account ON account.user_id = opening.user_id
LEFT JOIN event_totals ON event_totals.user_id = opening.user_id
WHERE opening.first_run_id = $1`, config.RunID).Scan(&balanceOpenings, &accounts, &balanceConflicts); err != nil {
		return Result{}, fmt.Errorf("verify imported invitation balances: %w", err)
	}
	if balanceOpenings != want.BalanceSourceRows || accounts != want.BalanceSourceRows || balanceConflicts != 0 {
		return Result{}, fmt.Errorf(
			"legacy invitation balances are incomplete: openings=%d accounts=%d expected=%d conflicts=%d",
			balanceOpenings, accounts, want.BalanceSourceRows, balanceConflicts,
		)
	}

	var codeOpenings, activeTokens, tokenConflicts int64
	if err := core.QueryRow(ctx, `
SELECT
    count(opening.legacy_invitation_id)::bigint,
    count(opening.registration_invitation_id)::bigint,
    count(*) FILTER (WHERE
        opening.registration_invitation_id IS NOT NULL
        AND (
            invitation.id IS NULL
            OR invitation.source_kind <> 'legacy'
            OR invitation.issuer_user_id IS DISTINCT FROM opening.inviter_user_id
            OR invitation.token_sha256 IS DISTINCT FROM opening.source_token_sha256
            OR invitation.created_at IS DISTINCT FROM opening.source_created_at
            OR invitation.expires_at IS DISTINCT FROM opening.source_valid_until
        )
    )::bigint
FROM migration.legacy_invitation_code_openings AS opening
LEFT JOIN identity.registration_invitations AS invitation
  ON invitation.id = opening.registration_invitation_id
WHERE opening.first_run_id = $1`, config.RunID).Scan(&codeOpenings, &activeTokens, &tokenConflicts); err != nil {
		return Result{}, fmt.Errorf("verify imported invitation code history: %w", err)
	}
	if codeOpenings != want.InvitationSourceRows || activeTokens != want.ActiveInvitationRows || tokenConflicts != 0 {
		return Result{}, fmt.Errorf(
			"legacy invitation code history is incomplete: openings=%d/%d active_tokens=%d/%d conflicts=%d",
			codeOpenings, want.InvitationSourceRows, activeTokens, want.ActiveInvitationRows, tokenConflicts,
		)
	}
	got.ImportedActiveTokens = activeTokens
	return got, nil
}

func sourceResult(runID uuid.UUID, state sourceState) Result {
	result := Result{RunID: runID, ObservedAt: state.ObservedAt}
	result.BalanceSourceRows = int64(len(state.Balances))
	for _, row := range state.Balances {
		result.BalanceTotal += int64(row.Remaining)
		if row.Remaining > 0 {
			result.PositiveBalanceUsers++
		}
	}
	result.InvitationSourceRows = int64(len(state.Codes))
	for _, row := range state.Codes {
		if row.Claimed {
			result.ClaimedInvitationRows++
		}
		if row.Active {
			result.ActiveInvitationRows++
		}
	}
	return result
}

func balanceFingerprint(row balanceRow) [sha256.Size]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte(balanceFingerprintDomain))
	writeInt64(hash, row.LegacyUserID)
	writeInt64(hash, int64(row.Remaining))
	writeString(hash, row.SourceUpdatedAt.Format(time.RFC3339Nano))
	return sum(hash)
}

func codeFingerprint(row codeRow) [sha256.Size]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte(codeFingerprintDomain))
	writeInt64(hash, row.LegacyInvitationID)
	writeInt64(hash, row.LegacyInviterID)
	if row.LegacyInviteeID == nil {
		_, _ = hash.Write([]byte{0})
	} else {
		_, _ = hash.Write([]byte{1})
		writeInt64(hash, *row.LegacyInviteeID)
	}
	writeBool(hash, row.Claimed)
	writeString(hash, row.Role)
	_, _ = hash.Write(row.TokenSHA256[:])
	writeString(hash, row.CreatedAt.Format(time.RFC3339Nano))
	writeString(hash, row.ValidUntil.Format(time.RFC3339Nano))
	if row.ClaimedAt == nil {
		_, _ = hash.Write([]byte{0})
	} else {
		_, _ = hash.Write([]byte{1})
		writeString(hash, row.ClaimedAt.Format(time.RFC3339Nano))
	}
	return sum(hash)
}

type byteWriter interface {
	Write([]byte) (int, error)
}

func writeInt64(writer byteWriter, value int64) {
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], uint64(value))
	_, _ = writer.Write(buffer[:])
}

func writeString(writer byteWriter, value string) {
	writeInt64(writer, int64(len(value)))
	_, _ = writer.Write([]byte(value))
}

func writeBool(writer byteWriter, value bool) {
	if value {
		_, _ = writer.Write([]byte{1})
		return
	}
	_, _ = writer.Write([]byte{0})
}

func sum(writer interface{ Sum([]byte) []byte }) [sha256.Size]byte {
	var result [sha256.Size]byte
	copy(result[:], writer.Sum(nil))
	return result
}
