package identity_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/peergo/peergo/services/core/internal/modules/identity"
	"net/netip"
	"os"
	"testing"
	"time"
)

func TestPostgresWebActivityProjectionIsBoundedAndAuthentic(t *testing.T) {
	url := os.Getenv("PEERGO_TEST_IDENTITY_ACTIVITY_DATABASE_URL")
	if url == "" {
		t.Skip("isolated activity database is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	now := time.Now().UTC().Truncate(time.Microsecond)
	user := uuid.New()
	token := sha256.Sum256([]byte(user.String()))
	_, err = pool.Exec(ctx, `INSERT INTO identity.users(id,credential_ref,username,display_name,status) VALUES($1,$2,$3,'Activity integration','active')`, user, uuid.New(), "activity-"+user.String()[:8])
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM identity.user_network_observations WHERE user_id=$1`, user)
		_, _ = pool.Exec(context.Background(), `DELETE FROM identity.sessions WHERE user_id=$1`, user)
		_, _ = pool.Exec(context.Background(), `DELETE FROM identity.user_activity WHERE user_id=$1`, user)
		_, _ = pool.Exec(context.Background(), `DELETE FROM identity.users WHERE id=$1`, user)
	}()
	repo := identity.NewPostgresRepository(pool)
	assertActivity := func(want time.Time, version int64) {
		t.Helper()
		var at time.Time
		var v int64
		if err := pool.QueryRow(ctx, `SELECT last_active_at,version FROM identity.user_activity WHERE user_id=$1`, user).Scan(&at, &v); err != nil {
			t.Fatal(err)
		}
		if !at.Equal(want) || v != version {
			t.Fatalf("activity %v version %d, want %v version %d", at, v, want, version)
		}
	}
	create := func(hash []byte, at time.Time, address netip.Addr) {
		t.Helper()
		if err := repo.CreateSession(ctx, identity.SessionRecord{TokenHash: hash, User: identity.User{ID: user}, CreatedAt: at, ExpiresAt: now.Add(2 * time.Hour), ClientAddress: address}); err != nil {
			t.Fatal(err)
		}
	}
	create(token[:], now, netip.Addr{})
	assertActivity(now, 1)
	if _, err := repo.ActiveSession(ctx, token[:], now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertActivity(now, 1)
	if _, err := repo.ActiveSession(ctx, token[:], now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertActivity(now.Add(5*time.Minute), 2)
	other := sha256.Sum256([]byte("other-" + user.String()))
	create(other[:], now.Add(6*time.Minute), netip.MustParseAddr("203.0.113.6"))
	assertActivity(now.Add(5*time.Minute), 2)
	if _, err := repo.ActiveSession(ctx, token[:], now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertActivity(now.Add(10*time.Minute), 3)
	if _, err := repo.ActiveSession(ctx, token[:], now.Add(8*time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertActivity(now.Add(10*time.Minute), 3)
	if err := repo.RevokeSession(ctx, token[:], now.Add(11*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ActiveSession(ctx, token[:], now.Add(20*time.Minute)); !errors.Is(err, identity.ErrSessionNotFound) {
		t.Fatalf("revoked session: %v", err)
	}
	assertActivity(now.Add(10*time.Minute), 3)
	if _, err := repo.ActiveSession(ctx, other[:], now.Add(3*time.Hour)); !errors.Is(err, identity.ErrSessionNotFound) {
		t.Fatalf("expired session: %v", err)
	}
	assertActivity(now.Add(10*time.Minute), 3)
	unknown := sha256.Sum256([]byte("unknown"))
	if _, err := repo.ActiveSession(ctx, unknown[:], now.Add(30*time.Minute)); !errors.Is(err, identity.ErrSessionNotFound) {
		t.Fatalf("unknown session: %v", err)
	}
	assertActivity(now.Add(10*time.Minute), 3)
	_, err = pool.Exec(ctx, `UPDATE identity.users SET status='disabled' WHERE id=$1`, user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ActiveSession(ctx, other[:], now.Add(30*time.Minute)); !errors.Is(err, identity.ErrSessionNotFound) {
		t.Fatalf("disabled user: %v", err)
	}
	assertActivity(now.Add(10*time.Minute), 3)
}
