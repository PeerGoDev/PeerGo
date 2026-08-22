package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadArtifactHashesTheStrictMode0600File(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "preview.jsonl")
	userID := "0198f20a-6da8-7e51-9c64-111111111111"
	contents := fmt.Sprintf(
		`{"schema_version":"seeding.reward.compensation.preview.v1","record_type":"manifest","tracker_source_stream":"PEERGO_TRACKER_ANNOUNCE_V1","tracker_fence_sequence":42,"maximum_interval_credit_seconds":2100,"first_window":"2026-08-21T05:00:00Z","last_window":"2026-08-21T05:00:00Z"}`+"\n"+
			`{"schema_version":"seeding.reward.compensation.preview.v1","record_type":"positive_delta","source_reference":"seeding_compensation:v1:1787288400:%s","window_start":"2026-08-21T05:00:00Z","user_id":"%s","policy_revision":"rousi-reward-v1","benefit_revision":"benefit-v1.e1.l1.rousi-v1","corrected_calculation_sha256":"%s","corrected_evidence_sha256":"%s","original_reward":0,"corrected_reward":5,"magic_delta":5,"experience_delta":"0.1","eligible_torrent_count":1,"capped":false}`+"\n",
		userID, userID, strings.Repeat("1", 64), strings.Repeat("2", 64),
	)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := validateArtifactPath(path); err != nil {
		t.Fatal(err)
	}
	artifact, size, digest, err := readArtifact(path)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(contents))
	if size != int64(len(contents)) || digest != want || artifact.RecordCount != 1 || artifact.MagicDelta != 5 {
		t.Fatalf("readArtifact() size=%d digest=%x artifact=%+v", size, digest, artifact)
	}
}

func TestValidateArtifactPathRejectsLooseModeAndSymlink(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "preview.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := validateArtifactPath(path); err == nil {
		t.Fatal("mode-0644 artifact was accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link.jsonl")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := validateArtifactPath(link); err == nil {
		t.Fatal("artifact symlink was accepted")
	}
}
