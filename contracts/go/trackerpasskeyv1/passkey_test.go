package trackerpasskeyv1

import (
	"errors"
	"testing"
)

func TestLookupHMACValidatesCanonicalPasskeyAndKey(t *testing.T) {
	t.Parallel()
	key := []byte("tracker-passkey-lookup-key-test-2026")
	passkey := "00112233445566778899aabbccddeeff"
	first, err := LookupHMAC(key, passkey)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LookupHMAC(key, passkey)
	if err != nil || first != second {
		t.Fatalf("LookupHMAC() = %x, %v", second, err)
	}
	for _, invalid := range []string{"short", "00112233445566778899AABBCCDDEEFF", "00112233445566778899aabbccddeezz"} {
		if _, err := LookupHMAC(key, invalid); !errors.Is(err, ErrInvalid) {
			t.Fatalf("LookupHMAC(%q) error = %v", invalid, err)
		}
	}
	if _, err := LookupHMAC([]byte("short"), passkey); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short key error = %v", err)
	}
}

func TestPtYesCompatibilityProfileIsExplicitAndUnambiguous(t *testing.T) {
	t.Parallel()
	key := []byte("tracker-passkey-lookup-key-test-2026")
	legacy := "PtYesLegacyPasskey2026ABCDEF1234"
	if len(legacy) != EncodedBytes {
		t.Fatalf("test legacy passkey length = %d", len(legacy))
	}
	profile, err := DetectProfile(legacy)
	if err != nil || profile != ProfilePtYesAlnum32V1 {
		t.Fatalf("DetectProfile() = %q, %v", profile, err)
	}
	if Validate(legacy) == nil {
		t.Fatal("canonical validator accepted the legacy profile")
	}
	legacyLookup, err := LookupHMACForProfile(key, profile, legacy)
	if err != nil {
		t.Fatal(err)
	}
	detectedLookup, err := LookupHMACAccepted(key, legacy)
	if err != nil || detectedLookup != legacyLookup {
		t.Fatalf("LookupHMACAccepted() = %x, %v", detectedLookup, err)
	}

	canonical := "00112233445566778899aabbccddeeff"
	if profile, err := DetectProfile(canonical); err != nil || profile != ProfileCanonicalHexV1 {
		t.Fatalf("canonical DetectProfile() = %q, %v", profile, err)
	}
	if ValidateForProfile(ProfilePtYesAlnum32V1, canonical) == nil {
		t.Fatal("legacy profile accepted an overlapping canonical value")
	}
	for _, invalid := range []string{
		"PtYesLegacyPasskey2026ABCDEF123!",
		"PtYesLegacyPasskey2026ABCDEF123",
		"00112233445566778899aabbccddeeffx",
	} {
		if _, err := LookupHMACAccepted(key, invalid); !errors.Is(err, ErrInvalid) {
			t.Fatalf("LookupHMACAccepted(%q) error = %v", invalid, err)
		}
	}
}
