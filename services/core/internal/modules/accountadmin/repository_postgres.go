// Package accountadmin owns the cross-ledger repository used by the narrow
// staff user-data adjustment command.  It composes existing economy and
// progression kernels instead of writing their projections directly.
package accountadmin

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/economy"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"github.com/peergo/peergo/services/core/internal/modules/progression"
)

const administratorAdjustmentPolicy = "administrator-adjustment-v1"

var (
	magicTransactionNamespace = uuid.MustParse("3c6a45e0-afc7-5bfd-bec8-81a238d13231")
	experienceEntryNamespace  = uuid.MustParse("48223745-b20d-5ba0-802b-c2ac2bd6fc42")
	invitationEventNamespace  = uuid.MustParse("a68ac022-0f94-5a53-8eab-d9f36bc9fab1")
)

type PostgresRepository struct {
	pool        *pgxpool.Pool
	economy     *economy.PostgresRepository
	progression *progression.PostgresRepository
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("account administration database is required")
	}
	economyRepository, err := economy.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("compose economy adjustment repository: %w", err)
	}
	progressionRepository, err := progression.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("compose progression adjustment repository: %w", err)
	}
	return &PostgresRepository{
		pool: pool, economy: economyRepository, progression: progressionRepository,
	}, nil
}

type pendingDomainEvent struct {
	trafficUploadedDelta   int64
	trafficDownloadedDelta int64
	rawUploadedAfter       int64
	rawDownloadedAfter     int64
	invitationDelta        int64
	invitationBalanceAfter int64
}

