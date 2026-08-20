// Package hnrcontrolv1 defines Core's authenticated command to append one
// global H&R policy revision in Settlement. Operator identity and free-form
// reasons deliberately stay in Core and never enter the accounting database.
package hnrcontrolv1

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/peergo/peergo/contracts/go/hnrpolicyv1"
	"github.com/peergo/peergo/contracts/go/signedsnapshotv1"
)

const (
	SchemaVersion   = "hnr-control.v1"
	MaxCommandBytes = 8 << 10
)

var (
	ErrInvalid         = errors.New("H&R control command is invalid")
	ErrInvalidEncoding = errors.New("H&R control command encoding is invalid")
	revisionIDPattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

type Command struct {
	SchemaVersion string             `json:"schema_version"`
	RevisionID    string             `json:"revision_id"`
	EffectiveAt   time.Time          `json:"effective_at"`
	Policy        hnrpolicyv1.Policy `json:"-"`
}

type wireCommand struct {
	SchemaVersion string          `json:"schema_version"`
	RevisionID    string          `json:"revision_id"`
	EffectiveAt   time.Time       `json:"effective_at"`
	Policy        json.RawMessage `json:"policy"`
}

func Validate(command Command) error {
	if command.SchemaVersion != SchemaVersion || !revisionIDPattern.MatchString(command.RevisionID) ||
		command.EffectiveAt.IsZero() || command.EffectiveAt.Location() != time.UTC ||
		hnrpolicyv1.Validate(command.Policy) != nil {
		return ErrInvalid
	}
	return nil
}

func Encode(command Command) ([]byte, error) {
	if Validate(command) != nil {
		return nil, ErrInvalidEncoding
	}
	policy, err := hnrpolicyv1.Encode(command.Policy)
	if err != nil {
		return nil, ErrInvalidEncoding
	}
	policy = bytes.TrimSuffix(policy, []byte{'\n'})
	encoded, err := json.Marshal(wireCommand{
		SchemaVersion: command.SchemaVersion, RevisionID: command.RevisionID,
		EffectiveAt: command.EffectiveAt, Policy: policy,
	})
	if err != nil {
		return nil, ErrInvalidEncoding
	}
	encoded = append(encoded, '\n')
	if len(encoded) < 3 || len(encoded) > MaxCommandBytes {
		return nil, ErrInvalidEncoding
	}
	return encoded, nil
}

func Decode(encoded []byte) (Command, error) {
	if len(encoded) < 3 || len(encoded) > MaxCommandBytes {
		return Command{}, ErrInvalidEncoding
	}
	var wire wireCommand
	if err := signedsnapshotv1.StrictJSON(encoded, &wire); err != nil {
		return Command{}, ErrInvalidEncoding
	}
	policy, err := hnrpolicyv1.Decode(append(bytes.Clone(wire.Policy), '\n'))
	if err != nil {
		return Command{}, ErrInvalidEncoding
	}
	command := Command{
		SchemaVersion: wire.SchemaVersion, RevisionID: wire.RevisionID,
		EffectiveAt: wire.EffectiveAt, Policy: policy,
	}
	canonical, err := Encode(command)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return Command{}, ErrInvalidEncoding
	}
	return command, nil
}

func SHA256(encoded []byte) ([sha256.Size]byte, error) {
	if _, err := Decode(encoded); err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}
