package swarmprojection_test

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/contracts/go/settlementseedingv1"
	"github.com/peergo/peergo/contracts/go/trackerswarmv1"
	"github.com/peergo/peergo/services/core/internal/modules/authz"
	"github.com/peergo/peergo/services/core/internal/modules/catalog"
	"github.com/peergo/peergo/services/core/internal/modules/economy/reseedreward"
	"github.com/peergo/peergo/services/core/internal/modules/economy/seedingreward"
	"github.com/peergo/peergo/services/core/internal/modules/notifications"
	"github.com/peergo/peergo/services/core/internal/modules/swarmprojection"
	"github.com/peergo/peergo/services/core/internal/modules/torrents"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

// Use a fresh disposable database separate from other snapshot tests, since
// exercising the packed-evidence path requires a synthetic historical clock.
func TestIntegrationReseedRewardOncePerVerifiedRecovery(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_RESEED_REWARD_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_RESEED_REWARD_DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := platformpostgres.RequireCurrentMigration(ctx, pool); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(time.Hour).Add(-100 * time.Hour)
	current := start
	clock := func() time.Time { return current }
	projection, err := swarmprojection.NewPostgresRepository(pool, clock, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := seedingreward.NewPostgresEvidenceRepository(pool, clock, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rewards, err := reseedreward.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := rewards.ReadSettings(ctx)
	if err != nil || defaults.Enabled || defaults.RequiredHours != 24 || defaults.MagicAmount != 1000 {
		t.Fatalf("default settings: %+v %v", defaults, err)
	}
	first := insertPublishedSwarmFixture(t, ctx, pool, "revive-first", start, 0, 0, 0)
	second := insertPublishedSwarmFixture(t, ctx, pool, "revive-second", start, 0, 0, 0)
	requester := newReseedTestRepository(t, pool)
	catalogService := catalog.NewService(catalog.NewPostgresRepository(pool), clock)
	inbox, err := notifications.NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	swarmOffer := func() *reseedreward.Offer {
		t.Helper()
		result, err := catalogService.GetTorrentSwarm(ctx, first.torrentID)
		if err != nil {
			t.Fatal(err)
		}
		return result.ReseedReward
	}
	noticeOffer := func(userID uuid.UUID, kind notifications.Kind) *reseedreward.Offer {
		t.Helper()
		result, err := inbox.List(ctx, userID, notifications.ListQuery{Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		for _, notice := range result.Items {
			if notice.Kind == kind && notice.TorrentReseed.TorrentID == torrents.TorrentID(first.torrentID) {
				return notice.TorrentReseed.Reward
			}
		}
		return nil
	}
	if offer := swarmOffer(); offer != nil {
		t.Fatalf("disabled reward was advertised: %+v", offer)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO economy.reseed_reward_policies (revision, enabled, required_hours, magic_amount, issued_at)
VALUES (2, true, 1, 125, $1)`, start); err != nil {
		t.Fatal(err)
	}
	sequence := int64(0)
	applySwarm := func(at time.Time, seeders int32) []byte {
		t.Helper()
		current = at
		sequence++
		chunk := newSnapshotChunk(t, mustUUIDV7(t), mustUUIDV7(t), "reseed-reward-test", 1, sequence, at, 0, 1, []trackerswarmv1.Entry{{
			InfoHashV1: hex.EncodeToString(first.infoHash), Seeders: seeders,
		}})
		payload := encodeSnapshot(t, chunk)
		if result, err := projection.ApplySnapshot(ctx, payload, at); err != nil || !result.Applied {
			t.Fatalf("apply swarm snapshot (%v, %d): %+v %v", at, seeders, result, err)
		}
		return payload
	}
	applySwarm(start, 0)
	assertReseedRewardOffer(t, swarmOffer(), "upcoming", 1, 125)
	if err := requester.RequestTorrentReseed(ctx, torrents.TorrentID(first.torrentID), second.userID, current); err != nil {
		t.Fatal(err)
	}
	directory, err := catalogService.ListOpenTorrentReseedRequests(ctx, nil, nil, true)
	if err != nil || directory.Total != 1 || len(directory.Items) != 1 {
		t.Fatalf("read reward in pending directory: %+v, %v", directory, err)
	}
	assertReseedRewardOffer(t, directory.Items[0].Reward, "upcoming", 1, 125)
	assertReseedRewardOffer(t, noticeOffer(first.userID, notifications.KindTorrentReseedRequested), "upcoming", 1, 125)
	current = start.Add(6 * time.Minute)
	if offer := swarmOffer(); offer != nil {
		t.Fatalf("stale zero-seeder snapshot advertised a reward: %+v", offer)
	}
	applySwarm(start.Add(15*time.Minute), 1)
	if _, err := pool.Exec(ctx, `
INSERT INTO economy.reseed_reward_policies (revision, enabled, required_hours, magic_amount, issued_at)
VALUES (3, true, 2, 500, $1)`, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertReseedRewardOffer(t, swarmOffer(), "active", 1, 125)
	assertReseedRewardOffer(t, noticeOffer(first.userID, notifications.KindTorrentReseedRequested), "active", 1, 125)
	assertReseedRewardOffer(t, noticeOffer(second.userID, notifications.KindTorrentReseedRestored), "active", 1, 125)
	firstItem := settlementseedingv1.Item{UserID: first.userID.String(), TorrentID: first.torrentID, ActiveSeconds: 3600, SnapshotSeeders: 1, EvidenceSHA256: strings.Repeat("a", 64)}
	secondItem := firstItem
	secondItem.UserID = second.userID.String()
	firstPayload := applyReseedRewardEvidence(t, ctx, evidence, start, firstItem)
	if _, err := pool.Exec(ctx, `UPDATE economy.seeding_reward_evidence_packing_control SET enabled = true`); err != nil {
		t.Fatal(err)
	}
	var packed int64
	if err := pool.QueryRow(ctx, `SELECT economy.seeding_reward_evidence_pack_batch(1)`).Scan(&packed); err != nil || packed != 1 {
		t.Fatalf("pack first hour: %d %v", packed, err)
	}
	if processed, err := rewards.SettleNext(ctx, start.Add(time.Hour)); err != nil || processed {
		t.Fatalf("settlement before swarm reached the hour end: %v %v", processed, err)
	}
	applySwarm(start.Add(time.Hour), 1)
	assertReseedRewardSettlement(t, ctx, rewards, current)
	var activeSeconds int64
	if err := pool.QueryRow(ctx, `SELECT active_seconds FROM economy.reseed_reward_progress WHERE user_id = $1`, first.userID).Scan(&activeSeconds); err != nil || activeSeconds != 2700 {
		t.Fatalf("only in-round time may count: %d %v", activeSeconds, err)
	}
	assertReseedRewardCount(t, ctx, pool, 0)

	firstItem.ActiveSeconds = 1200
	secondStart := start.Add(time.Hour)
	secondPayload := applyReseedRewardEvidence(t, ctx, evidence, secondStart, firstItem, secondItem)
	applySwarm(secondStart.Add(time.Hour), 0)
	assertReseedRewardOffer(t, swarmOffer(), "upcoming", 2, 500)
	assertReseedRewardOffer(t, noticeOffer(second.userID, notifications.KindTorrentReseedRestored), "ended", 1, 125)
	if _, err := pool.Exec(ctx, `ALTER TABLE community.torrent_reseed_notifications
ADD CONSTRAINT test_abort_reward_notice CHECK (kind <> 'torrent_reseed_rewarded')`); err != nil {
		t.Fatal(err)
	}
	if processed, err := rewards.SettleNext(ctx, current); err == nil || processed {
		t.Fatalf("settlement should roll back with the notification: %v %v", processed, err)
	}
	assertReseedRewardCount(t, ctx, pool, 0)
	if _, err := pool.Exec(ctx, `ALTER TABLE community.torrent_reseed_notifications DROP CONSTRAINT test_abort_reward_notice`); err != nil {
		t.Fatal(err)
	}
	assertReseedRewardSettlement(t, ctx, rewards, current)
	assertReseedRewardOffer(t, noticeOffer(first.userID, notifications.KindTorrentReseedRewarded), "awarded", 1, 125)
	assertReseedRecipients(t, ctx, pool, first.torrentID, notifications.KindTorrentReseedRewarded, first.userID)
	var winner uuid.UUID
	var policyRevision, balance int64
	if err := pool.QueryRow(ctx, `
SELECT award.user_id, round.policy_revision, award.active_seconds, posting.amount
FROM economy.reseed_reward_awards AS award
JOIN economy.reseed_reward_rounds AS round USING (request_id)
JOIN economy.magic_postings AS posting ON posting.transaction_id = award.magic_transaction_id AND posting.account_id = award.user_id`).Scan(
		&winner, &policyRevision, &activeSeconds, &balance); err != nil || winner != first.userID || policyRevision != 2 || activeSeconds != 3900 || balance != 125 {
		t.Fatalf("winner=%v policy=%d verified=%d reward=%d err=%v", winner, policyRevision, activeSeconds, balance, err)
	}
	for _, payload := range [][]byte{firstPayload, secondPayload} {
		if result, err := evidence.ApplyEvidence(ctx, payload, current); err != nil || !result.Duplicate {
			t.Fatalf("replayed evidence: %+v %v", result, err)
		}
	}
	if processed, err := rewards.SettleNext(ctx, current); err != nil || processed {
		t.Fatalf("settlement after replay: %v %v", processed, err)
	}
	assertReseedRewardCount(t, ctx, pool, 1)
	assertReseedRecipients(t, ctx, pool, first.torrentID, notifications.KindTorrentReseedRewarded, first.userID)

	if err := requester.RequestTorrentReseed(ctx, torrents.TorrentID(first.torrentID), second.userID, current); err != nil {
		t.Fatal(err)
	}
	thirdStart := current
	applySwarm(thirdStart.Add(20*time.Minute), 1)
	applyReseedRewardEvidence(t, ctx, evidence, thirdStart, secondItem)
	applySwarm(thirdStart.Add(40*time.Minute), 0)
	applySwarm(thirdStart.Add(time.Hour), 0)
	assertReseedRewardSettlement(t, ctx, rewards, current)
	if err := pool.QueryRow(ctx, `
SELECT round.policy_revision, progress.active_seconds
FROM economy.reseed_reward_rounds AS round
JOIN economy.reseed_reward_progress AS progress USING (request_id)
WHERE round.started_at = $1 AND progress.user_id = $2`, thirdStart.Add(20*time.Minute), second.userID).Scan(&policyRevision, &activeSeconds); err != nil || policyRevision != 3 || activeSeconds != 1200 {
		t.Fatalf("new round proof and policy: verified=%d policy=%d err=%v", activeSeconds, policyRevision, err)
	}
	assertReseedRewardCount(t, ctx, pool, 1)

	if err := requester.RequestTorrentReseed(ctx, torrents.TorrentID(first.torrentID), second.userID, current); err != nil {
		t.Fatal(err)
	}
	nextStart := current.Add(time.Hour)
	applySwarm(nextStart, 1)
	for offset := range 2 {
		hour := nextStart.Add(time.Duration(offset) * time.Hour)
		applyReseedRewardEvidence(t, ctx, evidence, hour, secondItem)
		applySwarm(hour.Add(time.Hour), 1)
		assertReseedRewardSettlement(t, ctx, rewards, current)
		assertReseedRewardCount(t, ctx, pool, 1+offset)
	}
	if err := pool.QueryRow(ctx, `
SELECT posting.amount
FROM economy.reseed_reward_awards AS award
JOIN economy.magic_postings AS posting ON posting.transaction_id = award.magic_transaction_id AND posting.account_id = award.user_id
WHERE award.user_id = $1`, second.userID).Scan(&balance); err != nil || balance != 500 {
		t.Fatalf("new round reward=%d err=%v", balance, err)
	}
	assertReseedRewardOffer(t, swarmOffer(), "awarded", 2, 500)
	assertReseedRewardOffer(t, noticeOffer(second.userID, notifications.KindTorrentReseedRewarded), "awarded", 2, 500)
	assertReseedRecipients(t, ctx, pool, first.torrentID, notifications.KindTorrentReseedRewarded, first.userID, second.userID)

	administration, err := reseedreward.NewAdministration(rewards, reseedRewardTestAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	actor := authz.StaffActor{Subject: authz.Subject{ID: first.userID, Status: authz.SubjectActive}}
	input := reseedreward.UpdateInput{ExpectedRevision: 3, Enabled: true, RequiredHours: 12, MagicAmount: 750}
	settings, err := administration.UpdateSettings(ctx, actor, input)
	if err != nil || settings.Revision != 4 || settings.RequiredHours != 12 || settings.MagicAmount != 750 {
		t.Fatalf("save staff settings: %+v %v", settings, err)
	}
	if _, err := administration.UpdateSettings(ctx, actor, input); !errors.Is(err, reseedreward.ErrConflict) {
		t.Fatalf("overwrite with old settings revision: %v", err)
	}
}

func assertReseedRewardOffer(t *testing.T, offer *reseedreward.Offer, status string, hours int32, amount int64) {
	t.Helper()
	want := reseedreward.Offer{Status: status, RequiredHours: hours, MagicAmount: amount}
	if offer == nil || *offer != want {
		t.Fatalf("reward terms: %+v, want %+v", offer, want)
	}
}

type reseedRewardTestAuthorizer struct{}

func (reseedRewardTestAuthorizer) Authorize(_ context.Context, request authz.Request) (authz.Decision, error) {
	if request.Action != authz.ActionEconomySeedingRewardPolicyIssue || request.CredentialAudience != authz.AudienceStaffSession {
		return authz.Decision{}, authz.ErrForbidden
	}
	return authz.Decision{ID: uuid.New(), Allow: true, GrantID: uuid.New(), GrantVersion: 1,
		MandateID: uuid.New(), RoleID: "site_admin", EffectiveUntil: request.Context.Now.Add(time.Hour)}, nil
}

func applyReseedRewardEvidence(t *testing.T, ctx context.Context, projector *seedingreward.PostgresEvidenceRepository, hour time.Time, items ...settlementseedingv1.Item) []byte {
	t.Helper()
	slices.SortFunc(items, func(left, right settlementseedingv1.Item) int { return strings.Compare(left.UserID, right.UserID) })
	end := hour.Add(time.Hour)
	event := settlementseedingv1.Event{
		SchemaVersion: settlementseedingv1.SchemaVersion, EventID: mustUUIDV7(t).String(),
		WindowStart: hour, WindowEnd: end, BuiltAt: end, SnapshotObservedAt: end,
		WindowEvidenceSHA256: strings.Repeat("a", 64), SnapshotID: mustUUIDV7(t).String(), SnapshotSequence: 1,
		ItemCount: int32(len(items)), ChunkCount: 1, Items: items,
	}
	digest, err := settlementseedingv1.ProjectionDigest(event, items)
	if err != nil {
		t.Fatal(err)
	}
	event.ProjectionSHA256 = settlementseedingv1.DigestHex(digest)
	payload, err := settlementseedingv1.Encode(event)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := projector.ApplyEvidence(ctx, payload, end); err != nil || !result.Complete {
		t.Fatalf("apply evidence: %+v %v", result, err)
	}
	return payload
}

func assertReseedRewardSettlement(t *testing.T, ctx context.Context, rewards *reseedreward.PostgresRepository, now time.Time) {
	t.Helper()
	if settled, err := rewards.SettleNext(ctx, now); err != nil || !settled {
		t.Fatalf("settle pending reseed reward evidence: %v %v", settled, err)
	}
}

func assertReseedRewardCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want int) {
	t.Helper()
	var awards, transactions int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM economy.reseed_reward_awards),
    (SELECT count(*) FROM economy.magic_transactions WHERE transaction_type = 'reseed_reward')`).Scan(&awards, &transactions); err != nil || awards != want || transactions != want {
		t.Fatalf("awards=%d ledger transactions=%d want=%d err=%v", awards, transactions, want, err)
	}
}
