package trackercontrolv1

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSignVerifyProducesSortedImmutableSnapshot(t *testing.T) {
	t.Parallel()
	privateKey := testPrivateKey(0x11)
	generatedAt := time.Date(2026, time.August, 8, 10, 30, 0, 123, time.FixedZone("UTC+8", 8*60*60))
	snapshot := Snapshot{
		GeneratedAt: generatedAt, ControlSequence: 9,
		Torrents: []Torrent{
			{TorrentID: 2, InfoHashV1: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", TotalSizeBytes: 20, TorrentVersion: 3, ControlSequence: 9},
			{TorrentID: 1, InfoHashV1: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", TotalSizeBytes: 10, TorrentVersion: 2, ControlSequence: 4},
		},
	}

	signed, err := Sign(snapshot, "control-2026-08", privateKey)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	verified, err := Verify(signed.Bytes, map[string]ed25519.PublicKey{
		"control-2026-08": privateKey.Public().(ed25519.PublicKey),
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if verified.Snapshot.GeneratedAt.Location() != time.UTC || !verified.Snapshot.GeneratedAt.Equal(generatedAt) ||
		verified.Snapshot.SchemaVersion != SnapshotSchemaVersion || verified.Snapshot.StateSHA256 == "" ||
		verified.Snapshot.Torrents[0].TorrentID != 1 || verified.Snapshot.Torrents[1].TorrentID != 2 {
		t.Fatalf("verified snapshot = %+v", verified.Snapshot)
	}
	if signed.PayloadSHA256 != verified.PayloadSHA256 || signed.ArtifactSHA256 != verified.ArtifactSHA256 {
		t.Fatal("signed and verified digests differ")
	}

	repeated, err := Sign(snapshot, "control-2026-08", privateKey)
	if err != nil || string(repeated.Bytes) != string(signed.Bytes) {
		t.Fatalf("deterministic Sign() error=%v equal=%t", err, string(repeated.Bytes) == string(signed.Bytes))
	}
}

func TestVerifyRejectsTamperingUnknownKeysAndUnknownPayloadFields(t *testing.T) {
	t.Parallel()
	privateKey := testPrivateKey(0x22)
	signed, err := Sign(testSnapshot(), "active", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	trusted := map[string]ed25519.PublicKey{"active": privateKey.Public().(ed25519.PublicKey)}

	var parsed envelope
	if err := json.Unmarshal(signed.Bytes, &parsed); err != nil {
		t.Fatal(err)
	}
	parsed.Payload[0] ^= 1
	tampered, _ := json.Marshal(parsed)
	if _, err := Verify(tampered, trusted); !errors.Is(err, ErrInvalid) {
		t.Fatalf("payload tamper error = %v", err)
	}

	if err := json.Unmarshal(signed.Bytes, &parsed); err != nil {
		t.Fatal(err)
	}
	parsed.Payload = append(parsed.Payload[:len(parsed.Payload)-1], []byte(`,"unexpected":true}`)...)
	digest := sha256.Sum256(parsed.Payload)
	parsed.PayloadSHA256 = hex.EncodeToString(digest[:])
	parsed.Signature = ed25519.Sign(privateKey, signatureMessage(parsed.KeyID, digest))
	unknownField, _ := json.Marshal(parsed)
	if _, err := Verify(unknownField, trusted); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown payload field error = %v", err)
	}

	if _, err := Verify(signed.Bytes, nil); !errors.Is(err, ErrKeyUnknown) {
		t.Fatalf("unknown key error = %v", err)
	}
	otherKey := testPrivateKey(0x23).Public().(ed25519.PublicKey)
	if _, err := Verify(signed.Bytes, map[string]ed25519.PublicKey{"active": otherKey}); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("wrong signature key error = %v", err)
	}
}

func TestVerifyRequiresCanonicalPayloadAndLowercaseDigest(t *testing.T) {
	t.Parallel()
	privateKey := testPrivateKey(0x24)
	signed, err := Sign(testSnapshot(), "active", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	trusted := map[string]ed25519.PublicKey{"active": privateKey.Public().(ed25519.PublicKey)}
	var parsed envelope
	if err := json.Unmarshal(signed.Bytes, &parsed); err != nil {
		t.Fatal(err)
	}
	parsed.Payload = append([]byte(" "), parsed.Payload...)
	digest := sha256.Sum256(parsed.Payload)
	parsed.PayloadSHA256 = hex.EncodeToString(digest[:])
	parsed.Signature = ed25519.Sign(privateKey, signatureMessage(parsed.KeyID, digest))
	nonCanonical, _ := json.Marshal(parsed)
	if _, err := Verify(nonCanonical, trusted); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-canonical payload error = %v", err)
	}

	if err := json.Unmarshal(signed.Bytes, &parsed); err != nil {
		t.Fatal(err)
	}
	parsed.PayloadSHA256 = strings.ToUpper(parsed.PayloadSHA256)
	upperDigest, _ := json.Marshal(parsed)
	if _, err := Verify(upperDigest, trusted); !errors.Is(err, ErrInvalid) {
		t.Fatalf("uppercase payload digest error = %v", err)
	}
}

func TestSignRejectsDivergentOrDuplicateState(t *testing.T) {
	t.Parallel()
	privateKey := testPrivateKey(0x33)
	snapshot := testSnapshot()
	snapshot.StateSHA256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if _, err := Sign(snapshot, "active", privateKey); !errors.Is(err, ErrInvalid) {
		t.Fatalf("divergent state digest error = %v", err)
	}

	snapshot = testSnapshot()
	duplicate := snapshot.Torrents[0]
	duplicate.InfoHashV1 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	snapshot.Torrents = append(snapshot.Torrents, duplicate)
	if _, err := Sign(snapshot, "active", privateKey); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate torrent id error = %v", err)
	}

	snapshot = testSnapshot()
	snapshot.ControlSequence = 0
	if _, err := Sign(snapshot, "active", privateKey); !errors.Is(err, ErrInvalid) {
		t.Fatalf("entry beyond zero sequence error = %v", err)
	}
}

func TestInspectUnverifiedChecksPayloadButNotSignature(t *testing.T) {
	t.Parallel()
	signed, err := Sign(testSnapshot(), "active", testPrivateKey(0x44))
	if err != nil {
		t.Fatal(err)
	}
	var parsed envelope
	if err := json.Unmarshal(signed.Bytes, &parsed); err != nil {
		t.Fatal(err)
	}
	parsed.Signature = make([]byte, ed25519.SignatureSize)
	encoded, _ := json.Marshal(parsed)
	inspection, err := InspectUnverified(encoded)
	if err != nil || inspection.Snapshot.ControlSequence != 4 || inspection.KeyID != "active" {
		t.Fatalf("InspectUnverified() = %+v, %v", inspection, err)
	}
}

func testSnapshot() Snapshot {
	return Snapshot{
		GeneratedAt: time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC), ControlSequence: 4,
		Torrents: []Torrent{{
			TorrentID:  1,
			InfoHashV1: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", TotalSizeBytes: 42,
			TorrentVersion: 2, ControlSequence: 4,
		}},
	}
}

func testPrivateKey(fill byte) ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = fill
	}
	return ed25519.NewKeyFromSeed(seed)
}
