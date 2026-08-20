// Package trackerpasskeyv1 owns the cross-service format and lookup derivation
// for PeerGo's private HTTP Tracker route credential. It never generates or
// persists credentials; Vault remains the sole plaintext owner.
package trackerpasskeyv1

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const (
	RawBytes     = 16
	EncodedBytes = RawBytes * 2
	LookupKeyMin = 32

	ProfileCanonicalHexV1 = "canonical_hex_v1"
	ProfilePtYesAlnum32V1 = "ptyes_alnum32_v1"

	lookupHMACDomain = "peergo:vault:tracker-passkey-lookup:v1\x00"
)

var ErrInvalid = errors.New("Tracker passkey input is invalid")

func Validate(passkey string) error {
	if len(passkey) != EncodedBytes {
		return ErrInvalid
	}
	decoded, err := hex.DecodeString(passkey)
	if err != nil || len(decoded) != RawBytes || hex.EncodeToString(decoded) != passkey {
		return ErrInvalid
	}
	return nil
}

// DetectProfile accepts the canonical PeerGo format plus the one narrowly
// scoped PtYes compatibility format found in the audited migration snapshot.
// The legacy profile deliberately excludes canonical values, so a route value
// always resolves to one profile without a fallback verification loop.
func DetectProfile(passkey string) (string, error) {
	if Validate(passkey) == nil {
		return ProfileCanonicalHexV1, nil
	}
	if validPtYesAlnum32(passkey) {
		return ProfilePtYesAlnum32V1, nil
	}
	return "", ErrInvalid
}

// ValidateForProfile prevents a stored credential from being reinterpreted
// under a wider legacy syntax after import.
func ValidateForProfile(profile, passkey string) error {
	switch profile {
	case ProfileCanonicalHexV1:
		return Validate(passkey)
	case ProfilePtYesAlnum32V1:
		if validPtYesAlnum32(passkey) {
			return nil
		}
	}
	return ErrInvalid
}

func validPtYesAlnum32(passkey string) bool {
	if len(passkey) != EncodedBytes || Validate(passkey) == nil {
		return false
	}
	for index := 0; index < len(passkey); index++ {
		value := passkey[index]
		if !((value >= '0' && value <= '9') ||
			(value >= 'a' && value <= 'z') ||
			(value >= 'A' && value <= 'Z')) {
			return false
		}
	}
	return true
}

func LookupHMAC(key []byte, passkey string) ([sha256.Size]byte, error) {
	return LookupHMACForProfile(key, ProfileCanonicalHexV1, passkey)
}

// LookupHMACForProfile keeps the HMAC derivation identical across profiles.
// Tracker can therefore derive one lookup from the route value after its
// unambiguous format check; the compatibility profile does not fork identity.
func LookupHMACForProfile(key []byte, profile, passkey string) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	if len(key) < LookupKeyMin || ValidateForProfile(profile, passkey) != nil {
		return result, ErrInvalid
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(lookupHMACDomain))
	_, _ = mac.Write([]byte(passkey))
	copy(result[:], mac.Sum(nil))
	return result, nil
}

// LookupHMACAccepted is the Tracker-side route helper. It accepts no format
// beyond DetectProfile and never tries arbitrary normalization.
func LookupHMACAccepted(key []byte, passkey string) ([sha256.Size]byte, error) {
	profile, err := DetectProfile(passkey)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return LookupHMACForProfile(key, profile, passkey)
}
