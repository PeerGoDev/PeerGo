// Package promotioncontrolv1 defines the immutable command delivered from
// Core's promotion control plane to the Settlement ledger. It intentionally
// contains no actor identity or free-form reason; those remain in Core and its
// audit stream while Settlement receives only traffic-affecting facts.
package promotioncontrolv1

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
	SchemaVersion   = "settlement.promotion-command.v1"
	MaxCommandBytes = 4 << 10
)

type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeTorrent Scope = "torrent"
)

type Promotion string

const (
	PromotionFree                     Promotion = "free"
	PromotionDoubleUpload             Promotion = "double_upload"
	PromotionDoubleUploadFree         Promotion = "double_upload_free"
	PromotionHalfDownload             Promotion = "half_download"
	PromotionDoubleUploadHalfDownload Promotion = "double_upload_half_download"
	PromotionThirtyPercentDownload    Promotion = "thirty_percent_download"
)

var (
	ErrInvalid    = errors.New("Settlement promotion command is invalid")
	uuidPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	reasonPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

// Command is one closed-open promotion assignment. A global rule always
// explicitly overrides lower scopes in v1, preserving an unambiguous public
// badge and allowing a torrent rule to resume automatically after the global
// campaign ends.
type Command struct {
	SchemaVersion       string    `json:"schema_version"`
	CampaignID          string    `json:"campaign_id"`
	Scope               Scope     `json:"scope"`
	TorrentID           *int64    `json:"torrent_id,omitempty"`
	Promotion           Promotion `json:"promotion"`
	StartsAt            time.Time `json:"starts_at"`
	EndsAt              time.Time `json:"ends_at"`
	OverrideLowerScopes bool      `json:"override_lower_scopes"`
	ReasonCode          string    `json:"reason_code"`
}

func Validate(command Command) error {
	if command.SchemaVersion != SchemaVersion || !uuidPattern.MatchString(command.CampaignID) ||
		!validPromotion(command.Promotion) || command.StartsAt.IsZero() ||
		!command.EndsAt.After(command.StartsAt) || !isUTC(command.StartsAt) ||
		!isUTC(command.EndsAt) || !reasonPattern.MatchString(command.ReasonCode) {
		return ErrInvalid
	}
	switch command.Scope {
	case ScopeGlobal:
		if command.TorrentID != nil || !command.OverrideLowerScopes {
			return ErrInvalid
		}
	case ScopeTorrent:
		if command.TorrentID == nil || *command.TorrentID < 1 || command.OverrideLowerScopes {
			return ErrInvalid
		}
	default:
		return ErrInvalid
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

func validPromotion(value Promotion) bool {
	switch value {
	case PromotionFree, PromotionDoubleUpload, PromotionDoubleUploadFree,
		PromotionHalfDownload, PromotionDoubleUploadHalfDownload,
		PromotionThirtyPercentDownload:
		return true
	default:
		return false
	}
}

func isUTC(value time.Time) bool {
	_, offset := value.Zone()
	return offset == 0
}
