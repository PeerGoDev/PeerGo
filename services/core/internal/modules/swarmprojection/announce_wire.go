package swarmprojection

import (
	"encoding/json"

	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackerannouncev2"
)

// decodeCompletionAnnounce is the Core boundary for the shared announce
// stream. Tracker writes v2 after the bounded-ingest cutover, while v1 remains
// valid for retained JetStream messages and WAL replay.
func decodeCompletionAnnounce(payload []byte) (trackerannouncev1.Event, error) {
	if len(payload) < 2 || len(payload) > trackerannouncev1.MaxEventBytes {
		return trackerannouncev1.Event{}, ErrInput
	}
	var header struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(payload, &header); err != nil {
		return trackerannouncev1.Event{}, ErrInput
	}
	switch header.SchemaVersion {
	case trackerannouncev1.SchemaVersion:
		event, err := trackerannouncev1.Decode(payload)
		if err != nil {
			return trackerannouncev1.Event{}, ErrInput
		}
		return event, nil
	case trackerannouncev2.SchemaVersion:
		event, err := trackerannouncev2.Decode(payload)
		if err != nil {
			return trackerannouncev1.Event{}, ErrInput
		}
		return event.ToV1(), nil
	default:
		return trackerannouncev1.Event{}, ErrInput
	}
}
