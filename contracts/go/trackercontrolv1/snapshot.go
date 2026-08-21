// Package trackercontrolv1 defines the language-neutral semantics and Go
// codec for PeerGo's signed Core-to-Tracker control snapshot. It deliberately
// contains no database, filesystem, HTTP or runtime policy dependency.
package trackercontrolv1

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"time"

	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
)

const (
	FormatVersion         = signedsnapshotv1.FormatVersion
	SnapshotSchemaVersion = "3.1.0"
	legacySchemaVersion   = "3.0.0"
	SignatureAlgorithm    = signedsnapshotv1.SignatureAlgorithm
	MaxArtifactBytes      = signedsnapshotv1.MaxArtifactBytes
	MaxSnapshotPayload    = signedsnapshotv1.MaxPayloadBytes
	MaxTorrentEntries     = 1_000_000

	signatureDomain = "peergo:tracker-control-snapshot-signature:v1\x00"
	stateHashDomain = "peergo:tracker-control-snapshot-state:v1\x00"
)

var (
	ErrInvalid          = signedsnapshotv1.ErrInvalid
	ErrSignatureInvalid = signedsnapshotv1.ErrSignatureInvalid
	ErrKeyUnknown       = signedsnapshotv1.ErrKeyUnknown

	hashPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Torrent is one enabled swarm identity plus the durable cumulative completion
// count required by authenticated scrape responses. Promotions, user
// entitlements, H&R, clients and traffic policy are intentionally absent.
type Torrent struct {
	TorrentID          int64  `json:"torrent_id"`
	InfoHashV1         string `json:"info_hash_v1"`
	TotalSizeBytes     int64  `json:"total_size_bytes"`
	CompletedDownloads int64  `json:"completed_downloads"`
	TorrentVersion     int64  `json:"torrent_version"`
	ControlSequence    int64  `json:"control_sequence"`
}

type Snapshot struct {
	SchemaVersion      string    `json:"schema_version"`
	GeneratedAt        time.Time `json:"generated_at"`
	ControlSequence    int64     `json:"control_sequence"`
	CompletionSequence int64     `json:"completion_sequence,omitempty"`
	StateSHA256        string    `json:"state_sha256"`
	Torrents           []Torrent `json:"torrents"`
}

type envelope = signedsnapshotv1.Envelope

type SignedArtifact struct {
	Bytes          []byte
	Snapshot       Snapshot
	KeyID          string
	PayloadSHA256  [sha256.Size]byte
	ArtifactSHA256 [sha256.Size]byte
}

type VerifiedSnapshot struct {
	Snapshot       Snapshot
	KeyID          string
	PayloadSHA256  [sha256.Size]byte
	ArtifactSHA256 [sha256.Size]byte
}

// Inspection contains checksum-validated but unauthenticated metadata. It is
// only for monotonic artifact publication. Authorization code must use Verify.
type Inspection struct {
	Snapshot      Snapshot
	KeyID         string
	PayloadSHA256 [sha256.Size]byte
}

func Sign(snapshot Snapshot, keyID string, privateKey ed25519.PrivateKey) (SignedArtifact, error) {
	if ValidateKeyID(keyID) != nil || len(privateKey) != ed25519.PrivateKeySize {
		return SignedArtifact{}, ErrInvalid
	}
	normalized, err := normalizeAndValidate(snapshot, true)
	if err != nil {
		return SignedArtifact{}, err
	}
	payload, err := json.Marshal(normalized)
	if err != nil || len(payload) < 2 || len(payload) > MaxSnapshotPayload {
		return SignedArtifact{}, ErrInvalid
	}
	signed, err := signedsnapshotv1.Sign(payload, keyID, privateKey, signatureDomain)
	if err != nil {
		return SignedArtifact{}, ErrInvalid
	}
	return SignedArtifact{
		Bytes: signed.Bytes, Snapshot: normalized, KeyID: signed.KeyID,
		PayloadSHA256: signed.PayloadSHA256, ArtifactSHA256: signed.ArtifactSHA256,
	}, nil
}

func ValidateKeyID(value string) error {
	return signedsnapshotv1.ValidateKeyID(value)
}

func Verify(encoded []byte, trustedKeys map[string]ed25519.PublicKey) (VerifiedSnapshot, error) {
	verified, err := signedsnapshotv1.Verify(encoded, trustedKeys, signatureDomain)
	if err != nil {
		return VerifiedSnapshot{}, err
	}
	snapshot, err := decodeSnapshot(verified.Payload)
	if err != nil {
		return VerifiedSnapshot{}, err
	}
	return VerifiedSnapshot{
		Snapshot: snapshot, KeyID: verified.KeyID, PayloadSHA256: verified.PayloadSHA256,
		ArtifactSHA256: verified.ArtifactSHA256,
	}, nil
}

// InspectUnverified validates framing, payload checksum and snapshot semantics
// without authenticating the signer. Filesystem publishers use it solely to
// prevent sequence regression when replacing a service-owned local artifact.
func InspectUnverified(encoded []byte) (Inspection, error) {
	inspection, err := signedsnapshotv1.Inspect(encoded)
	if err != nil {
		return Inspection{}, err
	}
	snapshot, err := decodeSnapshot(inspection.Payload)
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{Snapshot: snapshot, KeyID: inspection.KeyID, PayloadSHA256: inspection.PayloadSHA256}, nil
}

func DecodeInfoHash(value string) ([20]byte, error) {
	var hash [20]byte
	if !hashPattern.MatchString(value) {
		return hash, ErrInvalid
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != len(hash) {
		return hash, ErrInvalid
	}
	copy(hash[:], decoded)
	return hash, nil
}

func decodeSnapshot(payload []byte) (Snapshot, error) {
	var snapshot Snapshot
	if err := strictJSON(payload, &snapshot); err != nil {
		return Snapshot{}, ErrInvalid
	}
	normalized, err := normalizeAndValidate(snapshot, false)
	if err != nil {
		return Snapshot{}, err
	}
	canonical, err := json.Marshal(normalized)
	if err != nil || !bytes.Equal(canonical, payload) {
		return Snapshot{}, ErrInvalid
	}
	return normalized, nil
}

func normalizeAndValidate(snapshot Snapshot, preparing bool) (Snapshot, error) {
	if preparing {
		if snapshot.SchemaVersion == "" {
			snapshot.SchemaVersion = SnapshotSchemaVersion
		}
		if snapshot.SchemaVersion != SnapshotSchemaVersion {
			return Snapshot{}, ErrInvalid
		}
		snapshot.GeneratedAt = snapshot.GeneratedAt.UTC().Round(0)
		snapshot.Torrents = append([]Torrent(nil), snapshot.Torrents...)
		sort.Slice(snapshot.Torrents, func(left, right int) bool {
			return snapshot.Torrents[left].InfoHashV1 < snapshot.Torrents[right].InfoHashV1
		})
	}
	validSchema := snapshot.SchemaVersion == SnapshotSchemaVersion ||
		(snapshot.SchemaVersion == legacySchemaVersion && snapshot.CompletionSequence == 0)
	if !validSchema || snapshot.GeneratedAt.IsZero() ||
		snapshot.ControlSequence < 0 || snapshot.CompletionSequence < 0 || len(snapshot.Torrents) > MaxTorrentEntries {
		return Snapshot{}, ErrInvalid
	}
	_, offset := snapshot.GeneratedAt.Zone()
	if offset != 0 {
		return Snapshot{}, ErrInvalid
	}
	seenIDs := make(map[int64]struct{}, len(snapshot.Torrents))
	previousHash := ""
	for _, torrent := range snapshot.Torrents {
		if torrent.TorrentID < 1 || !hashPattern.MatchString(torrent.InfoHashV1) || torrent.TotalSizeBytes < 1 || torrent.CompletedDownloads < 0 ||
			torrent.TorrentVersion < 1 || torrent.ControlSequence < 1 ||
			torrent.ControlSequence > snapshot.ControlSequence || torrent.InfoHashV1 <= previousHash {
			return Snapshot{}, ErrInvalid
		}
		if _, exists := seenIDs[torrent.TorrentID]; exists {
			return Snapshot{}, ErrInvalid
		}
		seenIDs[torrent.TorrentID] = struct{}{}
		previousHash = torrent.InfoHashV1
	}
	if snapshot.ControlSequence == 0 && len(snapshot.Torrents) != 0 {
		return Snapshot{}, ErrInvalid
	}
	stateDigest, err := calculateStateDigest(snapshot.ControlSequence, snapshot.CompletionSequence, snapshot.Torrents)
	if err != nil {
		return Snapshot{}, err
	}
	wantStateDigest := hex.EncodeToString(stateDigest[:])
	if preparing && snapshot.StateSHA256 == "" {
		snapshot.StateSHA256 = wantStateDigest
	}
	if snapshot.StateSHA256 != wantStateDigest {
		return Snapshot{}, ErrInvalid
	}
	return snapshot, nil
}

func calculateStateDigest(sequence, completionSequence int64, torrents []Torrent) ([sha256.Size]byte, error) {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(stateHashDomain))
	var integer [8]byte
	binary.BigEndian.PutUint64(integer[:], uint64(sequence))
	_, _ = hasher.Write(integer[:])
	// CompletionSequence is an optional, backwards-compatible extension of
	// schema v3. A zero value preserves the exact digest of existing signed
	// artifacts; positive values order cumulative completion-stat refreshes
	// independently from torrent eligibility changes.
	if completionSequence > 0 {
		binary.BigEndian.PutUint64(integer[:], uint64(completionSequence))
		_, _ = hasher.Write(integer[:])
	}
	binary.BigEndian.PutUint64(integer[:], uint64(len(torrents)))
	_, _ = hasher.Write(integer[:])
	for _, torrent := range torrents {
		infoHash, err := DecodeInfoHash(torrent.InfoHashV1)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		for _, value := range []int64{torrent.TorrentID, torrent.TotalSizeBytes, torrent.CompletedDownloads, torrent.TorrentVersion, torrent.ControlSequence} {
			binary.BigEndian.PutUint64(integer[:], uint64(value))
			_, _ = hasher.Write(integer[:])
		}
		_, _ = hasher.Write(infoHash[:])
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}

func signatureMessage(keyID string, payloadDigest [sha256.Size]byte) []byte {
	return signedsnapshotv1.SignatureMessage(signatureDomain, keyID, payloadDigest)
}

func strictJSON(encoded []byte, destination any) error {
	return signedsnapshotv1.StrictJSON(encoded, destination)
}