func (repository *PostgresRepository) AdjustManagedUser(ctx context.Context, command identity.ManagedUserAdjustmentCommand) error {
	if command.AdjustmentID == uuid.Nil || command.UserID == uuid.Nil || command.ActorID == uuid.Nil ||
		command.ExpectedUserVersion < 1 || strings.TrimSpace(command.Delta) != command.Delta ||
		strings.TrimSpace(command.Reason) != command.Reason || command.Reason == "" ||
		command.OccurredAt.IsZero() || !command.Authorization.Allow || command.Authorization.ID == uuid.Nil {
		return identity.ErrUserAdministrationInput
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin managed user adjustment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	lockKey := "managed-user-adjustment:" + command.AdjustmentID.String()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return fmt.Errorf("lock managed user adjustment: %w", err)
	}
	replayed, err := managedUserAdjustmentReplay(ctx, tx, command)
	if err != nil {
		return err
	}
	if replayed {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit managed user adjustment replay: %w", err)
		}
		return nil
	}

	var currentVersion int64
	err = tx.QueryRow(ctx, `
SELECT administration_version
FROM identity.users
WHERE id = $1
FOR UPDATE`, command.UserID).Scan(&currentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrManagedUserNotFound
	}
	if err != nil {
		return fmt.Errorf("lock managed user: %w", err)
	}
	if currentVersion != command.ExpectedUserVersion {
		return identity.ErrManagedUserVersionConflict
	}

	balanceAfter, linkedLedgerID, domainEvent, err := repository.applyAdjustment(ctx, tx, command)
	if err != nil {
		return err
	}
	nextVersion := currentVersion + 1
	if _, err := tx.Exec(ctx, `
UPDATE identity.users
SET administration_version = $2,
    updated_at = GREATEST(updated_at, $3)
WHERE id = $1`, command.UserID, nextVersion, command.OccurredAt); err != nil {
		return fmt.Errorf("advance managed user version: %w", err)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO identity.managed_user_adjustment_events (
    id, idempotency_key, user_id, actor_user_id, field, delta,
    balance_after, reason_summary, authorization_decision_id,
    linked_ledger_id, user_version_before, user_version_after,
    occurred_at, recorded_at
) VALUES (
    $1, $1, $2, $3, $4, $5::numeric(38, 20),
    $6::numeric(38, 20), $7, $8, NULLIF($9::uuid, $10::uuid),
    $11, $12, $13, $13
)`, command.AdjustmentID, command.UserID, command.ActorID, string(command.Field),
		command.Delta, balanceAfter, command.Reason, command.Authorization.ID,
		linkedLedgerID, uuid.Nil, currentVersion, nextVersion, command.OccurredAt); err != nil {
		return fmt.Errorf("record managed user adjustment: %w", err)
	}
	if err := insertDomainEvent(ctx, tx, command, domainEvent); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit managed user adjustment: %w", err)
	}
	return nil
}

func managedUserAdjustmentReplay(ctx context.Context, tx pgx.Tx, command identity.ManagedUserAdjustmentCommand) (bool, error) {
	var userID, actorID uuid.UUID
	var field, delta, reason string
	var versionBefore int64
	err := tx.QueryRow(ctx, `
SELECT user_id, actor_user_id, field, delta::text, reason_summary, user_version_before
FROM identity.managed_user_adjustment_events
WHERE idempotency_key = $1`, command.AdjustmentID).Scan(
		&userID, &actorID, &field, &delta, &reason, &versionBefore,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read managed user adjustment replay: %w", err)
	}
	if userID != command.UserID || actorID != command.ActorID ||
		field != string(command.Field) || canonicalNumericText(delta) != canonicalNumericText(command.Delta) ||
		reason != command.Reason || versionBefore != command.ExpectedUserVersion {
		return false, identity.ErrManagedUserAdjustmentConflict
	}
	return true, nil
}

func (repository *PostgresRepository) applyAdjustment(ctx context.Context, tx pgx.Tx, command identity.ManagedUserAdjustmentCommand) (string, uuid.UUID, pendingDomainEvent, error) {
	switch command.Field {
	case identity.ManagedUserAdjustmentUploadedBytes, identity.ManagedUserAdjustmentDownloadedBytes:
		balance, event, err := adjustTraffic(ctx, tx, command)
		return balance, uuid.Nil, event, err
	case identity.ManagedUserAdjustmentMagicBalance:
		balance, ledgerID, err := repository.adjustMagic(ctx, tx, command)
		return balance, ledgerID, pendingDomainEvent{}, err
	case identity.ManagedUserAdjustmentExperience:
		balance, ledgerID, err := repository.adjustExperience(ctx, tx, command)
		return balance, ledgerID, pendingDomainEvent{}, err
	case identity.ManagedUserAdjustmentRemainingInvites:
		balance, event, err := adjustInvitations(ctx, tx, command)
		return balance, uuid.Nil, event, err
	case identity.ManagedUserAdjustmentDonationAmount:
		balance, err := adjustDonation(ctx, tx, command)
		return balance, uuid.Nil, pendingDomainEvent{}, err
	default:
		return "", uuid.Nil, pendingDomainEvent{}, identity.ErrUserAdministrationInput
	}
}

func adjustTraffic(ctx context.Context, tx pgx.Tx, command identity.ManagedUserAdjustmentCommand) (string, pendingDomainEvent, error) {
	delta, err := strconv.ParseInt(command.Delta, 10, 64)
	if err != nil || delta == 0 {
		return "", pendingDomainEvent{}, identity.ErrUserAdministrationInput
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO traffic.user_totals (
    user_id, raw_uploaded, raw_downloaded, credited_uploaded,
    charged_downloaded, entry_count, version, updated_at
) VALUES ($1, 0, 0, 0, 0, 0, 0, $2)
ON CONFLICT (user_id) DO NOTHING`, command.UserID, command.OccurredAt); err != nil {
		return "", pendingDomainEvent{}, fmt.Errorf("ensure traffic totals: %w", err)
	}
	var rawUploaded, rawDownloaded, creditedUploaded, chargedDownloaded int64
	if err := tx.QueryRow(ctx, `
SELECT raw_uploaded, raw_downloaded, credited_uploaded, charged_downloaded
FROM traffic.user_totals
WHERE user_id = $1
FOR UPDATE`, command.UserID).Scan(
		&rawUploaded, &rawDownloaded, &creditedUploaded, &chargedDownloaded,
	); err != nil {
		return "", pendingDomainEvent{}, fmt.Errorf("lock traffic totals: %w", err)
	}
	event := pendingDomainEvent{rawUploadedAfter: rawUploaded, rawDownloadedAfter: rawDownloaded}
	balanceAfter := int64(0)
	if command.Field == identity.ManagedUserAdjustmentUploadedBytes {
		nextRaw, okRaw := addInt64(rawUploaded, delta)
		nextCredited, okCredited := addInt64(creditedUploaded, delta)
		if !okRaw || !okCredited {
			return "", pendingDomainEvent{}, identity.ErrManagedUserAdjustmentConflict
		}
		if nextRaw < 0 || nextCredited < 0 {
			return "", pendingDomainEvent{}, identity.ErrManagedUserAdjustmentInsufficient
		}
		rawUploaded, creditedUploaded, balanceAfter = nextRaw, nextCredited, nextRaw
		event.trafficUploadedDelta = delta
		event.rawUploadedAfter = nextRaw
	} else {
		nextRaw, okRaw := addInt64(rawDownloaded, delta)
		nextCharged, okCharged := addInt64(chargedDownloaded, delta)
		if !okRaw || !okCharged {
			return "", pendingDomainEvent{}, identity.ErrManagedUserAdjustmentConflict
		}
		if nextRaw < 0 || nextCharged < 0 {
			return "", pendingDomainEvent{}, identity.ErrManagedUserAdjustmentInsufficient
		}
		rawDownloaded, chargedDownloaded, balanceAfter = nextRaw, nextCharged, nextRaw
		event.trafficDownloadedDelta = delta
		event.rawDownloadedAfter = nextRaw
	}
	if _, err := tx.Exec(ctx, `
UPDATE traffic.user_totals
SET raw_uploaded = $2,
    raw_downloaded = $3,
    credited_uploaded = $4,
    charged_downloaded = $5,
    version = version + 1,
    updated_at = $6
WHERE user_id = $1`, command.UserID, rawUploaded, rawDownloaded,
		creditedUploaded, chargedDownloaded, command.OccurredAt); err != nil {
		return "", pendingDomainEvent{}, fmt.Errorf("update traffic totals: %w", err)
	}
	return strconv.FormatInt(balanceAfter, 10), event, nil
}

