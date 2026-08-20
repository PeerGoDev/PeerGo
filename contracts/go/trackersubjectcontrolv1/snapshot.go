// Package trackersubjectcontrolv1 defines the signed Core-to-Tracker subject
// admission snapshot. It contains only irreversible passkey lookup material
// and stable internal identity; plaintext passkeys and account profile fields
// are forbidden by this contract.
package trackersubjectcontrolv1

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
	SnapshotSchemaVersion = "1.0.0"
	MaxSubjectEntries     = 1_000_000

	signatureDomain = "peergo:tracker-subject-control-snapshot-signature:v1\x00"
	stateHashDomain = "peergo:tracker-subject-control-snapshot-state:v1\x00"
)

var (
	ErrInvalid          = signedsnapshotv1.ErrInvalid
	ErrSignatureInvalid = signedsnapshotv1.ErrSignatureInvalid
	ErrKeyUnknown       = signedsnapshotv1.ErrKeyUnknown

	uuidPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Subject is one account currently allowed to announce. LookupHMAC is the
// lowercase hex encoding of the domain-separated HMAC produced by Vault and
// recomputed locally by Tracker; it cannot be used as the route passkey.
type Subject struct {
	UserID             string `json:"user_id"`
	NumericUserID      int64  `json:"numeric_user_id,omitempty"`
	LookupHMAC         string `json:"lookup_hmac"`
	CredentialVersion  int64  `json:"credential_version"`
	DownloadRestricted bool   `json:"download_restricted,omitempty"`
}

type Snapshot struct {
	SchemaVersion   string    `json:"schema_version"`
	GeneratedAt     time.Time `json:"generated_at"`
	ControlSequence int64     `json:"control_sequence"`
	StateSHA256     string    `json:"state_sha256"`
	Subjects        []Subject `json:"subjects"`
}

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

type Inspection struct {
	Snapshot      Snapshot
	KeyID         string
	PayloadSHA256 [sha256.Size]byte
}

func ValidateKeyID(value string) error {
	return signedsnapshotv1.ValidateKeyID(value)
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
	if err != nil {
		return SignedArtifact{}, ErrInvalid
	}
	signed, err := signedsnapshotv1.Sign(payload, keyID, privateKey, signatureDomain)
	if err != nil {
		return SignedArtifact{}, err
	}
	return SignedArtifact{
		Bytes: signed.Bytes, Snapshot: normalized, KeyID: signed.KeyID,
		PayloadSHA256: signed.PayloadSHA256, ArtifactSHA256: signed.ArtifactSHA256,
	}, nil
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

func DecodeLookupHMAC(value string) ([sha256.Size]byte, error) {
	var lookup [sha256.Size]byte
	if !digestPattern.MatchString(value) {
		return lookup, ErrInvalid
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != len(lookup) {
		return lookup, ErrInvalid
	}
	copy(lookup[:], decoded)
	return lookup, nil
}

func decodeSnapshot(payload []byte) (Snapshot, error) {
	var snapshot Snapshot
	if err := signedsnapshotv1.StrictJSON(payload, &snapshot); err != nil {
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
		snapshot.GeneratedAt = snapshot.GeneratedAt.UTC().Round(0)
		snapshot.Subjects = append([]Subject(nil), snapshot.Subjects...)
		sort.Slice(snapshot.Subjects, func(left, right int) bool {
			return snapshot.Subjects[left].LookupHMAC < snapshot.Subjects[right].LookupHMAC
		})
	}
	if snapshot.SchemaVersion != SnapshotSchemaVersion || snapshot.GeneratedAt.IsZero() ||
		snapshot.ControlSequence < 1 || len(snapshot.Subjects) > MaxSubjectEntries {
		return Snapshot{}, ErrInvalid
	}
	_, offset := snapshot.GeneratedAt.Zone()
	if offset != 0 {
		return Snapshot{}, ErrInvalid
	}
	seenUsers := make(map[string]struct{}, len(snapshot.Subjects))
	previousLookup := ""
	for _, subject := range snapshot.Subjects {
		if !uuidPattern.MatchString(subject.UserID) || subject.NumericUserID < 0 || subject.CredentialVersion < 1 ||
			!digestPattern.MatchString(subject.LookupHMAC) || subject.LookupHMAC <= previousLookup {
			return Snapshot{}, ErrInvalid
		}
		if _, exists := seenUsers[subject.UserID]; exists {
			return Snapshot{}, ErrInvalid
		}
		if _, err := DecodeLookupHMAC(subject.LookupHMAC); err != nil {
			return Snapshot{}, err
		}
		seenUsers[subject.UserID] = struct{}{}
		previousLookup = subject.LookupHMAC
	}
	digest, err := calculateStateDigest(snapshot.ControlSequence, snapshot.Subjects)
	if err != nil {
		return Snapshot{}, err
	}
	want := hex.EncodeToString(digest[:])
	if preparing && snapshot.StateSHA256 == "" {
		snapshot.StateSHA256 = want
	}
	if snapshot.StateSHA256 != want {
		return Snapshot{}, ErrInvalid
	}
	return snapshot, nil
}

func calculateStateDigest(sequence int64, subjects []Subject) ([sha256.Size]byte, error) {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(stateHashDomain))
	var integer [8]byte
	binary.BigEndian.PutUint64(integer[:], uint64(sequence))
	_, _ = hasher.Write(integer[:])
	binary.BigEndian.PutUint64(integer[:], uint64(len(subjects)))
	_, _ = hasher.Write(integer[:])
	for _, subject := range subjects {
		userID, err := decodeUUID(subject.UserID)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		lookup, err := DecodeLookupHMAC(subject.LookupHMAC)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		_, _ = hasher.Write(userID[:])
		_, _ = hasher.Write(lookup[:])
		binary.BigEndian.PutUint64(integer[:], uint64(subject.CredentialVersion))
		_, _ = hasher.Write(integer[:])
	}
	// Numeric IDs are intentionally a separate optional digest section so
	// pre-registry snapshots keep their historical hash. A zero value is safe:
	// it can never satisfy a member-bound Seedbox rule.
	hasNumericUserID := false
	for _, subject := range subjects {
		hasNumericUserID = hasNumericUserID || subject.NumericUserID > 0
	}
	if hasNumericUserID {
		_, _ = hasher.Write([]byte("peergo:tracker-subject-numeric-user-ids:v1\x00"))
		for _, subject := range subjects {
			binary.BigEndian.PutUint64(integer[:], uint64(subject.NumericUserID))
			_, _ = hasher.Write(integer[:])
		}
	}
	// Preserve the exact digest of older unrestricted snapshots. A separate,
	// domain-separated fixed-width bitmap is present only when at least one
	// subject is restricted, avoiding ambiguous variable-length records.
	hasDownloadRestriction := false
	for _, subject := range subjects {
		hasDownloadRestriction = hasDownloadRestriction || subject.DownloadRestricted
	}
	if hasDownloadRestriction {
		_, _ = hasher.Write([]byte("peergo:tracker-subject-download-restrictions:v1\x00"))
		for _, subject := range subjects {
			if subject.DownloadRestricted {
				_, _ = hasher.Write([]byte{1})
			} else {
				_, _ = hasher.Write([]byte{0})
			}
		}
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}

func decodeUUID(value string) ([16]byte, error) {
	var id [16]byte
	if !uuidPattern.MatchString(value) {
		return id, ErrInvalid
	}
	compact := make([]byte, 0, 32)
	for index := range value {
		if value[index] != '-' {
			compact = append(compact, value[index])
		}
	}
	decoded, err := hex.DecodeString(string(compact))
	if err != nil || len(decoded) != len(id) {
		return id, ErrInvalid
	}
	copy(id[:], decoded)
	return id, nil
}
