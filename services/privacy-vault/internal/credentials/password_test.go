package credentials

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	encoded, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	match, needsRehash, err := VerifyPassword(encoded, "correct horse battery staple")
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !match || needsRehash {
		t.Fatalf("VerifyPassword() = match %v, needsRehash %v", match, needsRehash)
	}

	match, _, err = VerifyPassword(encoded, "incorrect")
	if err != nil {
		t.Fatalf("VerifyPassword(incorrect) error = %v", err)
	}
	if match {
		t.Fatal("VerifyPassword(incorrect) matched")
	}
}

func TestLegacyPtYesBcryptRequiresRehash(t *testing.T) {
	legacy, err := bcrypt.GenerateFromPassword([]byte("PtYes-member-password"), 10)
	if err != nil {
		t.Fatal(err)
	}
	match, needsRehash, err := VerifyPassword(string(legacy), "PtYes-member-password")
	if err != nil || !match || !needsRehash {
		t.Fatalf("VerifyPassword(legacy) = %v, %v, %v", match, needsRehash, err)
	}
	match, needsRehash, err = VerifyPassword(string(legacy), "wrong")
	if err != nil || match || needsRehash {
		t.Fatalf("VerifyPassword(wrong legacy) = %v, %v, %v", match, needsRehash, err)
	}
}

func TestLegacyBcryptVerifierRejectsUnapprovedProfiles(t *testing.T) {
	cost12, err := bcrypt.GenerateFromPassword([]byte("password"), 12)
	if err != nil {
		t.Fatal(err)
	}
	for _, encoded := range []string{
		string(cost12),
		strings.Replace(string(cost12), "$2a$12$", "$2b$12$", 1),
		"$2a$10$invalid",
	} {
		if _, _, err := VerifyPassword(encoded, "password"); err == nil {
			t.Fatalf("VerifyPassword() accepted unapproved bcrypt profile %q", encoded[:min(len(encoded), 7)])
		}
	}
}

func TestPasswordHashRejectsUnboundedParameters(t *testing.T) {
	malicious := "$argon2id$v=19$m=999999999,t=3,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaA"
	if _, _, err := VerifyPassword(malicious, "password"); err == nil {
		t.Fatal("VerifyPassword() accepted unbounded memory")
	}

	if _, _, err := VerifyPassword(strings.Repeat("x", maxEncodedHashBytes+1), "password"); err == nil {
		t.Fatal("VerifyPassword() accepted oversized encoded hash")
	}
}

func TestLookupHMACNormalizesCaseAndWhitespace(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	left, err := LookupHMAC(key, " Demo@PeerGo.Local ")
	if err != nil {
		t.Fatalf("LookupHMAC() error = %v", err)
	}
	right, err := LookupHMAC(key, "demo@peergo.local")
	if err != nil {
		t.Fatalf("LookupHMAC() error = %v", err)
	}
	if string(left) != string(right) {
		t.Fatal("LookupHMAC() did not normalize case and whitespace")
	}
}
