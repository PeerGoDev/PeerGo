package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/peergo/peergo/services/settlement/internal/hnrpolicy"
)

func TestParseRevisionReadsCanonicalHNRPolicy(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "policy.json")
	encoded, err := hnrpolicy.Encode(hnrpolicy.Policy{
		Rule: hnrpolicy.RuleRef{ID: "global-hnr", Version: 1}, Mode: hnrpolicy.ModeEnforced,
		RequiredSeedSeconds: 604800, RequiredRatioBasisPoints: 10_000,
		AssessmentWindowSeconds: 604800, GracePeriodSeconds: 86400, MaxIntervalCreditSeconds: 5400,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	revision, err := parseRevision(
		"0198f20a-6da8-7e51-9c64-111111111111", path, "2026-08-10T00:00:00Z", "", "", "", "",
	)
	if err != nil || revision.Policy.Mode != hnrpolicy.ModeEnforced || revision.Policy.RequiredSeedSeconds != 604800 {
		t.Fatalf("parseRevision() revision=%+v error=%v", revision, err)
	}
}
