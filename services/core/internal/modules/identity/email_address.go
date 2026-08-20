package identity

import (
	"net/mail"
	"strings"
	"unicode/utf8"
)

// normalizeEmailAddress is the single Core-side syntax boundary for transient
// email input. The normalized address may be forwarded to Vault but is never
// persisted by Core.
func normalizeEmailAddress(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || utf8.RuneCountInString(email) > 254 {
		return "", ErrInvalidInput
	}
	return email, nil
}
