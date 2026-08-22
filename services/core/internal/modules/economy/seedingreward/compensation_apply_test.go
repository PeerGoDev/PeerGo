package seedingreward

import (
	"crypto/sha256"
	"testing"
)

func TestParseCompensationBenefitRevision(t *testing.T) {
	entitlement, level, policy, err := parseCompensationBenefitRevision("benefit-v1.e12.l3.rousi-v1")
	if err != nil || entitlement != 12 || level != 3 || policy != "rousi-v1" {
		t.Fatalf("parseCompensationBenefitRevision() = %d, %d, %q, %v", entitlement, level, policy, err)
	}
	for _, invalid := range []string{"", "benefit-v1.e0.l1.rousi-v1", "benefit-v1.e1.l0.rousi-v1", "benefit-v1.e1.l1"} {
		if _, _, _, err := parseCompensationBenefitRevision(invalid); err == nil {
			t.Fatalf("invalid benefit revision %q was accepted", invalid)
		}
	}
}

func TestCompensationPostingDigestBindsArtifact(t *testing.T) {
	_, records := compensationArtifactFixture()
	firstArtifact := sha256.Sum256([]byte("first"))
	secondArtifact := sha256.Sum256([]byte("second"))
	first, err := compensationPostingSHA256(firstArtifact, records[0])
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := compensationPostingSHA256(firstArtifact, records[0])
	if err != nil || replayed != first {
		t.Fatalf("replayed posting digest = %x, %v; want %x", replayed, err, first)
	}
	changed, err := compensationPostingSHA256(secondArtifact, records[0])
	if err != nil || changed == first {
		t.Fatal("posting digest did not bind the approved artifact")
	}
}
