package policy

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSnapshotCodecRoundTripsCanonicalResolvedPolicy(t *testing.T) {
	t.Parallel()
	promotion, err := ResolvePromotion(ProfilePeerGoV1, testNow, []PromotionRule{
		testPromotionRule(SourceTorrentPromotion, ScopeTorrent, "torrent-free", PromotionDoubleUploadFree, testNow.Add(-time.Hour), nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := testSnapshot(ProfilePeerGoV1, "peergo-policy-2026-08", promotion)
	snapshot.Benefits.Medal = &FactorGrant{Rule: testRule(SourceMedal, "medal-125", 1), Factors: Factors{Upload: 12_500, Download: OneX}}
	encoded, err := EncodeSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeSnapshot(encoded)
	if err != nil {
		t.Fatal(err)
	}
	result, err := SettleDelta(decoded, 100, 100)
	if err != nil || result.CreditedUploaded != 200 || result.ChargedDownloaded != 0 {
		t.Fatalf("decoded snapshot settlement = %+v, %v", result, err)
	}
	if _, err := SnapshotSHA256(decoded); err != nil {
		t.Fatalf("SnapshotSHA256() error = %v", err)
	}
}

func TestSnapshotCodecRejectsUnknownAndNonCanonicalJSON(t *testing.T) {
	t.Parallel()
	promotion, err := ResolvePromotion(ProfilePeerGoV1, testNow, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeSnapshot(testSnapshot(ProfilePeerGoV1, "policy-v1", promotion))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeSnapshot(append([]byte(" "), encoded...)); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("DecodeSnapshot(noncanonical) error = %v", err)
	}
	if _, err := DecodeSnapshot(append(encoded[:len(encoded)-1], []byte(`,"extra":true}`)...)); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("DecodeSnapshot(unknown) error = %v", err)
	}
}

func TestDevelopmentNormalSnapshotExampleIsCanonical(t *testing.T) {
	t.Parallel()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate policy test source")
	}
	examplePath := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "..", "examples", "settlement", "policy-snapshot.peergo-v1-normal.json")
	encoded, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := DecodeSnapshot(encoded)
	if err != nil {
		t.Fatalf("DecodeSnapshot(example) error = %v", err)
	}
	if snapshot.Profile != ProfilePeerGoV1 || snapshot.Promotion.Factors != (Factors{Upload: OneX, Download: OneX}) ||
		snapshot.Revision.ID != "development-normal-policy" || snapshot.Revision.Version != 1 {
		t.Fatalf("example snapshot = %+v", snapshot)
	}
}
