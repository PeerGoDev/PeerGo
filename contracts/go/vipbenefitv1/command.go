// Package vipbenefitv1 defines the immutable Core-to-Settlement command for
// VIP accounting entitlement changes. The command contains no staff identity
// or free-form reason; those remain in Core's audit boundary.
package vipbenefitv1

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
)

const (
	SchemaVersion                   = "settlement.vip-benefit-command.v1"
	MaxCommandBytes                 = 2 << 10
	EntitlementDownloadChargeExempt = "traffic.download.charge_exempt"
)

var (
	ErrInvalid  = errors.New("Settlement VIP benefit command is invalid")
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

// Command is one VIP state transition. ActiveUntil is part of the immutable
// fact so an expiring VIP stops affecting settlement without requiring a
// best-effort timer to emit a second command at the expiry instant.
type Command struct {
	SchemaVersion string     `json:"schema_version"`
	TransitionID  string     `json:"transition_id"`
	UserID        string     `json:"user_id"`
	Entitlement   string     `json:"entitlement"`
	Enabled       bool       `json:"enabled"`
	ActiveUntil   *time.Time `json:"active_until,omitempty"`
	StateVersion  int64      `json:"state_version"`
	EffectiveAt   time.Time  `json:"effective_at"`
}

func Validate(command Command) error {
	if command.SchemaVersion != SchemaVersion || !uuidPattern.MatchString(command.TransitionID) ||
		!uuidPattern.MatchString(command.UserID) || command.Entitlement != EntitlementDownloadChargeExempt ||
		command.StateVersion < 1 || command.EffectiveAt.IsZero() || !isUTC(command.EffectiveAt) {
		return ErrInvalid
	}
	if command.ActiveUntil != nil {
		if !command.Enabled || command.ActiveUntil.IsZero() || !isUTC(*command.ActiveUntil) {
			return ErrInvalid
		}
	}
	return nil
}

func Encode(command Command) ([]byte, error) {
	if Validate(command) != nil {
		return nil, ErrInvalid
	}
	encoded, err := json.Marshal(command)
	if err != nil || len(encoded) < 2 || len(encoded) > MaxCommandBytes {
		return nil, ErrInvalid
	}
	return encoded, nil
}

func Decode(encoded []byte) (Command, error) {
	if len(encoded) < 2 || len(encoded) > MaxCommandBytes {
		return Command{}, ErrInvalid
	}
	var command Command
	if err := signedsnapshotv1.StrictJSON(encoded, &command); err != nil || Validate(command) != nil {
		return Command{}, ErrInvalid
	}
	canonical, err := Encode(command)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return Command{}, ErrInvalid
	}
	return command, nil
}

func SHA256(encoded []byte) ([sha256.Size]byte, error) {
	if _, err := Decode(encoded); err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

func isUTC(value time.Time) bool {
	_, offset := value.Zone()
	return offset == 0
}
