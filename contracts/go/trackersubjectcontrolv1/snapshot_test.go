package trackersubjectcontrolv1

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"
)

func TestSignVerifySortsSubjectsAndRejectsTampering(t *testing.T) {
	t.Parallel()
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = 0x51
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	now := time.Date(2026, 8, 8, 20, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	artifact, err := Sign(Snapshot{
		GeneratedAt: now, ControlSequence: 7,
		Subjects: []Subject{
			{UserID: "0198f20a-6da8-7e51-9c64-222222222222", LookupHMAC: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", CredentialVersion: 2, DownloadRestricted: true},
			{UserID: "0198f20a-6da8-7e51-9c64-111111111111", LookupHMAC: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CredentialVersion: 1},
		},
	}, "active", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Verify(artifact.Bytes, map[string]ed25519.PublicKey{"active": privateKey.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	if !verified.Snapshot.GeneratedAt.Equal(now) || verified.Snapshot.GeneratedAt.Location() != time.UTC ||
		verified.Snapshot.Subjects[0].CredentialVersion != 1 || !verified.Snapshot.Subjects[1].DownloadRestricted ||
		verified.Snapshot.StateSHA256 == "" {
		t.Fatalf("verified snapshot = %+v", verified.Snapshot)
	}

	tampered := append([]byte(nil), artifact.Bytes...)
	tampered[len(tampered)/2] ^= 1
	if _, err := Verify(tampered, map[string]ed25519.PublicKey{"active": privateKey.Public().(ed25519.PublicKey)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tamper error = %v", err)
	}
}

func TestSignRejectsDuplicateUsersAndLookupHMACs(t *testing.T) {
	t.Parallel()
	privateKey := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	base := Subject{UserID: "0198f20a-6da8-7e51-9c64-111111111111", LookupHMAC: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CredentialVersion: 1}
	for name, subjects := range map[string][]Subject{
		"user":   {base, {UserID: base.UserID, LookupHMAC: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", CredentialVersion: 1}},
		"lookup": {base, {UserID: "0198f20a-6da8-7e51-9c64-222222222222", LookupHMAC: base.LookupHMAC, CredentialVersion: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Sign(Snapshot{GeneratedAt: time.Now(), ControlSequence: 1, Subjects: subjects}, "active", privateKey)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("Sign() error = %v", err)
			}
		})
	}
}
