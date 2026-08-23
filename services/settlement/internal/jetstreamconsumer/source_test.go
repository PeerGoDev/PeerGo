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
		MaximumProcessingTime: 10 * time.Second, MaximumAckTime: 5 * time.Second, BatchSize: 64,
	}
	info := &jetstream.ConsumerInfo{
		Stream: config.Stream, Name: config.Durable,
		Config: jetstream.ConsumerConfig{
			Name: config.Durable, Durable: config.Durable,
			DeliverPolicy: jetstream.DeliverAllPolicy, AckPolicy: jetstream.AckExplicitPolicy,
			AckWait: 30 * time.Second, MaxDeliver: -1, FilterSubject: config.Subject,
			ReplayPolicy: jetstream.ReplayInstantPolicy, MaxAckPending: 64,
			MaxRequestBatch: 64, MaxRequestExpires: 5 * time.Second,
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
	info.Config.MaxAckPending = 32
	if err := validateConsumerBinding(info, config); !errors.Is(err, ErrConsumerDrift) {
		t.Fatalf("parallel consumer error = %v", err)
	}
}

func TestPendingRedeliveryDelayWaitsOneAckWindow(t *testing.T) {
	t.Parallel()
	info := &jetstream.ConsumerInfo{
		Config:        jetstream.ConsumerConfig{AckWait: 30 * time.Second},
		NumAckPending: 64,
	}
	if delay := pendingRedeliveryDelay(info); delay != 30*time.Second {
		t.Fatalf("pendingRedeliveryDelay() = %v, want 30s", delay)
	}
	info.NumAckPending = 0
	if delay := pendingRedeliveryDelay(info); delay != 0 {
		t.Fatalf("pendingRedeliveryDelay() without pending messages = %v, want 0", delay)
	}
}
