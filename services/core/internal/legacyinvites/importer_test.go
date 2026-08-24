package legacyinvites

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSourceResultPreservesInvitationBalancesAndInventory(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC)
	state := sourceState{
		ObservedAt: now,
		Balances: []balanceRow{
			{LegacyUserID: 1, Remaining: 99},
			{LegacyUserID: 2, Remaining: 0},
			{LegacyUserID: 3, Remaining: 4},
		},
		Codes: []codeRow{
			{LegacyInvitationID: 1, Claimed: true},
			{LegacyInvitationID: 2, Active: true},
		},
	}
	result := sourceResult(uuid.New(), state)
	if result.BalanceSourceRows != 3 || result.BalanceTotal != 103 ||
		result.PositiveBalanceUsers != 2 || result.InvitationSourceRows != 2 ||
		result.ClaimedInvitationRows != 1 || result.ActiveInvitationRows != 1 {
		t.Fatalf("source result = %+v", result)
	}
}

func TestValidateCodeAcceptsPtYesTokenAndRequiresConsistentClaim(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC)
	createdAt := now.Add(-time.Hour)
	row := codeRow{
		LegacyInvitationID: 4, LegacyInviterID: 1, Role: "user",
		CreatedAt: createdAt, ValidUntil: now.Add(time.Hour),
	}
	if err := validateCode(row, strings.Repeat("a", 64), now); err != nil {
		t.Fatalf("valid PtYes invitation rejected: %v", err)
	}
	invitee := int64(8)
	row.LegacyInviteeID = &invitee
	if err := validateCode(row, strings.Repeat("a", 64), now); err == nil {
		t.Fatal("unclaimed invitation with an invitee was accepted")
	}
}

func TestBalanceFingerprintChangesWithRemainingInvites(t *testing.T) {
	t.Parallel()
	updatedAt := time.Date(2026, 8, 24, 8, 0, 0, 0, time.UTC)
	left := balanceFingerprint(balanceRow{LegacyUserID: 1, Remaining: 99, SourceUpdatedAt: updatedAt})
	right := balanceFingerprint(balanceRow{LegacyUserID: 1, Remaining: 98, SourceUpdatedAt: updatedAt})
	if left == right {
		t.Fatal("balance fingerprint ignored the remaining invitation count")
	}
}
