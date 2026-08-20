package contenttip

import (
	"encoding/hex"
	"testing"
	"time"
)

func TestNormalizePolicyMatchesMigrationBaseline(t *testing.T) {
	t.Parallel()
	policy, snapshot, err := NormalizePolicy(PolicyRevision{
		Revision: "content-tip-disabled-v1", Enabled: false,
		MinimumAmount: 1, MaximumAmount: 10_000, DailyGrossLimit: 20_000,
		FeeBPS: 0, CreatedAt: time.Date(2026, 8, 17, 0, 0, 1, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NormalizePolicy() error = %v", err)
	}
	if got, want := string(snapshot), `{"revision":"content-tip-disabled-v1","enabled":false,"minimum_amount":1,"maximum_amount":10000,"daily_gross_limit":20000,"fee_bps":0,"created_at":"2026-08-17T00:00:01Z"}`; got != want {
		t.Fatalf("snapshot = %s, want %s", got, want)
	}
	if got, want := hex.EncodeToString(policy.SnapshotSHA256[:]), "b487c1e65db7ddd583a8d7595287b801a1b6d2411260eb558b7efa57b5fc663f"; got != want {
		t.Fatalf("digest = %s, want %s", got, want)
	}
}

func TestTargetReferencesAreStronglyTyped(t *testing.T) {
	t.Parallel()
	if !TorrentTarget(42).validReference() {
		t.Fatal("positive torrent target must be valid")
	}
	invalid := TorrentTarget(42)
	invalid.PostID = [16]byte{1}
	if invalid.validReference() {
		t.Fatal("target carrying more than one identity must be invalid")
	}
}
