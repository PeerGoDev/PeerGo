package ingest

import (
	"encoding/json"

	"github.com/peergo/peergo/contracts/go/trackerannouncev1"
	"github.com/peergo/peergo/contracts/go/trackerannouncev2"
)

type producerIdentity struct {
	ID       string
	Epoch    string
	Sequence int64
}

type decodedAnnounce struct {
	Event    trackerannouncev1.Event
	Producer *producerIdentity
}

func decodeAnnounce(payload []byte) (decodedAnnounce, error) {
	if len(payload) < 2 || len(payload) > trackerannouncev1.MaxEventBytes {
		return decodedAnnounce{}, ErrInvalidInput
	}
	var header struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(payload, &header); err != nil {
		return decodedAnnounce{}, ErrInvalidInput
	}
	switch header.SchemaVersion {
	case trackerannouncev1.SchemaVersion:
		event, err := trackerannouncev1.Decode(payload)
		if err != nil {
			return decodedAnnounce{}, ErrInvalidInput
		}
		return decodedAnnounce{Event: event}, nil
	case trackerannouncev2.SchemaVersion:
		event, err := trackerannouncev2.Decode(payload)
		if err != nil {
			return decodedAnnounce{}, ErrInvalidInput
		}
		return decodedAnnounce{
			Event:    event.ToV1(),
			Producer: &producerIdentity{ID: event.ProducerID, Epoch: event.ProducerEpoch, Sequence: event.ProducerSequence},
		}, nil
	default:
		return decodedAnnounce{}, ErrInvalidInput
	}
}
