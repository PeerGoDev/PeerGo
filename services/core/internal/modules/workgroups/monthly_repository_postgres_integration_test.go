package workgroups

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWorkgroupMonthlyEvidenceAndConfirmedPayroll(t *testing.T) {
	databaseURL := os.Getenv("PEERGO_TEST_CORE_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PEERGO_TEST_CORE_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewPostgresRepository(pool)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	month := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	sharesAt := month.Add(time.Hour)
	afterClose := month.AddDate(0, 1, 0).Add(time.Hour)
	userID, retentionID, reseedID, reseedTransition := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec(`INSERT INTO identity.users (id,credential_ref,username,display_name,status)
          VALUES ($1,$2,$3,'Monthly worker','active')`, userID, uuid.New(), "monthly-"+userID.String()[:8])
	exec(`INSERT INTO economy.magic_accounts (id,user_id,account_kind,account_code,balance,updated_at)
          VALUES ($1::uuid,$1::uuid,'member','member:' || ($1::uuid)::text,0,$2)`, userID, month)
	for _, member := range []struct {
		id, transition uuid.UUID
		kind           GroupKind
	}{
		{retentionID, uuid.New(), GroupRetention}, {reseedID, reseedTransition, GroupReseed},
	} {
		exec(`INSERT INTO workgroups.memberships (id,group_kind,user_id,status,source,started_at,updated_at)
              VALUES ($1,$2,$3,'active','staff',$4,$4)`, member.id, member.kind, userID, sharesAt)
		exec(`INSERT INTO workgroups.membership_transitions (id,membership_id,group_kind,user_id,transition,to_status,actor_id,source,reason,authorization_decision_id,state_version,occurred_at)
              VALUES ($1,$2,$3,$4,'joined','active',$4,'staff','monthly integration fixture',$5,1,$6)`,
			member.transition, member.id, member.kind, userID, uuid.New(), sharesAt)
	}
	category := "monthly-" + userID.String()[:8]
	exec(`INSERT INTO catalog.categories (id,name,display_order,enabled,created_at,updated_at)
          VALUES ($1,'Monthly test',100000 + abs(hashtext($1)) % 900000,true,$2,$2)`, category, month)
	insertTorrent := func(submittedAt time.Time, approvedAt *time.Time, source string) int64 {
		t.Helper()
		objectID := uuid.New()
		digest := sha256.Sum256(objectID[:])
		exec(`INSERT INTO torrents.torrent_objects (id,content_sha256,byte_length,parser_version,validation_profile,info_offset,info_length,created_at)
              VALUES ($1,$2,256,'integration-v1','strict_upload',0,128,$3)`, objectID, digest[:], submittedAt)
		var torrentID int64
		if err := pool.QueryRow(ctx, `
INSERT INTO torrents.torrents (uploader_id,category_id,object_id,info_hash_v1,content_name,title,total_size_bytes,payload_size_bytes,
    file_count,piece_length_bytes,piece_count,state,version,submitted_at,published_at,state_changed_at,updated_at)
VALUES ($1,$2,$3,$4,'monthly.bin','Monthly test',4096,4096,1,16384,1,
    CASE WHEN $6::timestamptz IS NULL THEN 'pending_review' ELSE 'published' END,
    CASE WHEN $6::timestamptz IS NULL THEN 1 ELSE 2 END,$5,$6,COALESCE($6,$5),COALESCE($6,$5))
RETURNING id`, userID, category, objectID, digest[:20], submittedAt, approvedAt).Scan(&torrentID); err != nil {
			t.Fatal(err)
		}
		if approvedAt != nil {
			var tenure *uuid.UUID
			if source == "trusted_workgroup" {
				tenure = &reseedTransition
			}
			exec(`INSERT INTO review.torrent_decisions (id,torrent_id,reviewer_id,decision,reason_code,reason,
                expected_torrent_version,resulting_torrent_version,resulting_state,authorization_decision_id,
                occurred_at,resolution_source,membership_transition_id)
              VALUES ($1,$2,$3,'approve','meets_requirements','monthly integration fixture',1,2,'published',$4,$5,$6,$7)`,
				uuid.New(), torrentID, userID, uuid.New(), *approvedAt, source, tenure)
		}
		return torrentID
	}
	target := insertTorrent(sharesAt, &sharesAt, "trusted_workgroup")
	insertTorrent(sharesAt, &afterClose, "staff")
	insertTorrent(sharesAt, nil, "")
	insertTorrent(month, &sharesAt, "staff")
	publications, err := repository.ReseedPublications(ctx, reseedID, month, afterClose, 2, 0)
	if err != nil || publications.Counted != 2 || publications.Pending != 1 || publications.Total != 3 || len(publications.Items) != 2 {
		t.Fatalf("reseed publications = %+v, error = %v", publications, err)
	}
	if err := repository.SaveRetentionTarget(ctx, target, 1, false, userID, uuid.New(), sharesAt); err != nil {
		t.Fatal(err)
	}
	if err := repository.ClaimRetentionTarget(ctx, target, userID, uuid.New(), sharesAt.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	targets, err := repository.RetentionTargets(ctx, userID, 20, 0)
	if err != nil || len(targets) != 1 || targets[0].ClaimedAt == nil {
		t.Fatalf("retention targets = %+v, error = %v", targets, err)
	}
	for _, window := range []time.Time{sharesAt, sharesAt.Add(time.Hour)} {
		digest := bytes.Repeat([]byte{1}, 32)
		exec(`
INSERT INTO economy.seeding_reward_evidence_windows (
    window_start,window_end,built_at,window_evidence_sha256,projection_sha256,snapshot_id,snapshot_sequence,
    snapshot_observed_at,item_count,chunk_count,received_chunk_count,status,created_at,completed_at
) VALUES ($1,$2,$2,$3,$3,$4,1,$1,1,1,1,'complete',$2,$2)`, window, window.Add(time.Hour), digest, uuid.New())
		exec(`INSERT INTO economy.seeding_reward_evidence_items (
                 window_start,user_id,torrent_id,active_seconds,raw_uploaded_bytes,snapshot_seeders,snapshot_leechers,tracker_evidence_sha256
              ) VALUES ($1,$2,$3,3600,0,1,0,$4)`, window, userID, target, digest)
	}
	exec("UPDATE economy.seeding_reward_evidence_packing_control SET enabled = true WHERE singleton")
	exec("SELECT economy.seeding_reward_evidence_pack_batch(1)")
	progress, err := repository.RetentionProgress(ctx, uuid.Nil, retentionID, month, afterClose)
	if err != nil || len(progress.Items) != 1 || progress.Items[0].ActiveSeconds != 5400 ||
		progress.QualifiedCount != 1 || progress.QualifiedBytes != 4096 || progress.CompleteHours != 2 {
		t.Fatalf("retention progress = %+v, error = %v", progress, err)
	}
	input := PayrollInput{MembershipID: retentionID, Group: GroupRetention, Month: month, Amount: 500000, MinimumCount: 1}
	preview, err := repository.PreviewPayroll(ctx, input, afterClose)
	if err != nil || preview.State != "eligible" || preview.MissingHours == 0 {
		t.Fatalf("preview = %+v, error = %v", preview, err)
	}
	expected, _ := hex.DecodeString(preview.Digest)
	changed := input
	changed.Amount++
	if _, err := repository.PayPayroll(ctx, changed, expected, userID, uuid.New(), afterClose); !errors.Is(err, ErrPayrollPreviewChanged) {
		t.Fatalf("changed preview = %v", err)
	}
	for range 2 {
		paid, err := repository.PayPayroll(ctx, input, expected, userID, uuid.New(), afterClose)
		if err != nil || paid.State != "paid" || paid.TransactionID == nil || paid.Contribution != 1 {
			t.Fatalf("payroll = %+v, error = %v", paid, err)
		}
	}
	var balance int64
	if err := pool.QueryRow(ctx, "SELECT balance FROM economy.magic_accounts WHERE id=$1", userID).Scan(&balance); err != nil || balance != input.Amount {
		t.Fatalf("paid balance = %d, error = %v", balance, err)
	}
}
