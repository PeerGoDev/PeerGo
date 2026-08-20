// Package signedsnapshotv1 implements the common authenticated envelope used
// by PeerGo's independent Tracker control sections. Business payload schemas
// remain in their owning packages; this package only owns framing, checksums,
// key selection and domain-separated Ed25519 signatures.
package signedsnapshotv1

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	FormatVersion      = "1.0.0"
	SignatureAlgorithm = "Ed25519"
	MaxArtifactBytes   = 64 << 20
	MaxPayloadBytes    = 63 << 20
)

var (
	ErrInvalid          = errors.New("signed snapshot envelope is invalid")
	ErrSignatureInvalid = errors.New("signed snapshot signature is invalid")
	ErrKeyUnknown       = errors.New("signed snapshot signing key is unknown")

	keyIDPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Envelope is exported only so schema packages can perform focused tamper
// tests. Runtime callers should use Sign, Verify or Inspect.
type Envelope struct {
	FormatVersion string `json:"format_version"`
	Algorithm     string `json:"algorithm"`
	KeyID         string `json:"key_id"`
	PayloadSHA256 string `json:"payload_sha256"`
	Payload       []byte `json:"payload"`
	Signature     []byte `json:"signature"`
}

type Signed struct {
	Bytes          []byte
	KeyID          string
	PayloadSHA256  [sha256.Size]byte
	ArtifactSHA256 [sha256.Size]byte
}

type Verified struct {
	Payload        []byte
	KeyID          string
	PayloadSHA256  [sha256.Size]byte
	ArtifactSHA256 [sha256.Size]byte
}

type Inspection struct {
	Payload       []byte
	KeyID         string
	PayloadSHA256 [sha256.Size]byte
}

func ValidateKeyID(value string) error {
	if !keyIDPattern.MatchString(value) {
		return ErrInvalid
	}
	return nil
}

// ParseTrustedKeys is the shared configuration codec for rotatable snapshot
// verification keys. Keeping it beside the authenticated envelope prevents
// Core acceptance tools and Tracker runtime from interpreting the same key set
// differently during a cutover.
func ParseTrustedKeys(value string) (map[string]ed25519.PublicKey, error) {
	keys := make(map[string]ed25519.PublicKey)
	for _, item := range strings.Split(value, ",") {
		parts := strings.SplitN(strings.TrimSpace(item), "=", 2)
		if len(parts) != 2 || ValidateKeyID(parts[0]) != nil {
			return nil, errors.New("trusted snapshot keys must contain key_id=base64 entries")
		}
		if _, duplicate := keys[parts[0]]; duplicate {
			return nil, errors.New("trusted snapshot keys contain a duplicate key ID")
		}
		decoded, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, errors.New("trusted snapshot key values must be 32-byte Ed25519 public keys in standard padded Base64")
		}
		keys[parts[0]] = append(ed25519.PublicKey(nil), decoded...)
	}
	if len(keys) == 0 {
		return nil, errors.New("trusted snapshot keys require at least one key")
	}
	return keys, nil
}

// Sign authenticates already-canonical payload bytes. The caller owns the
// payload schema and must validate it before calling this function.
func Sign(payload []byte, keyID string, privateKey ed25519.PrivateKey, signatureDomain string) (Signed, error) {
	if ValidateKeyID(keyID) != nil || len(privateKey) != ed25519.PrivateKeySize ||
		len(payload) < 2 || len(payload) > MaxPayloadBytes || signatureDomain == "" {
		return Signed{}, ErrInvalid
	}
	payloadDigest := sha256.Sum256(payload)
	signature := ed25519.Sign(privateKey, SignatureMessage(signatureDomain, keyID, payloadDigest))
	encoded, err := json.Marshal(Envelope{
		FormatVersion: FormatVersion,
		Algorithm:     SignatureAlgorithm,
		KeyID:         keyID,
		PayloadSHA256: hex.EncodeToString(payloadDigest[:]),
		Payload:       payload,
		Signature:     signature,
	})
	if err != nil || len(encoded) > MaxArtifactBytes {
		return Signed{}, ErrInvalid
	}
	return Signed{
		Bytes: append([]byte(nil), encoded...), KeyID: keyID,
		PayloadSHA256: payloadDigest, ArtifactSHA256: sha256.Sum256(encoded),
	}, nil
}

func Verify(encoded []byte, trustedKeys map[string]ed25519.PublicKey, signatureDomain string) (Verified, error) {
	parsed, payloadDigest, err := decode(encoded)
	if err != nil || signatureDomain == "" {
		return Verified{}, ErrInvalid
	}
	publicKey, ok := trustedKeys[parsed.KeyID]
	if !ok {
		return Verified{}, ErrKeyUnknown
	}
	if len(publicKey) != ed25519.PublicKeySize ||
		!ed25519.Verify(publicKey, SignatureMessage(signatureDomain, parsed.KeyID, payloadDigest), parsed.Signature) {
		return Verified{}, ErrSignatureInvalid
	}
	return Verified{
		Payload: append([]byte(nil), parsed.Payload...), KeyID: parsed.KeyID,
		PayloadSHA256: payloadDigest, ArtifactSHA256: sha256.Sum256(encoded),
	}, nil
}

// Inspect validates framing and checksums without authenticating a signer. It
// is only appropriate for monotonic replacement of a service-owned file.
func Inspect(encoded []byte) (Inspection, error) {
	parsed, payloadDigest, err := decode(encoded)
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{
		Payload: append([]byte(nil), parsed.Payload...), KeyID: parsed.KeyID,
		PayloadSHA256: payloadDigest,
	}, nil
}

func SignatureMessage(signatureDomain, keyID string, payloadDigest [sha256.Size]byte) []byte {
	message := make([]byte, 0, len(signatureDomain)+2+len(keyID)+len(payloadDigest))
	message = append(message, signatureDomain...)
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(keyID)))
	message = append(message, size[:]...)
	message = append(message, keyID...)
	message = append(message, payloadDigest[:]...)
	return message
}

func StrictJSON(encoded []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func decode(encoded []byte) (Envelope, [sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	if len(encoded) < 2 || len(encoded) > MaxArtifactBytes {
		return Envelope{}, digest, ErrInvalid
	}
	var parsed Envelope
	if err := StrictJSON(encoded, &parsed); err != nil || parsed.FormatVersion != FormatVersion ||
		parsed.Algorithm != SignatureAlgorithm || ValidateKeyID(parsed.KeyID) != nil ||
		len(parsed.Payload) < 2 || len(parsed.Payload) > MaxPayloadBytes ||
		len(parsed.Signature) != ed25519.SignatureSize {
		return Envelope{}, digest, ErrInvalid
	}
	decodedDigest, err := hex.DecodeString(parsed.PayloadSHA256)
	if err != nil || !digestPattern.MatchString(parsed.PayloadSHA256) || len(decodedDigest) != len(digest) {
		return Envelope{}, digest, ErrInvalid
	}
	copy(digest[:], decodedDigest)
	observed := sha256.Sum256(parsed.Payload)
	if !bytes.Equal(observed[:], digest[:]) {
		return Envelope{}, digest, ErrInvalid
	}
	return parsed, digest, nil
}
