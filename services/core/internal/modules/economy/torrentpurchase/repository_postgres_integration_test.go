package torrentpurchase

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/peergo/peergo/services/core/internal/modules/economy"
	platformpostgres "github.com/peergo/peergo/services/core/internal/platform/postgres"
)

func TestPostgresPurchaseAllowsMultipleBuyersForOneSellerAndTorrent(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("PEERGO_TEST_CORE_DATABASE_URL"))
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

	now := time.Now().UTC().Truncate(time.Microsecond)
	sellerID := insertPurchaseIntegrationUser(t, ctx, pool, "seller", now)
	firstBuyerID := insertPurchaseIntegrationUser(t, ctx, pool, "buyer-a", now)
	secondBuyerID := insertPurchaseIntegrationUser(t, ctx, pool, "buyer-b", now)
	categoryID := "purchase-it-" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, `
INSERT INTO catalog.categories (id, name, display_order, enabled, created_at, updated_at)
VALUES ($1, '种子购买集成测试', 100000 + abs(hashtext($1)) % 900000, true, $2, $2)`, categoryID, now); err != nil {
		t.Fatalf("insert integration category: %v", err)
	}

	objectID := uuid.New()
	objectDigest := sha256.Sum256([]byte("purchase-object:" + objectID.String()))
	infoDigest := sha256.Sum256([]byte("purchase-info:" + objectID.String()))
	if _, err := pool.Exec(ctx, `
INSERT INTO torrents.torrent_objects (
    id, content_sha256, byte_length, parser_version, validation_profile,
    compatibility_flags, info_offset, info_length, created_at
) VALUES ($1, $2, 256, 'integration-v1', 'strict_upload', ARRAY[]::text[], 0, 128, $3)`, objectID, objectDigest[:], now); err != nil {
		t.Fatalf("insert integration torrent object: %v", err)
	}
	var torrentID int64
	if err := pool.QueryRow(ctx, `
INSERT INTO torrents.torrents (
    uploader_id, category_id, object_id, info_hash_v1,
    content_name, title, subtitle, total_size_bytes, payload_size_bytes,
    file_count, padding_file_count, piece_length_bytes, piece_count,
    state, version, submitted_at, published_at, state_changed_at, updated_at,
    purchase_price
) VALUES (
    $1, $2, $3, $4,
    'purchase-integration.bin', 'Repeated Seller Income Integration', '', 4096, 4096,
    1, 0, 16384, 1,
    'published', 2, $5, $5, $5, $5,
    100
)
RETURNING id`, sellerID, categoryID, objectID, infoDigest[:20], now).Scan(&torrentID); err != nil {
		t.Fatalf("insert integration torrent: %v", err)
	}

	economyRepository, err := economy.NewPostgresRepository(pool)
	if err != nil {
		t.Fatalf("NewPostgresRepository(economy) error = %v", err)
	}
	creditPurchaseIntegrationBuyer(t, ctx, economyRepository, firstBuyerID, now)
	creditPurchaseIntegrationBuyer(t, ctx, economyRepository, secondBuyerID, now.Add(time.Second))

	repository, err := NewPostgresRepository(pool)
	if err != nil {
		t.Fatalf("NewPostgresRepository(purchase) error = %v", err)
	}
	firstCommand := PurchaseCommand{RequestID: uuid.New(), UserID: firstBuyerID, TorrentID: torrentID, Now: now.Add(2 * time.Second)}
	first, err := repository.Purchase(ctx, firstCommand)
	if err != nil {
		t.Fatalf("Purchase(first buyer) error = %v", err)
	}
	secondCommand := PurchaseCommand{RequestID: uuid.New(), UserID: secondBuyerID, TorrentID: torrentID, Now: now.Add(3 * time.Second)}
	second, err := repository.Purchase(ctx, secondCommand)
	if err != nil {
		t.Fatalf("Purchase(second buyer) error = %v", err)
	}
	if first.BalanceAfter != 900 || second.BalanceAfter != 900 || first.SellerIncome != 90 || second.SellerIncome != 90 {
		t.Fatalf("purchase receipts first=%+v second=%+v", first, second)
	}
	replayed, err := repository.Purchase(ctx, firstCommand)
	if err != nil || !replayed.Replayed || replayed.EntitlementID != first.EntitlementID {
		t.Fatalf("Purchase(first replay) = %+v, %v", replayed, err)
	}

	var sellerBalance, entitlementCount, sellerIncomeEntries, distinctSources int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM economy.magic_accounts WHERE user_id = $1`, sellerID).Scan(&sellerBalance); err != nil {
		t.Fatalf("read seller balance: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*)::bigint
FROM economy.torrent_purchase_entitlements
WHERE torrent_id = $1 AND source_kind = 'live_purchase'`, torrentID).Scan(&entitlementCount); err != nil {
		t.Fatalf("count live entitlements: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*)::bigint, count(DISTINCT source_reference)::bigint
FROM economy.magic_ledger_entries
WHERE user_id = $1
  AND entry_type = 'earn'
  AND source_reference LIKE $2`, sellerID, "torrent:"+strconv.FormatInt(torrentID, 10)+":purchase:%").Scan(&sellerIncomeEntries, &distinctSources); err != nil {
		t.Fatalf("count seller income statements: %v", err)
	}
	if sellerBalance != 180 || entitlementCount != 2 || sellerIncomeEntries != 2 || distinctSources != 2 {
		t.Fatalf("seller balance=%d entitlements=%d income_entries=%d distinct_sources=%d", sellerBalance, entitlementCount, sellerIncomeEntries, distinctSources)
	}
}

func insertPurchaseIntegrationUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, role string, now time.Time) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	username := fmt.Sprintf("purchase-it-%s-%s", role, userID.String()[:8])
	if _, err := pool.Exec(ctx, `
INSERT INTO identity.users (
    id, credential_ref, username, display_name, status, created_at, updated_at
) VALUES ($1, $2, $3, $3, 'active', $4, $4)`, userID, uuid.New(), username, now); err != nil {
		t.Fatalf("insert %s integration user: %v", role, err)
	}
	return userID
}

func creditPurchaseIntegrationBuyer(t *testing.T, ctx context.Context, repository *economy.PostgresRepository, buyerID uuid.UUID, occurredAt time.Time) {
	t.Helper()
	transactionID := uuid.New()
	reference := "purchase-it-credit:" + strings.ReplaceAll(transactionID.String(), "-", "")
	digest := sha256.Sum256([]byte(reference))
	if _, err := repository.Record(ctx, economy.RecordCommand{
		TransactionID: transactionID, TransactionType: economy.TransactionActivityReward,
		IdempotencyKey: reference, SourceReference: reference,
		PolicyRevision: "purchase-integration-v1", PayloadSHA256: digest,
		OccurredAt: occurredAt, RecordedAt: occurredAt,
		Postings: []economy.PostingInput{
			{AccountID: economy.ActivityMintAccountID(), Amount: -1000},
			{AccountID: buyerID, Amount: 1000},
		},
	}); err != nil {
		t.Fatalf("credit integration buyer %s: %v", buyerID, err)
	}
}
