package trafficoutbox

import (
	"context"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/peergo/peergo/services/settlement/internal/resultstream"
)

var ErrStreamDrift = resultstream.ErrDrift

type StreamManager = resultstream.Manager
type NATSStreamManager = resultstream.NATSManager

func NewNATSStreamManager(js jetstream.JetStream) (*NATSStreamManager, error) {
	manager, err := resultstream.NewNATSManager(js)
	if err != nil {
		return nil, ErrInput
	}
	return manager, nil
}

func EnsureStream(ctx context.Context, manager StreamManager, desired jetstream.StreamConfig) (bool, error) {
	created, err := resultstream.Ensure(ctx, manager, desired)
	if err == resultstream.ErrInput {
		return false, ErrInput
	}
	return created, err
}
