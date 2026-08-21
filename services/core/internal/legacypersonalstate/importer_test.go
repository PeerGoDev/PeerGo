package legacypersonalstate

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCanonicalBookmarkRowsKeepsOldestSourceRow(t *testing.T) {
	t.Parallel()
	rows := []bookmarkRow{
		{ID: 9, LegacyUserID: 4, LegacyTorrentID: 7},
		{ID: 3, LegacyUserID: 4, LegacyTorrentID: 7},
		{ID: 8, LegacyUserID: 4, LegacyTorrentID: 8},
	}
	canonical := canonicalBookmarkRows(rows)
	if len(canonical) != 2 || canonical[[2]int64{4, 7}] != 3 || canonical[[2]int64{4, 8}] != 8 {
		t.Fatalf("canonical bookmark rows = %+v", canonical)
	}
}

func TestValidateRelationshipGraphRejectsCycleAndConflictingParent(t *testing.T) {
	t.Parallel()
	if err := validateRelationshipGraph([]relationshipRow{
		{ID: 1, LegacyInviterID: 1, LegacyInviteeID: 2},
		{ID: 2, LegacyInviterID: 2, LegacyInviteeID: 3},
	}); err != nil {
		t.Fatalf("valid graph rejected: %v", err)
	}
	if err := validateRelationshipGraph([]relationshipRow{
		{ID: 1, LegacyInviterID: 1, LegacyInviteeID: 2},
		{ID: 2, LegacyInviterID: 2, LegacyInviteeID: 1},
	}); err == nil {
		t.Fatal("cycle was accepted")
	}
	if err := validateRelationshipGraph([]relationshipRow{
		{ID: 1, LegacyInviterID: 1, LegacyInviteeID: 3},
		{ID: 2, LegacyInviterID: 2, LegacyInviteeID: 3},
	}); err == nil {
		t.Fatal("conflicting inviter was accepted")
	}
}

func TestSourceResultSeparatesHistoricalRewardKinds(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	state := sourceState{
		Bookmarks: []bookmarkRow{
			{ID: 1, LegacyUserID: 1, LegacyTorrentID: 7},
			{ID: 2, LegacyUserID: 1, LegacyTorrentID: 7},
		},
		Relationships: []relationshipRow{{ID: 1, LegacyInviterID: 1, LegacyInviteeID: 2}},
		Rewards: []rewardRow{
			{LegacyUserID: 1, Kind: "harem", SourceRows: 11, FirstAt: now, LastAt: now},
			{LegacyUserID: 1, Kind: "invite_reward", SourceRows: 2, FirstAt: now, LastAt: now},
		},
	}
	result := sourceResult(uuid.New(), state)
	if result.BookmarkSourceRows != 2 || result.BookmarkDistinctPairs != 1 ||
		result.InvitationSourceRows != 1 || result.HaremRewardSourceRows != 11 ||
		result.HaremRewardUsers != 1 || result.InvitationRewardSourceRows != 2 ||
		result.InvitationRewardUsers != 1 {
		t.Fatalf("source result = %+v", result)
	}
}
