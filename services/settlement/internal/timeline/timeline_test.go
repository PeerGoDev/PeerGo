package timeline

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/peergo/peergo/services/settlement/internal/policy"
)

func TestResolveIntervalSplitsAtImmutablePolicyBoundary(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	context := testContext(start, start.Add(10*time.Minute))
	revisions := []Revision{
		testRevision("0198f20a-6da8-7e51-9c64-111111111111", Scope{}, start.Add(-time.Hour), snapshot(t, "baseline", policy.PromotionNormal)),
		testRevision("0198f20a-6da8-7e51-9c64-222222222222", Scope{}, start.Add(4*time.Minute), snapshot(t, "free", policy.PromotionFree)),
	}
	slices, err := ResolveInterval(context, revisions)
	if err != nil || len(slices) != 2 {
		t.Fatalf("ResolveInterval() = %+v, %v", slices, err)
	}
	if !slices[0].EndsAt.Equal(start.Add(4*time.Minute)) || slices[0].Snapshot.Revision.ID != "baseline" || slices[1].Snapshot.Revision.ID != "free" {
		t.Fatalf("slices = %+v", slices)
	}
}

func TestResolveIntervalUsesControlSequenceSpecificRevision(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	context := testContext(start, start.Add(time.Minute))
	sequence := context.TorrentControlSequence
	revisions := []Revision{
		testRevision("0198f20a-6da8-7e51-9c64-111111111111", Scope{}, start.Add(-time.Hour), snapshot(t, "global", policy.PromotionNormal)),
		testRevision("0198f20a-6da8-7e51-9c64-222222222222", Scope{TorrentControlSequence: &sequence}, start.Add(-time.Hour), snapshot(t, "control-7-free", policy.PromotionFree)),
	}
	slices, err := ResolveInterval(context, revisions)
	if err != nil || len(slices) != 1 || slices[0].Snapshot.Revision.ID != "control-7-free" {
		t.Fatalf("ResolveInterval() = %+v, %v", slices, err)
	}
}

func TestResolveIntervalRefusesMissingOrAmbiguousCoverage(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	context := testContext(start, start.Add(time.Minute))
	if _, err := ResolveInterval(context, nil); !errors.Is(err, ErrNoCoverage) {
		t.Fatalf("ResolveInterval(no coverage) error = %v", err)
	}
	torrentID := context.TorrentID
	userID := context.UserID
	revisions := []Revision{
		testRevision("0198f20a-6da8-7e51-9c64-111111111111", Scope{TorrentID: &torrentID}, start.Add(-time.Hour), snapshot(t, "torrent", policy.PromotionNormal)),
		testRevision("0198f20a-6da8-7e51-9c64-222222222222", Scope{UserID: &userID}, start.Add(-time.Hour), snapshot(t, "user", policy.PromotionFree)),
	}
	if _, err := ResolveInterval(context, revisions); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("ResolveInterval(ambiguous) error = %v", err)
	}
}

func testContext(start, end time.Time) IntervalContext {
	return IntervalContext{
		UserID: uuid.MustParse("0198f20a-6da8-4e51-9c64-111111111111"), TorrentID: 11,
		TorrentControlSequence: 7, SubjectControlSequence: 9, StartsAt: start, EndsAt: end,
	}
}

func testRevision(id string, scope Scope, effectiveAt time.Time, snapshot policy.Snapshot) Revision {
	return Revision{ID: uuid.MustParse(id), Scope: scope, EffectiveAt: effectiveAt, Snapshot: snapshot}
}

func snapshot(t *testing.T, id string, promotion policy.PromotionType) policy.Snapshot {
	t.Helper()
	start := time.Date(2026, time.August, 9, 10, 0, 0, 0, time.UTC)
	rule := policy.PromotionRule{
		Rule:  policy.RuleRef{Source: policy.SourceTorrentPromotion, ID: "promotion-" + id, Version: 1},
		Scope: policy.ScopeTorrent, Promotion: promotion, Window: policy.Window{StartsAt: start},
	}
	if promotion == policy.PromotionNormal {
		resolved, err := policy.ResolvePromotion(policy.ProfilePeerGoV1, start, nil)
		if err != nil {
			t.Fatal(err)
		}
		return policy.Snapshot{Revision: policy.RuleRef{Source: policy.SourcePolicyRevision, ID: id, Version: 1}, Profile: policy.ProfilePeerGoV1, Promotion: resolved}
	}
	resolved, err := policy.ResolvePromotion(policy.ProfilePeerGoV1, start, []policy.PromotionRule{rule})
	if err != nil {
		t.Fatal(err)
	}
	return policy.Snapshot{Revision: policy.RuleRef{Source: policy.SourcePolicyRevision, ID: id, Version: 1}, Profile: policy.ProfilePeerGoV1, Promotion: resolved}
}
