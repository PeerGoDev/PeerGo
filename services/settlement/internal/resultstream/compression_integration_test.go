package resultstream

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestIntegrationCompressionPreservesMessagesConsumerAndDedup(t *testing.T) {
	url := os.Getenv("PEERGO_TEST_NATS_URL")
	if url == "" {
		t.Skip("isolated PEERGO_TEST_NATS_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("COMPRESSION_TEST_%d", time.Now().UnixNano())
	subject := "test.compression." + name
	cfg := jetstream.StreamConfig{Name: name, Subjects: []string{subject}, Storage: jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy, Discard: jetstream.DiscardNew, MaxConsumers: 4,
		MaxMsgs: -1, MaxMsgsPerSubject: -1, MaxBytes: 1 << 30, MaxAge: 24 * time.Hour,
		MaxMsgSize: 1 << 20, Replicas: 1, Duplicates: time.Minute, Metadata: map[string]string{"test": "compression"}}
	stream, err := js.CreateStream(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = js.DeleteStream(context.Background(), name) }()
	payload := func(i int) []byte {
		return append(bytes.Repeat([]byte("canonical-evidence;"), 1000), []byte(fmt.Sprint(i))...)
	}
	for i := 1; i <= 256; i++ {
		if _, err := js.Publish(ctx, subject, payload(i), jetstream.WithMsgID(fmt.Sprint(i))); err != nil {
			t.Fatal(err)
		}
	}
	consumer, err := stream.CreateConsumer(ctx, jetstream.ConsumerConfig{Durable: "VERIFY", AckPolicy: jetstream.AckExplicitPolicy})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := consumer.Fetch(5, jetstream.FetchMaxWait(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for msg := range batch.Messages() {
		count++
		if err := msg.DoubleAck(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if batch.Error() != nil || count != 5 {
		t.Fatalf("fetch count=%d err=%v", count, batch.Error())
	}
	ci, err := consumer.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before, err := stream.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewNATSManager(js)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Compression = jetstream.S2Compression
	if created, err := Ensure(ctx, manager, cfg); err != nil || created {
		t.Fatalf("upgrade created=%v err=%v", created, err)
	}
	after, err := stream.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.State.Msgs != before.State.Msgs || after.State.FirstSeq != before.State.FirstSeq || after.State.LastSeq != before.State.LastSeq || after.Config.Compression != jetstream.S2Compression || after.Config.MaxAge != before.Config.MaxAge {
		t.Fatal("stream state or retention changed")
	}
	next, err := consumer.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ci.AckFloor.Consumer != next.AckFloor.Consumer || ci.AckFloor.Stream != next.AckFloor.Stream || ci.Delivered.Consumer != next.Delivered.Consumer || ci.Delivered.Stream != next.Delivered.Stream {
		t.Fatal("consumer cursor changed")
	}
	for i := 1; i <= 256; i++ {
		msg, err := stream.GetMsg(ctx, uint64(i))
		if err != nil || !bytes.Equal(msg.Data, payload(i)) {
			t.Fatalf("message %d changed: %v", i, err)
		}
	}
	ack, err := js.Publish(ctx, subject, payload(256), jetstream.WithMsgID("256"))
	if err != nil || !ack.Duplicate || ack.Sequence != 256 {
		t.Fatalf("dedup changed: %+v %v", ack, err)
	}
	batch, err = consumer.Fetch(1, jetstream.FetchMaxWait(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	count = 0
	for msg := range batch.Messages() {
		count++
		meta, err := msg.Metadata()
		if err != nil || meta.Sequence.Stream != 6 || !bytes.Equal(msg.Data(), payload(6)) {
			t.Fatal("consumer did not resume at message 6")
		}
		if err = msg.DoubleAck(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 || batch.Error() != nil {
		t.Fatal("consumer resume failed")
	}
}
