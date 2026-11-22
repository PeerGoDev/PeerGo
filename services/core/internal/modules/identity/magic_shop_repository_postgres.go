package identity

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/peergo/peergo/services/core/internal/generated/identitydb"
)

var ErrVIPPermanent = errors.New("VIP is already permanent")

// The helpers below run inside a magic shop purchase transaction so the magic
// debit and the delivered entitlement either commit together or not at all.

// InvitationPurchaseBlocker reports why a member could not use a purchased
// invitation right now. The remaining balance is deliberately ignored because
// the purchase itself is what adds to it.
func InvitationPurchaseBlocker(ctx context.Context, tx pgx.Tx, userID uuid.UUID, now time.Time) (InvitationEligibilityBlocker, error) {
	queries := identitydb.New(tx)
	contextRow, err := queries.GetInvitationIssuerContext(ctx, identitydb.GetInvitationIssuerContextParams{
		AsOf: invitationTimestamp(now), UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return InvitationBlockerAccountUnavailable, nil
	}
	if err != nil {
		return "", fmt.Errorf("get invitation issuer context for purchase: %w", err)
	}
	used, err := countInvitationCapUsage(ctx, tx, userID, now)
	if err != nil {
		return "", err
	}
	if used < 0 || used > math.MaxInt {
		return "", ErrInvitationInvariant
	}
	snapshot := invitationSnapshotFromContext(contextRow, int(used), 1)
	eligibility, err := invitationEligibility(snapshot, now.UTC())
	if err != nil {
		return "", err
	}
	return eligibility.Blocker, nil
}

// GrantPurchasedInvitations adds permanent invitation slots and returns the
// new remaining balance.
func GrantPurchasedInvitations(ctx context.Context, tx pgx.Tx, userID uuid.UUID, quantity int, occurredAt time.Time) (int, error) {
	if userID == uuid.Nil || quantity < 1 || quantity > 100 || occurredAt.IsZero() {
		return 0, ErrInvitationInput
	}
	var balance int
	err := tx.QueryRow(ctx, `
INSERT INTO identity.invitation_accounts (user_id, remaining_invites, version, updated_at)
VALUES ($1, $2, 1, $3)
ON CONFLICT (user_id) DO UPDATE
SET remaining_invites = identity.invitation_accounts.remaining_invites + EXCLUDED.remaining_invites,
    version = identity.invitation_accounts.version + 1,
    updated_at = EXCLUDED.updated_at
WHERE identity.invitation_accounts.remaining_invites + EXCLUDED.remaining_invites <= 1000000
RETURNING remaining_invites`, userID, quantity, occurredAt).Scan(&balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrInvitationInvariant
	}
	if err != nil {
		return 0, fmt.Errorf("grant purchased invitations: %w", err)
	}
	return balance, nil
}

// GrantTemporaryInvitationCredits stores purchased temporary invitations.
// Each credit may issue one invitation code before it expires.
func GrantTemporaryInvitationCredits(ctx context.Context, tx pgx.Tx, userID, orderID uuid.UUID, quantity int, expiresAt, occurredAt time.Time) error {
	if userID == uuid.Nil || orderID == uuid.Nil || quantity < 1 || quantity > 100 || !expiresAt.After(occurredAt) {
		return ErrInvitationInput
	}
	for index := range quantity {
		creditID := uuid.NewSHA1(orderID, []byte(fmt.Sprintf("temporary-invitation:%d", index)))
		if _, err := tx.Exec(ctx, `
INSERT INTO identity.temporary_invitation_credits (
    id, user_id, shop_order_id, expires_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $5)`, creditID, userID, orderID, expiresAt, occurredAt); err != nil {
			return fmt.Errorf("insert temporary invitation credit: %w", err)
		}
	}
	return nil
}

// TemporaryInvitationSummary counts still-usable temporary invitations.
func TemporaryInvitationSummary(ctx context.Context, querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, userID uuid.UUID, now time.Time) (int, *time.Time, error) {
	var count int64
	var nextExpiry pgtype.Timestamptz
	if err := querier.QueryRow(ctx, `
SELECT count(*)::bigint, min(expires_at)
FROM identity.temporary_invitation_credits
WHERE user_id = $1 AND invitation_id IS NULL AND expires_at > $2`, userID, now).Scan(&count, &nextExpiry); err != nil {
		return 0, nil, fmt.Errorf("summarize temporary invitations: %w", err)
	}
	return int(count), invitationOptionalTime(nextExpiry), nil
}

const MaximumVIPShopDurationDays = 3650

// ExtendVIPFromShop adds purchased VIP days on top of any remaining timed VIP
// and returns the new expiry. It writes the same projection, timeline,
// Settlement outbox and reward-benefit revision as a staff change.
func ExtendVIPFromShop(ctx context.Context, tx pgx.Tx, userID, orderID uuid.UUID, durationDays int, occurredAt time.Time) (time.Time, error) {
	if userID == uuid.Nil || orderID == uuid.Nil || durationDays < 1 || durationDays > MaximumVIPShopDurationDays || occurredAt.IsZero() {
		return time.Time{}, ErrUserAdministrationInput
	}
	queries := identitydb.New(tx)
	user, err := queries.LockManagedUserForAccountRestriction(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrManagedUserNotFound
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("lock member for VIP purchase: %w", err)
	}
	if err := validateLockedManagedUser(user); err != nil {
		return time.Time{}, err
	}
	if AccountStatus(user.Status) != AccountStatusActive {
		return time.Time{}, ErrManagedUserNotActive
	}
	current, exists, err := lockVIPState(ctx, queries, userID, occurredAt)
	if err != nil {
		return time.Time{}, err
	}
	base := occurredAt
	if current.Active {
		if current.Until == nil {
			return time.Time{}, ErrVIPPermanent
		}
		base = *current.Until
	}
	target := base.AddDate(0, 0, durationDays).UTC()
	command := ChangeVIPCommand{
		ChangeVIPInput: ChangeVIPInput{
			UserID: userID, Enabled: true, ExpectedStateVersion: current.Version,
		},
		OccurredAt: occurredAt,
	}
	stateVersion, err := writeVIPState(ctx, queries, command, exists, &target)
	if err != nil {
		return time.Time{}, err
	}
	if stateVersion != current.Version+1 {
		return time.Time{}, errors.New("VIP purchase returned an invalid state version")
	}
	transition := VIPTransitionGranted
	if current.Enabled {
		transition = VIPTransitionRenewed
	}
	transitionID := uuid.NewSHA1(orderID, []byte("vip-transition"))
	if _, err := tx.Exec(ctx, `
INSERT INTO identity.user_vip_transitions (
    id, user_id, transition, origin, reason, actor_id,
    from_enabled, from_until, to_enabled, to_until,
    from_state_version, state_version, occurred_at
) VALUES ($1, $2, $3, 'shop', $4, NULL, $5, $6, true, $7, $8, $9, $10)`,
		transitionID, userID, string(transition),
		fmt.Sprintf("魔力商店购买 %d 天 VIP（订单 %s）", durationDays, orderID.String()[:8]),
		current.Enabled, pgNullableTimestamp(current.Until), target,
		current.Version, stateVersion, occurredAt); err != nil {
		return time.Time{}, fmt.Errorf("insert shop VIP transition: %w", err)
	}
	if rows, err := insertVIPBenefitOutbox(ctx, tx, transitionID, userID, true, &target, stateVersion, occurredAt); err != nil {
		return time.Time{}, err
	} else if rows != 1 {
		return time.Time{}, errors.New("VIP benefit delivery was not enqueued")
	}
	if err := exemptActiveNewcomerAssessmentForVIP(ctx, tx, userID, transitionID, occurredAt); err != nil {
		return time.Time{}, err
	}
	rewardRevision, err := queries.InsertVIPRewardBenefitRevision(ctx, identitydb.InsertVIPRewardBenefitRevisionParams{
		UserID: userID, OccurredAt: timestamp(occurredAt),
		VipEnabled: true, VipUntil: pgNullableTimestamp(&target),
		TransitionID: transitionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, errors.New("VIP reward benefit opening is missing")
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("append shop VIP reward benefit revision: %w", err)
	}
	if rewardRevision < 2 {
		return time.Time{}, errors.New("VIP reward benefit revision is invalid")
	}
	if _, err := queries.AdvanceUserAdministrationVersion(ctx, identitydb.AdvanceUserAdministrationVersionParams{
		UpdatedAt: timestamp(occurredAt), UserID: userID,
		ExpectedVersion: user.AdministrationVersion,
	}); err != nil {
		return time.Time{}, fmt.Errorf("advance user version after VIP purchase: %w", err)
	}
	return target, nil
}

// CurrentVIPState reads the member's VIP projection without locking it.
func CurrentVIPState(ctx context.Context, tx pgx.Tx, userID uuid.UUID, asOf time.Time) (VIPState, error) {
	row, err := identitydb.New(tx).GetVIPState(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return VIPState{}, nil
	}
	if err != nil {
		return VIPState{}, fmt.Errorf("read VIP state: %w", err)
	}
	return vipStateFromValues(row.VipEnabled, row.VipUntil, row.Version, asOf)
}
