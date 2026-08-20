package hnrpolicy

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/settlement/internal/timeline"
)

func TestPolicyCanonicalRoundTrip(t *testing.T) {
	t.Parallel()
	policy := enforcedPolicy("global", 1)
	encoded, err := Encode(policy)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || decoded != policy {
		t.Fatalf("decoded=%+v error=%v", decoded, err)
	}
}

func TestResolveAtUsesLatestMostSpecificRevision(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("0198f20a-6da8-4e51-9c64-111111111111")
	torrentID := int64(42)
	revisions := []Revision{
		{ID: uuid.MustParse("0198f20a-6da8-4e51-9c64-222222222222"), EffectiveAt: now.Add(-48 * time.Hour), Policy: enforcedPolicy("global", 1)},
		{ID: uuid.MustParse("0198f20a-6da8-4e51-9c64-333333333333"), Scope: timeline.Scope{TorrentID: &torrentID}, EffectiveAt: now.Add(-24 * time.Hour), Policy: enforcedPolicy("torrent", 2)},
	}
	resolved, err := ResolveAt(Context{UserID: userID, TorrentID: torrentID, TorrentControlSequence: 1, SubjectControlSequence: 1, At: now}, revisions)
	if err != nil || resolved.Policy.Rule.ID != "torrent" {
		t.Fatalf("resolved=%+v error=%v", resolved, err)
	}
}

func TestResolveAtRejectsEqualSpecificityOverlap(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("0198f20a-6da8-4e51-9c64-111111111111")
	torrentID := int64(42)
	sequence := int64(3)
	revisions := []Revision{
		{ID: uuid.MustParse("0198f20a-6da8-4e51-9c64-222222222222"), Scope: timeline.Scope{TorrentID: &torrentID}, EffectiveAt: now.Add(-time.Hour), Policy: enforcedPolicy("torrent", 1)},
		{ID: uuid.MustParse("0198f20a-6da8-4e51-9c64-333333333333"), Scope: timeline.Scope{TorrentControlSequence: &sequence}, EffectiveAt: now.Add(-time.Hour), Policy: enforcedPolicy("sequence", 1)},
	}
	_, err := ResolveAt(Context{UserID: userID, TorrentID: torrentID, TorrentControlSequence: sequence, SubjectControlSequence: 1, At: now}, revisions)
	if err == nil {
		t.Fatal("overlapping equal-specificity revisions were accepted")
	}
}

func enforcedPolicy(id string, version int64) Policy {
	return Policy{
		Rule: RuleRef{ID: id, Version: version}, Mode: ModeEnforced,
		RequiredSeedSeconds: 7 * 24 * 60 * 60, RequiredRatioBasisPoints: 10_000,
		AssessmentWindowSeconds: 10 * 24 * 60 * 60, GracePeriodSeconds: 3 * 24 * 60 * 60,
		MaxIntervalCreditSeconds: 90 * 60,
	}
}