func (repository *PostgresRepository) adjustMagic(ctx context.Context, tx pgx.Tx, command identity.ManagedUserAdjustmentCommand) (string, uuid.UUID, error) {
	delta, err := strconv.ParseInt(command.Delta, 10, 64)
	if err != nil || delta == 0 {
		return "", uuid.Nil, identity.ErrUserAdministrationInput
	}
	digest := adjustmentDigest(command)
	transactionID := uuid.NewSHA1(magicTransactionNamespace, []byte(command.AdjustmentID.String()))
	transaction, err := repository.economy.RecordInTransaction(ctx, tx, economy.RecordCommand{
		TransactionID:   transactionID,
		TransactionType: economy.TransactionAdjustment,
		IdempotencyKey:  adjustmentSourceReference(command.AdjustmentID),
		SourceReference: adjustmentSourceReference(command.AdjustmentID),
		PolicyRevision:  administratorAdjustmentPolicy,
		PayloadSHA256:   digest,
		OccurredAt:      command.OccurredAt,
		RecordedAt:      command.OccurredAt,
		Postings: []economy.PostingInput{
			{AccountID: economy.AdministratorAdjustmentAccountID(), Amount: -delta},
			{AccountID: command.UserID, Amount: delta},
		},
	})
	if errors.Is(err, economy.ErrInsufficientBalance) {
		return "", uuid.Nil, identity.ErrManagedUserAdjustmentInsufficient
	}
	if errors.Is(err, economy.ErrIdempotencyConflict) {
		return "", uuid.Nil, identity.ErrManagedUserAdjustmentConflict
	}
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("record magic adjustment: %w", err)
	}
	for _, posting := range transaction.Postings {
		if posting.AccountID == command.UserID {
			return strconv.FormatInt(posting.BalanceAfter, 10), transaction.ID, nil
		}
	}
	return "", uuid.Nil, errors.New("magic adjustment omitted member posting")
}

func (repository *PostgresRepository) adjustExperience(ctx context.Context, tx pgx.Tx, command identity.ManagedUserAdjustmentCommand) (string, uuid.UUID, error) {
	amount, err := progression.ParseAmount(command.Delta)
	if err != nil || amount.Sign() == 0 {
		return "", uuid.Nil, identity.ErrUserAdministrationInput
	}
	var levelPolicyVersion string
	if err := tx.QueryRow(ctx, `
SELECT policy_version
FROM progression.level_policy_revisions
WHERE effective_at <= $1
ORDER BY effective_at DESC, sequence DESC
LIMIT 1`, command.OccurredAt).Scan(&levelPolicyVersion); errors.Is(err, pgx.ErrNoRows) {
		return "", uuid.Nil, progression.ErrLevelPolicyNotFound
	} else if err != nil {
		return "", uuid.Nil, fmt.Errorf("read active level policy: %w", err)
	}
	digest := adjustmentDigest(command)
	entryID := uuid.NewSHA1(experienceEntryNamespace, []byte(command.AdjustmentID.String()))
	entry, err := repository.progression.RecordInTransaction(ctx, tx, progression.RecordCommand{
		EntryID:            entryID,
		IdempotencyKey:     adjustmentSourceReference(command.AdjustmentID),
		UserID:             command.UserID,
		EntryType:          progression.EntryAdjustment,
		Amount:             amount,
		SourceReference:    adjustmentSourceReference(command.AdjustmentID),
		SourceKind:         progression.SourceAdministratorAdjust,
		PolicyRevision:     administratorAdjustmentPolicy,
		LevelPolicyVersion: levelPolicyVersion,
		PayloadSHA256:      digest,
		OccurredAt:         command.OccurredAt,
		RecordedAt:         command.OccurredAt,
	})
	if errors.Is(err, progression.ErrInsufficientXP) {
		return "", uuid.Nil, identity.ErrManagedUserAdjustmentInsufficient
	}
	if errors.Is(err, progression.ErrIdempotencyConflict) {
		return "", uuid.Nil, identity.ErrManagedUserAdjustmentConflict
	}
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("record experience adjustment: %w", err)
	}
	return entry.BalanceAfter.String(), entry.ID, nil
}

