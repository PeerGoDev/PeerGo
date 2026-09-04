package accountadmin_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/accountadmin"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestPostgresManagedUserAdjustmentsKeepDomainLedgersAndReceiptConsistent(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_CORE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatalf("RequireCurrentMigration() error = %v", err)
	}

	actorID := insertManagedUser(t, ctx, pool, "actor")
	targetID := insertManagedUser(t, ctx, pool, "target")
	repository, err := accountadmin.NewPostgresRepository(pool)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	decision := authz.Decision{ID: uuid.New(), Allow: true}
	version := int64(1)
	commands := []identity.ManagedUserAdjustmentCommand{
		managedAdjustment(targetID, actorID, identity.ManagedUserAdjustmentUploadedBytes, "1073741824", version, now, decision),
		managedAdjustment(targetID, actorID, identity.ManagedUserAdjustmentDownloadedBytes, "2147483648", version+1, now.Add(time.Second), decision),
		managedAdjustment(targetID, actorID, identity.ManagedUserAdjustmentMagicBalance, "100", version+2, now.Add(2*time.Second), decision),
		managedAdjustment(targetID, actorID, identity.ManagedUserAdjustmentExperience, "1200.5", version+3, now.Add(3*time.Second), decision),
		managedAdjustment(targetID, actorID, identity.ManagedUserAdjustmentRemainingInvites, "3", version+4, now.Add(4*time.Second), decision),
		managedAdjustment(targetID, actorID, identity.ManagedUserAdjustmentDonationAmount, "42.50", version+5, now.Add(5*time.Second), decision),
	}
	for _, command := range commands {
		if err := repository.AdjustManagedUser(ctx, command); err != nil {
			t.Fatalf("AdjustManagedUser(%s) error = %v", command.Field, err)
		}
	}
	if err := repository.AdjustManagedUser(ctx, commands[len(commands)-1]); err != nil {
		t.Fatalf("AdjustManagedUser(replay) error = %v", err)
	}
	conflict := commands[len(commands)-1]
	conflict.Reason = "different replay payload"
	if err := repository.AdjustManagedUser(ctx, conflict); !errors.Is(err, identity.ErrManagedUserAdjustmentConflict) {
		t.Fatalf("AdjustManagedUser(conflict) error = %v", err)
	}

	var userVersion, uploaded, downloaded, magic int64
	var experience, donation string
	var invites int
	var receiptCount, trafficAdjustmentCount, invitationAdjustmentCount int64
	err = pool.QueryRow(ctx, `
SELECT
    user_account.administration_version,
    traffic.raw_uploaded,
    traffic.raw_downloaded,
    magic_account.balance,
    progress.experience::text,
    invitation.remaining_invites,
    donation.amount::text,
    (SELECT count(*) FROM identity.managed_user_adjustment_events WHERE user_id = user_account.id),
    (SELECT count(*) FROM traffic.user_traffic_adjustments WHERE user_id = user_account.id),
    (SELECT count(*) FROM identity.invitation_balance_events WHERE user_id = user_account.id AND event_kind = 'staff_adjustment')
FROM identity.users AS user_account
JOIN traffic.user_totals AS traffic ON traffic.user_id = user_account.id
JOIN economy.magic_accounts AS magic_account ON magic_account.id = user_account.id
JOIN progression.user_progress AS progress ON progress.user_id = user_account.id
JOIN identity.invitation_accounts AS invitation ON invitation.user_id = user_account.id
JOIN identity.user_donation_totals AS donation ON donation.user_id = user_account.id
WHERE user_account.id = $1`, targetID).Scan(
		&userVersion, &uploaded, &downloaded, &magic, &experience, &invites, &donation,
		&receiptCount, &trafficAdjustmentCount, &invitationAdjustmentCount,
	)
	if err != nil {
		t.Fatalf("read managed adjustment projections: %v", err)
	}
	if userVersion != 7 || uploaded != 1_073_741_824 || downloaded != 2_147_483_648 ||
		magic != 100 || experience != "1200.50000000000000000000" || invites != 3 ||
		donation != "42.50" || receiptCount != 6 || trafficAdjustmentCount != 2 || invitationAdjustmentCount != 1 {
		t.Fatalf("projections: version=%d upload=%d download=%d magic=%d xp=%s invites=%d donation=%s receipts=%d traffic=%d invitations=%d",
			userVersion, uploaded, downloaded, magic, experience, invites, donation,
			receiptCount, trafficAdjustmentCount, invitationAdjustmentCount)
	}
}

func insertManagedUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	username := fmt.Sprintf("account-admin-it-%s-%s", suffix, userID.String()[:8])
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (id, credential_ref, username, display_name, status)
VALUES ($1, $2, $3, $3, 'active')`, userID, uuid.New(), username); err != nil {
		t.Fatalf("insert managed user: %v", err)
	}
	return userID
}

func managedAdjustment(
	userID, actorID uuid.UUID,
	field identity.ManagedUserAdjustmentField,
	delta string,
	version int64,
	occurredAt time.Time,
	decision authz.Decision,
) identity.ManagedUserAdjustmentCommand {
	return identity.ManagedUserAdjustmentCommand{
		AdjustmentID: uuid.New(), UserID: userID, ActorID: actorID,
		Field: field, Delta: delta, Reason: "integration managed-user adjustment",
		ExpectedUserVersion: version, OccurredAt: occurredAt, Authorization: decision,
	}
}
