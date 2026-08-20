package credentials

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const identifierDomainSeparation = "peergo:vault:identifier:v1\x00"

// LookupHMAC normalizes a login identifier and returns a keyed equality index.
// The key lives in service configuration and is never stored in PostgreSQL.
func LookupHMAC(key []byte, identifier string) ([]byte, error) {
	normalized := strings.ToLower(strings.TrimSpace(identifier))
	if normalized == "" || utf8.RuneCountInString(normalized) > 254 {
		return nil, errors.New("identifier must contain between 1 and 254 characters")
	}
	if len(key) < sha256.Size {
		return nil, errors.New("identifier key must contain at least 32 bytes")
	}

	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(identifierDomainSeparation))
	_, _ = mac.Write([]byte(normalized))
	return mac.Sum(nil), nil
}

// MaskUsername returns the bounded display-only form stored beside a keyed
// lookup. Keeping masking here gives runtime provisioning and development
// seeding one implementation without exposing the original identifier.
func MaskUsername(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= 1 {
		return "*"
	}
	return fmt.Sprintf("%c***", runes[0])
}

// MaskEmail preserves only the domain and one local-part rune. It is not an
// authentication index and must never be used to reconstruct or compare mail.
func MaskEmail(value string) string {
	parts := strings.SplitN(strings.TrimSpace(value), "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "***"
	}
	return MaskUsername(parts[0]) + "@" + strings.ToLower(parts[1])
}
