// Package trackerevent defines the immutable Core-to-Tracker control envelope.
// It deliberately contains no transport client: Core persists events first and
// the projector consumes them asynchronously in global sequence order.
package trackerevent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	TorrentEligibilityChangedType          = "tracker.torrent-eligibility.changed"
	TorrentEligibilityChangedSchemaVersion = "2.0.0"
	MaxPayloadBytes                        = 64 << 10
)

type Event struct {
	ID               uuid.UUID
	Type             string
	SchemaVersion    string
	AggregateID      int64
	AggregateVersion int64
	OccurredAt       time.Time
	Payload          []byte
	PayloadSHA256    [sha256.Size]byte
}

type Appender interface {
	Append(context.Context, Event) error
}

type TorrentEligibilityChangedV1 struct {
	SchemaVersion  string    `json:"schema_version"`
	EventType      string    `json:"event_type"`
	EventID        uuid.UUID `json:"event_id"`
	OccurredAt     time.Time `json:"occurred_at"`
	TorrentID      int64     `json:"torrent_id"`
	InfoHashV1     string    `json:"info_hash_v1"`
	TotalSizeBytes int64     `json:"total_size_bytes"`
	Enabled        bool      `json:"enabled"`
	TorrentVersion int64     `json:"torrent_version"`
}

type TorrentEligibilityInput struct {
	EventID        uuid.UUID
	OccurredAt     time.Time
	TorrentID      int64
	InfoHashV1     [20]byte
	TotalSizeBytes int64
	Enabled        bool
	TorrentVersion int64
}

func NewTorrentEligibilityChanged(input TorrentEligibilityInput) (Event, error) {
	if input.EventID == uuid.Nil || input.OccurredAt.IsZero() || input.TorrentID < 1 ||
		input.InfoHashV1 == ([20]byte{}) ||
		input.TotalSizeBytes < 1 || input.TorrentVersion < 1 {
		return Event{}, errors.New("Tracker eligibility event is missing required metadata")
	}
	payloadValue := TorrentEligibilityChangedV1{
		SchemaVersion:  TorrentEligibilityChangedSchemaVersion,
		EventType:      TorrentEligibilityChangedType,
		EventID:        input.EventID,
		OccurredAt:     input.OccurredAt.UTC(),
		TorrentID:      input.TorrentID,
		InfoHashV1:     hex.EncodeToString(input.InfoHashV1[:]),
		TotalSizeBytes: input.TotalSizeBytes,
		Enabled:        input.Enabled,
		TorrentVersion: input.TorrentVersion,
	}
	payload, err := json.Marshal(payloadValue)
	if err != nil {
		return Event{}, err
	}
	digest := sha256.Sum256(payload)
	event := Event{
		ID: input.EventID, Type: TorrentEligibilityChangedType,
		SchemaVersion: TorrentEligibilityChangedSchemaVersion,
		AggregateID:   input.TorrentID, AggregateVersion: input.TorrentVersion,
		OccurredAt: input.OccurredAt.UTC(), Payload: payload, PayloadSHA256: digest,
	}
	if err := Validate(event); err != nil {
		return Event{}, err
	}
	return event, nil
}

func DecodeTorrentEligibilityChanged(event Event) (TorrentEligibilityChangedV1, error) {
	if err := Validate(event); err != nil {
		return TorrentEligibilityChangedV1{}, err
	}
	var payload TorrentEligibilityChangedV1
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return TorrentEligibilityChangedV1{}, err
	}
	decodedHash, err := hex.DecodeString(payload.InfoHashV1)
	if err != nil || len(decodedHash) != 20 ||
		payload.TotalSizeBytes < 1 || payload.TorrentID != event.AggregateID ||
		payload.TorrentVersion != event.AggregateVersion {
		return TorrentEligibilityChangedV1{}, errors.New("Tracker eligibility payload is invalid")
	}
	return payload, nil
}

func Validate(event Event) error {
	if event.ID == uuid.Nil || event.Type != TorrentEligibilityChangedType ||
		event.SchemaVersion != TorrentEligibilityChangedSchemaVersion || event.AggregateID < 1 ||
		event.AggregateVersion < 1 || event.OccurredAt.IsZero() || len(event.Payload) < 2 ||
		len(event.Payload) > MaxPayloadBytes || !json.Valid(event.Payload) {
		return errors.New("Tracker control event is invalid")
	}
	var envelope struct {
		SchemaVersion  string    `json:"schema_version"`
		EventType      string    `json:"event_type"`
		EventID        uuid.UUID `json:"event_id"`
		OccurredAt     time.Time `json:"occurred_at"`
		TorrentID      int64     `json:"torrent_id"`
		TorrentVersion int64     `json:"torrent_version"`
	}
	if err := json.Unmarshal(event.Payload, &envelope); err != nil ||
		envelope.SchemaVersion != event.SchemaVersion || envelope.EventType != event.Type ||
		envelope.EventID != event.ID || !samePersistedTimestamp(envelope.OccurredAt, event.OccurredAt) ||
		envelope.TorrentID != event.AggregateID || envelope.TorrentVersion != event.AggregateVersion {
		return errors.New("Tracker control event envelope does not match metadata")
	}
	digest := sha256.Sum256(event.Payload)
	if !bytes.Equal(digest[:], event.PayloadSHA256[:]) {
		return errors.New("Tracker control event digest does not match")
	}
	return nil
}

// PostgreSQL timestamptz stores microsecond precision. Payloads retain the
// producer's nanoseconds, so compare the envelope at the durable precision
// used by the outbox row instead of rejecting an otherwise identical event.
func samePersistedTimestamp(left, right time.Time) bool {
	return left.UTC().Truncate(time.Microsecond).Equal(right.UTC().Truncate(time.Microsecond))
}
