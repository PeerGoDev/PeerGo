package trackerruntimepolicyv1

import (
	"net/netip"
	"testing"
)

func TestClassifyAddressUsesMostSpecificReviewedRule(t *testing.T) {
	policy := validSnapshot().Policy
	policy.Seedbox = SeedboxPolicy{
		Enabled: true, UploadFactorBasisPoints: 5_000, DownloadFactorBasisPoints: 20_000,
		SeedboxSpeedLimitBytesPerSecond:  200 << 20,
		StandardSpeedLimitBytesPerSecond: 20 << 20,
		Rules: []SeedboxRule{
			{ID: "provider-range", CIDR: "198.51.100.0/24"},
			{ID: "member-box", CIDR: "198.51.100.7/32"},
		},
	}
	classified, err := policy.ClassifyAddress(netip.MustParseAddr("198.51.100.7"))
	if err != nil || !classified.Seedbox || classified.RuleID != "member-box" ||
		classified.UploadFactorBasisPoints != 5_000 || classified.DownloadFactorBasisPoints != 20_000 ||
		classified.SpeedLimitBytesPerSecond != 200<<20 {
		t.Fatalf("classification = %+v, error = %v", classified, err)
	}
	standard, err := policy.ClassifyAddress(netip.MustParseAddr("203.0.113.7"))
	if err != nil || standard.Seedbox || standard.UploadFactorBasisPoints != 10_000 ||
		standard.DownloadFactorBasisPoints != 10_000 || standard.SpeedLimitBytesPerSecond != 20<<20 {
		t.Fatalf("standard classification = %+v, error = %v", standard, err)
	}
}

func TestClassifyAddressForUserDoesNotLeakMemberRule(t *testing.T) {
	policy := validSnapshot().Policy
	policy.Seedbox = SeedboxPolicy{
		Enabled: true, UploadFactorBasisPoints: 5000, DownloadFactorBasisPoints: 20_000,
		Rules: []SeedboxRule{
			{ID: "global-provider", CIDR: "198.51.100.0/24"},
			{ID: "member-box", CIDR: "198.51.100.7/32", UserNumericID: 42},
		},
	}
	member, err := policy.ClassifyAddressForUser(netip.MustParseAddr("198.51.100.7"), 42)
	if err != nil || member.RuleID != "member-box" {
		t.Fatalf("member classification = %#v, %v", member, err)
	}
	other, err := policy.ClassifyAddressForUser(netip.MustParseAddr("198.51.100.7"), 43)
	if err != nil || other.RuleID != "global-provider" {
		t.Fatalf("other classification = %#v, %v", other, err)
	}
}
