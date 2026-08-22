package main

import (
	"testing"

	"github.com/peergo/peergo/contracts/go/trackerruntimepolicyv1"
)

func TestRateTargetsValidateProductionBounds(t *testing.T) {
	t.Parallel()
	valid := rateTargets{UserRequestsPerMinute: 600, UserBurst: 1200, AddressRequestsPerMinute: 5000, AddressBurst: 10000}
	if !valid.valid() {
		t.Fatal("production recovery defaults were rejected")
	}
	for name, candidate := range map[string]rateTargets{
		"user rate above contract": {UserRequestsPerMinute: 601, UserBurst: 1200, AddressRequestsPerMinute: 5000, AddressBurst: 10000},
		"user burst below rate":    {UserRequestsPerMinute: 600, UserBurst: 599, AddressRequestsPerMinute: 5000, AddressBurst: 10000},
		"address below user":       {UserRequestsPerMinute: 600, UserBurst: 1200, AddressRequestsPerMinute: 599, AddressBurst: 10000},
		"address burst below rate": {UserRequestsPerMinute: 600, UserBurst: 1200, AddressRequestsPerMinute: 5000, AddressBurst: 4999},
	} {
		candidate := candidate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if candidate.valid() {
				t.Fatal("invalid rate target was accepted")
			}
		})
	}
}

func TestRateTargetsApplyPreservesNonRatePolicy(t *testing.T) {
	t.Parallel()
	rules := []trackerruntimepolicyv1.SeedboxRule{{ID: "box-1", UserNumericID: 7, CIDR: "203.0.113.8/32"}}
	policy := trackerruntimepolicyv1.Policy{
		Revision: "tracker-runtime-current", AnnounceIntervalSeconds: 1800, MinAnnounceIntervalSeconds: 900,
		DefaultNumWant: 50, MaxNumWant: 100, ScrapeEnabled: true, MaxScrapeHashes: 50,
		ClientMode: trackerruntimepolicyv1.ClientModeAllowAll, UserRequestsPerMinute: 30, UserBurst: 60,
		AddressRequestsPerMinute: 120, AddressBurst: 240,
		Seedbox: trackerruntimepolicyv1.SeedboxPolicy{Enabled: true, UploadFactorBasisPoints: 5000,
			DownloadFactorBasisPoints: 20000, Rules: rules},
	}
	targets := rateTargets{UserRequestsPerMinute: 600, UserBurst: 1200, AddressRequestsPerMinute: 5000, AddressBurst: 10000}
	updated, changed := targets.apply(policy)
	if !changed || updated.UserRequestsPerMinute != 600 || updated.UserBurst != 1200 ||
		updated.AddressRequestsPerMinute != 5000 || updated.AddressBurst != 10000 {
		t.Fatalf("updated policy = %+v, changed=%v", updated, changed)
	}
	if updated.Revision != policy.Revision || updated.AnnounceIntervalSeconds != policy.AnnounceIntervalSeconds ||
		updated.Seedbox.DownloadFactorBasisPoints != policy.Seedbox.DownloadFactorBasisPoints ||
		len(updated.Seedbox.Rules) != 1 || updated.Seedbox.Rules[0] != rules[0] {
		t.Fatalf("non-rate policy changed: before=%+v after=%+v", policy, updated)
	}
	_, changed = targets.apply(updated)
	if changed {
		t.Fatal("applying the same targets was not idempotent")
	}
}
