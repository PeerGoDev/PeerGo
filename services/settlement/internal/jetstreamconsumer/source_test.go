package jetstreamconsumer

import (
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func TestValidateConsumerBindingRequiresOrderedConfirmedAckHeadroom(t *testing.T) {
	config := BindingConfig{
		Stream: "PEERGO_TRACKER_ANNOUNCE_V1", Subject: "peergo.tracker.announce.v1",
		Durable: "PEERGO_SETTLEMENT_V1", FetchWait: 2 * time.Second,
		MaximumProcessingTime: 10 * time.Second, MaximumAckTime: 5 * time.Second,
	}
	info := &jetstream.ConsumerInfo{
		Stream: config.Stream, Name: config.Durable,
		Config: jetstream.ConsumerConfig{
			Name: config.Durable, Durable: config.Durable,
			DeliverPolicy: jetstream.DeliverAllPolicy, AckPolicy: jetstream.AckExplicitPolicy,
			AckWait: 30 * time.Second, MaxDeliver: -1, FilterSubject: config.Subject,
			ReplayPolicy: jetstream.ReplayInstantPolicy, MaxAckPending: 1,
			MaxRequestBatch: 1, MaxRequestExpires: 5 * time.Second,
		},
	}
	if err := validateConsumerBinding(info, config); err != nil {
		t.Fatal(err)
	}
	info.Config.AckWait = 15 * time.Second
	if err := validateConsumerBinding(info, config); !errors.Is(err, ErrConsumerDrift) {
		t.Fatalf("insufficient ACK headroom error = %v", err)
	}
	info.Config.AckWait = 30 * time.Second
	info.Config.MaxAckPending = 2
	if err := validateConsumerBinding(info, config); !errors.Is(err, ErrConsumerDrift) {
		t.Fatalf("parallel consumer error = %v", err)
	}
}
