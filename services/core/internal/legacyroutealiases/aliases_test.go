package legacyroutealiases

import (
	"encoding/hex"
	"errors"
	"testing"
)

func TestDigestCanonicalizesLegacyUUIDWithoutRetainingIt(t *testing.T) {
	lower, err := Digest("8afa7211-23aa-4abf-915e-c862506a7a5f")
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	upper, err := Digest("8AFA7211-23AA-4ABF-915E-C862506A7A5F")
	if err != nil {
		t.Fatalf("Digest() uppercase error = %v", err)
	}
	if lower != upper || len(hex.EncodeToString(lower[:])) != 64 {
		t.Fatalf("Digest() did not produce one canonical SHA-256 value")
	}
	if _, err := Digest("9830"); !errors.Is(err, ErrInvalidAlias) {
		t.Fatalf("Digest() numeric error = %v", err)
	}
}