func adjustInvitations(ctx context.Context, tx pgx.Tx, command identity.ManagedUserAdjustmentCommand) (string, pendingDomainEvent, error) {
	delta, err := strconv.ParseInt(command.Delta, 10, 32)
	if err != nil || delta == 0 {
		return "", pendingDomainEvent{}, identity.ErrUserAdministrationInput
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.invitation_accounts (user_id, remaining_invites, version, updated_at)
VALUES ($1, 0, 1, $2)
ON CONFLICT (user_id) DO NOTHING`, command.UserID, command.OccurredAt); err != nil {
		return "", pendingDomainEvent{}, fmt.Errorf("ensure invitation account: %w", err)
	}
	var current int64
	if err := tx.QueryRow(ctx, `
SELECT remaining_invites::bigint
FROM identity.invitation_accounts
WHERE user_id = $1
FOR UPDATE`, command.UserID).Scan(&current); err != nil {
		return "", pendingDomainEvent{}, fmt.Errorf("lock invitation account: %w", err)
	}
	next, ok := addInt64(current, delta)
	if !ok || next > 1_000_000 {
		return "", pendingDomainEvent{}, identity.ErrManagedUserAdjustmentConflict
	}
	if next < 0 {
		return "", pendingDomainEvent{}, identity.ErrManagedUserAdjustmentInsufficient
	}
	if _, err := tx.Exec(ctx, `
UPDATE identity.invitation_accounts
SET remaining_invites = $2, version = version + 1, updated_at = $3
WHERE user_id = $1`, command.UserID, next, command.OccurredAt); err != nil {
		return "", pendingDomainEvent{}, fmt.Errorf("update invitation account: %w", err)
	}
	return strconv.FormatInt(next, 10), pendingDomainEvent{
		invitationDelta: delta, invitationBalanceAfter: next,
	}, nil
}

func adjustDonation(ctx context.Context, tx pgx.Tx, command identity.ManagedUserAdjustmentCommand) (string, error) {
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.user_donation_totals (user_id, amount, version, updated_at)
VALUES ($1, 0, 1, $2)
ON CONFLICT (user_id) DO NOTHING`, command.UserID, command.OccurredAt); err != nil {
		return "", fmt.Errorf("ensure donation total: %w", err)
	}
	var balance string
	err := tx.QueryRow(ctx, `
UPDATE identity.user_donation_totals
SET amount = amount + $2::numeric(12, 2),
    version = version + 1,
    updated_at = $3
WHERE user_id = $1
  AND amount + $2::numeric(12, 2) BETWEEN 0 AND 9999999999.99
RETURNING amount::text`, command.UserID, command.Delta, command.OccurredAt).Scan(&balance)
	if errors.Is(err, pgx.ErrNoRows) {
		if strings.HasPrefix(command.Delta, "-") {
			return "", identity.ErrManagedUserAdjustmentInsufficient
		}
		return "", identity.ErrManagedUserAdjustmentConflict
	}
	if err != nil {
		return "", fmt.Errorf("update donation total: %w", err)
	}
	return balance, nil
}

