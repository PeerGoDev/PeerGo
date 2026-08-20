package legacyusers

import (
	"bytes"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestSourceUserFingerprintIsStableAndBindsImportedFields(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("PtYes-password"), 10)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
	user := sourceUser{
		LegacyID: 1, Username: "legacy", Nickname: "旧用户", Avatar: "/uploads/images/avatar.png",
		Email: "legacy@example.test", PasswordHash: string(passwordHash),
		Passkey: "00112233445566778899aabbccddeeff", EmailVerified: true,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	key := bytes.Repeat([]byte{0x51}, 32)
	first, err := user.fingerprint(key)
	if err != nil {
		t.Fatal(err)
	}
	second, err := user.fingerprint(key)
	if err != nil || first != second {
		t.Fatalf("fingerprint retry = %x, %v", second, err)
	}
	user.Avatar = "/uploads/images/changed.png"
	changed, err := user.fingerprint(key)
	if err != nil || changed == first {
		t.Fatalf("fingerprint did not bind avatar: %x, %v", changed, err)
	}
}

func TestSourceUserAcceptsOnlyAuditedPtYesCredentialProfiles(t *testing.T) {
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("PtYes-password"), 10)
	now := time.Now().UTC()
	user := sourceUser{
		LegacyID: 1, Username: "legacy", Email: "legacy@example.test",
		PasswordHash: string(passwordHash), Passkey: "PtYesLegacyPasskey2026ABCDEF1234",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := user.validate(); err != nil {
		t.Fatalf("validate legacy profile: %v", err)
	}
	user.Passkey = "not/a/recognized/passkey"
	if err := user.validate(); err == nil {
		t.Fatal("validate accepted an unrecognized passkey")
	}
}

func TestSourceUserUsesPtYesEffectiveTrimmedIdentity(t *testing.T) {
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("PtYes-password"), 10)
	now := time.Now().UTC()
	user := sourceUser{
		LegacyID: 1, Username: " legacy ", Nickname: " 旧昵称 ", Email: "legacy@example.test",
		PasswordHash: string(passwordHash), Passkey: "00112233445566778899aabbccddeeff",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := user.validate(); err != nil {
		t.Fatalf("validate PtYes-normalized identity: %v", err)
	}
	if got := user.username(); got != "legacy" {
		t.Fatalf("username = %q", got)
	}
	if got := user.displayName(); got != "旧昵称" {
		t.Fatalf("display name = %q", got)
	}
}
