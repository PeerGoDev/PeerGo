package legacyusers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/peergo/peergo/contracts/go/trackerpasskeyv1"
	"github.com/peergo/peergo/services/privacy-vault/internal/credentials"
)

const userFingerprintDomain = "peergo:migration:ptyes-user:v1\x00"

var errInvalidSourceUser = errors.New("PtYes source user is invalid")

type sourceUser struct {
	LegacyID      int64
	Username      string
	Nickname      string
	Avatar        string
	Email         string
	PasswordHash  string
	Passkey       string
	EmailVerified bool
	Banned        bool
	TOTPEnabled   bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
}

func (user sourceUser) username() string {
	// PtYes normalizes usernames at registration and lookup time, but three
	// audited legacy rows still contain boundary whitespace. The raw value stays
	// in the keyed fingerprint while PeerGo receives PtYes' effective identity.
	return strings.TrimSpace(user.Username)
}

func (user sourceUser) displayName() string {
	nickname := strings.TrimSpace(user.Nickname)
	if nickname == "" {
		return user.username()
	}
	return nickname
}

func (user sourceUser) emailAddress() string {
	return strings.ToLower(strings.TrimSpace(user.Email))
}

func (user sourceUser) passkeyProfile() (string, error) {
	return trackerpasskeyv1.DetectProfile(user.Passkey)
}

func (user sourceUser) validate() error {
	username := user.username()
	usernameRunes := utf8.RuneCountInString(username)
	displayName := user.displayName()
	displayRunes := utf8.RuneCountInString(displayName)
	if user.LegacyID < 1 || !utf8.ValidString(user.Username) || usernameRunes < 1 || usernameRunes > 64 ||
		!utf8.ValidString(displayName) || strings.TrimSpace(displayName) == "" || displayRunes > 80 ||
		!utf8.ValidString(user.Email) || strings.TrimSpace(user.Email) != user.Email ||
		credentials.MaskEmail(user.Email) == "***" ||
		credentials.ValidateLegacyPtYesPasswordHash(user.PasswordHash) != nil ||
		user.CreatedAt.IsZero() || user.UpdatedAt.Before(user.CreatedAt) {
		return errInvalidSourceUser
	}
	if _, err := user.passkeyProfile(); err != nil {
		return errInvalidSourceUser
	}
	if user.DeletedAt != nil && user.DeletedAt.Before(user.CreatedAt) {
		return errInvalidSourceUser
	}
	return nil
}

// fingerprint commits to every source user field needed by the current or
// asset follow-up import. It is keyed because password hashes, passkeys and
// email addresses are low-entropy or directly reusable credentials. Only the
// resulting 32 bytes cross into Core.
func (user sourceUser) fingerprint(key []byte) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	if len(key) < sha256.Size || user.validate() != nil {
		return result, errInvalidSourceUser
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(userFingerprintDomain))
	writeInt64(mac, user.LegacyID)
	for _, value := range []string{
		user.Username,
		user.Nickname,
		user.Avatar,
		user.Email,
		user.PasswordHash,
		user.Passkey,
		user.CreatedAt.UTC().Format(time.RFC3339Nano),
		user.UpdatedAt.UTC().Format(time.RFC3339Nano),
	} {
		writeString(mac, value)
	}
	writeBool(mac, user.EmailVerified)
	writeBool(mac, user.Banned)
	writeBool(mac, user.TOTPEnabled)
	if user.DeletedAt == nil {
		writeBool(mac, false)
	} else {
		writeBool(mac, true)
		writeString(mac, user.DeletedAt.UTC().Format(time.RFC3339Nano))
	}
	copy(result[:], mac.Sum(nil))
	return result, nil
}

type byteWriter interface {
	Write([]byte) (int, error)
}

func writeInt64(writer byteWriter, value int64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(value))
	_, _ = writer.Write(encoded[:])
}

func writeString(writer byteWriter, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write([]byte(value))
}

func writeBool(writer byteWriter, value bool) {
	if value {
		_, _ = writer.Write([]byte{1})
		return
	}
	_, _ = writer.Write([]byte{0})
}

func sourceUserError(legacyID int64, code string) error {
	// The numeric legacy ID and a bounded code are safe operator diagnostics;
	// never wrap the source row or a driver error containing query arguments.
	return fmt.Errorf("legacy user %d: %s: %w", legacyID, code, errInvalidSourceUser)
}