func insertDomainEvent(ctx context.Context, tx pgx.Tx, command identity.ManagedUserAdjustmentCommand, event pendingDomainEvent) error {
	if event.trafficUploadedDelta != 0 || event.trafficDownloadedDelta != 0 {
		if _, err := tx.Exec(ctx, `
INSERT INTO traffic.user_traffic_adjustments (
    adjustment_id, user_id, uploaded_delta, downloaded_delta,
    raw_uploaded_after, raw_downloaded_after, occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)`, command.AdjustmentID, command.UserID,
			event.trafficUploadedDelta, event.trafficDownloadedDelta,
			event.rawUploadedAfter, event.rawDownloadedAfter, command.OccurredAt); err != nil {
			return fmt.Errorf("record traffic adjustment: %w", err)
		}
	}
	if event.invitationDelta != 0 {
		eventID := uuid.NewSHA1(invitationEventNamespace, []byte(command.AdjustmentID.String()))
		if _, err := tx.Exec(ctx, `
INSERT INTO identity.invitation_balance_events (
    id, user_id, invitation_id, event_kind, delta, balance_after,
    authorization_decision_id, source_reference, occurred_at, recorded_at
) VALUES ($1, $2, NULL, 'staff_adjustment', $3, $4, $5, $6, $7, $7)`,
			eventID, command.UserID, event.invitationDelta, event.invitationBalanceAfter,
			command.Authorization.ID, adjustmentSourceReference(command.AdjustmentID), command.OccurredAt); err != nil {
			return fmt.Errorf("record invitation adjustment: %w", err)
		}
	}
	return nil
}

func (repository *PostgresRepository) ListManagedUserNetworkHistory(
	ctx context.Context,
	userID uuid.UUID,
	cutoff time.Time,
	limit int,
) ([]identity.ManagedUserNetworkObservation, error) {
	if userID == uuid.Nil || cutoff.IsZero() || limit < 1 || limit > 20 {
		return nil, identity.ErrUserAdministrationInput
	}
	var exists bool
	if err := repository.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM identity.users WHERE id = $1)`, userID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("read network-history user: %w", err)
	}
	if !exists {
		return nil, identity.ErrManagedUserNotFound
	}
	rows, err := repository.pool.Query(ctx, `
SELECT
    host(observation.ip_address),
    observation.first_seen_at,
    observation.last_seen_at,
    observation.legacy_seen_count + observation.web_login_seen_count,
    (
        SELECT count(*)::bigint
        FROM identity.user_network_observations AS related
        WHERE related.ip_address = observation.ip_address
          AND related.last_seen_at >= $2
    ) AS related_user_count
FROM identity.user_network_observations AS observation
WHERE observation.user_id = $1
  AND observation.last_seen_at >= $2
ORDER BY observation.last_seen_at DESC, observation.ip_address
LIMIT $3`, userID, cutoff.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("query managed user network history: %w", err)
	}
	defer rows.Close()
	result := make([]identity.ManagedUserNetworkObservation, 0, limit)
	for rows.Next() {
		var item identity.ManagedUserNetworkObservation
		if err := rows.Scan(
			&item.Address, &item.FirstSeenAt, &item.LastSeenAt,
			&item.SeenCount, &item.RelatedUserCount,
		); err != nil {
			return nil, fmt.Errorf("scan managed user network history: %w", err)
		}
		item.FirstSeenAt = item.FirstSeenAt.UTC()
		item.LastSeenAt = item.LastSeenAt.UTC()
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish managed user network history: %w", err)
	}
	return result, nil
}

func addInt64(left, right int64) (int64, bool) {
	if right > 0 && left > math.MaxInt64-right {
		return 0, false
	}
	if right < 0 && left < math.MinInt64-right {
		return 0, false
	}
	return left + right, true
}

func adjustmentSourceReference(id uuid.UUID) string {
	return "staff-user-adjustment:" + id.String()
}

func adjustmentDigest(command identity.ManagedUserAdjustmentCommand) [sha256.Size]byte {
	return sha256.Sum256([]byte(strings.Join([]string{
		"peergo:managed-user-adjustment:v1",
		command.AdjustmentID.String(),
		command.UserID.String(),
		command.ActorID.String(),
		string(command.Field),
		command.Delta,
		command.Reason,
		strconv.FormatInt(command.ExpectedUserVersion, 10),
	}, "\x00")))
}

func canonicalNumericText(value string) string {
	value = strings.TrimSpace(value)
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(strings.TrimPrefix(value, "+"), "-")
	parts := strings.SplitN(value, ".", 2)
	integer := strings.TrimLeft(parts[0], "0")
	if integer == "" {
		integer = "0"
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = strings.TrimRight(parts[1], "0")
	}
	result := integer
	if fraction != "" {
		result += "." + fraction
	}
	if negative && result != "0" {
		result = "-" + result
	}
	return result
}

var _ identity.ManagedUserDataAdministrationRepository = (*PostgresRepository)(nil)
