package kafka

import (
	"context"
	"errors"
	"testing"
	"time"

	"avito-ai-buddy/chat/internal/config"
	"avito-ai-buddy/chat/internal/port"

	ckafka "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"go.uber.org/zap"
)

type fakeBatchConsumer struct {
	events   []ckafka.Event
	assigned []ckafka.TopicPartition
	revoked  bool
}

func (c *fakeBatchConsumer) Poll(int) ckafka.Event {
	if len(c.events) == 0 {
		return nil
	}
	event := c.events[0]
	c.events = c.events[1:]
	return event
}

func (c *fakeBatchConsumer) Assign(partitions []ckafka.TopicPartition) error {
	c.assigned = partitions
	return nil
}

func (c *fakeBatchConsumer) Unassign() error {
	c.revoked = true
	return nil
}

func dispatchConfig() *config.KafkaConfig {
	cfg := &config.KafkaConfig{}
	cfg.BatchSize = 2
	cfg.PollTimeoutMs = 0
	cfg.RetryBaseBackoff = time.Millisecond
	cfg.RetryMaxBackoff = time.Millisecond
	cfg.MaxAttempts = 2
	cfg.DLQTopic = "task.status.changed-dlq"
	return cfg
}

func messageAt(topic string, partition int32, offset int64, value string) *ckafka.Message {
	return &ckafka.Message{
		TopicPartition: ckafka.TopicPartition{Topic: &topic, Partition: partition, Offset: ckafka.Offset(offset)},
		Value:          []byte(value),
	}
}

func TestPollBatchCollectsMessagesUntilBatchSize(t *testing.T) {
	topic := "task.status.changed"
	consumer := &fakeBatchConsumer{events: []ckafka.Event{
		messageAt(topic, 0, 1, `{"user_id":"7"}`),
		messageAt(topic, 0, 2, `{"user_id":"8"}`),
		messageAt(topic, 0, 3, `{"user_id":"9"}`),
	}}

	batch, ok := pollBatch(context.Background(), zap.NewNop(), dispatchConfig(), consumer)
	if !ok {
		t.Fatal("pollBatch() stopped the consumer, want it running")
	}
	if len(batch) != 2 {
		t.Fatalf("batch size = %d, want 2 (bounded by config)", len(batch))
	}
	if int64(batch[0].TopicPartition.Offset) != 1 || int64(batch[1].TopicPartition.Offset) != 2 {
		t.Fatalf("batch offsets = %d,%d, want 1,2",
			batch[0].TopicPartition.Offset, batch[1].TopicPartition.Offset)
	}
}

func TestPollBatchHandlesPartitionLifecycle(t *testing.T) {
	topic := "task.status.changed"
	consumer := &fakeBatchConsumer{events: []ckafka.Event{
		ckafka.AssignedPartitions{Partitions: []ckafka.TopicPartition{{Topic: &topic, Partition: 1}}},
		messageAt(topic, 1, 7, `{}`),
		ckafka.RevokedPartitions{},
	}}

	batch, ok := pollBatch(context.Background(), zap.NewNop(), dispatchConfig(), consumer)
	if !ok || len(batch) != 1 {
		t.Fatalf("pollBatch() = %d messages, ok %v, want 1, true", len(batch), ok)
	}
	if len(consumer.assigned) != 1 || consumer.assigned[0].Partition != 1 {
		t.Fatalf("assigned partitions = %v, want partition 1", consumer.assigned)
	}
	if !consumer.revoked {
		t.Fatal("Unassign() was not called on RevokedPartitions")
	}
}

func TestPollBatchStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	consumer := &fakeBatchConsumer{}

	batch, ok := pollBatch(ctx, zap.NewNop(), dispatchConfig(), consumer)
	if ok {
		t.Fatal("pollBatch() continued after context cancellation")
	}
	if len(batch) != 0 {
		t.Fatalf("batch = %d messages, want 0", len(batch))
	}
}

func TestProcessMessageDeliversValidPayload(t *testing.T) {
	ctx := context.Background()
	notifier := &recordingNotifier{}

	message := messageAt("task.status.changed", 0, 1, `{"user_id":"7","task_id":"task-42","status":"approved"}`)
	if !processMessageWithoutCommit(ctx, zap.NewNop(), dispatchConfig(), nil, nil, "", notifier, message) {
		t.Fatal("processMessageWithoutCommit() = false, want true for a valid payload")
	}
	if len(notifier.calls) != 1 || notifier.calls[0].TaskID != "task-42" {
		t.Fatalf("notifier calls = %+v, want single delivery of task-42", notifier.calls)
	}
}

func TestProcessMessageDiscardsMalformedPayloadWithDisabledDLQ(t *testing.T) {
	// A nil producer disables the DLQ writer: the message is discarded
	// without parking, but the batch still proceeds.
	notifier := &recordingNotifier{}

	message := messageAt("task.status.changed", 0, 1, `not-json`)
	if !processMessageWithoutCommit(context.Background(), zap.NewNop(), dispatchConfig(), nil, nil, "", notifier, message) {
		t.Fatal("processMessageWithoutCommit() = false, want true for a malformed payload")
	}
	if len(notifier.calls) != 0 {
		t.Fatal("notifier was called for a malformed payload")
	}
}

func TestProcessMessageExhaustsAttemptsAndProceedsWhenDLQDisabled(t *testing.T) {
	notifier := &recordingNotifier{errs: []error{
		errors.New("hub unavailable"),
		errors.New("hub unavailable"),
	}}

	message := messageAt("task.status.changed", 0, 1, `{"user_id":"7","task_id":"task-42","status":"approved"}`)
	if !processMessageWithoutCommit(context.Background(), zap.NewNop(), dispatchConfig(), nil, nil, "", notifier, message) {
		t.Fatal("processMessageWithoutCommit() = false, want true after attempts exhausted")
	}
	if len(notifier.calls) != 2 {
		t.Fatalf("notifier attempts = %d, want 2 (max attempts)", len(notifier.calls))
	}
}

func TestProcessMessageStopsBatchOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	notifier := &recordingNotifier{errs: []error{errors.New("hub unavailable")}}

	message := messageAt("task.status.changed", 0, 1, `{"user_id":"7","task_id":"task-42","status":"approved"}`)
	if processMessageWithoutCommit(ctx, zap.NewNop(), dispatchConfig(), nil, nil, "", notifier, message) {
		t.Fatal("processMessageWithoutCommit() = true, want false on cancelled context")
	}
}

func TestProcessBatchStopsAtFirstFailedMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	topic := "task.status.changed"
	notifier := &recordingNotifier{}
	batch := []*ckafka.Message{
		messageAt(topic, 0, 1, `{"user_id":"7","task_id":"task-42","status":"approved"}`),
		messageAt(topic, 0, 2, `{"user_id":"8","task_id":"task-43","status":"approved"}`),
	}

	if processBatchWithoutCommit(ctx, zap.NewNop(), dispatchConfig(), nil, nil, "", notifier, batch) {
		t.Fatal("processBatchWithoutCommit() = true, want false on cancelled context")
	}
}

func TestSendToDLQRequiresTopic(t *testing.T) {
	cfg := dispatchConfig()
	cfg.DLQTopic = ""
	producer := &fakeDLQWriter{}
	message := messageAt("task.status.changed", 0, 1, `bad`)

	err := sendToDLQ(context.Background(), cfg, producer, message, errors.New("bad payload"))
	if err == nil {
		t.Fatal("sendToDLQ() = nil, want error for an empty DLQ topic")
	}
	if len(producer.sent) != 0 {
		t.Fatal("producer was called without a DLQ topic")
	}
}

func TestSendToDLQAttachesFailureHeaders(t *testing.T) {
	cfg := dispatchConfig()
	producer := &fakeDLQWriter{}
	message := messageAt("task.status.changed", 3, 41, `bad`)
	message.Key = []byte("42")

	err := sendToDLQ(context.Background(), cfg, producer, message, errors.New("bad payload"))
	if err != nil {
		t.Fatalf("sendToDLQ() = %v, want nil", err)
	}
	if len(producer.sent) != 1 {
		t.Fatalf("produced %d messages, want 1", len(producer.sent))
	}
	sent := producer.sent[0]
	if sent.TopicPartition.Partition != ckafka.PartitionAny || string(sent.Value) != "bad" || string(sent.Key) != "42" {
		t.Fatalf("DLQ message = %+v, want original key/value on PartitionAny", sent.TopicPartition)
	}
	headers := map[string]string{}
	for _, header := range sent.Headers {
		headers[header.Key] = string(header.Value)
	}
	want := map[string]string{
		"x-original-topic":     "task.status.changed",
		"x-original-partition": "3",
		"x-original-offset":    "41",
		"x-error":              "bad payload",
	}
	for key, value := range want {
		if headers[key] != value {
			t.Fatalf("header %q = %q, want %q", key, headers[key], value)
		}
	}
}

func TestSendToDLQReportsUnexpectedDeliveryEvent(t *testing.T) {
	cfg := dispatchConfig()
	producer := &fakeDLQWriter{deliveries: []ckafka.Event{ckafka.NewError(ckafka.ErrUnknown, "not a message", false)}}
	message := messageAt("task.status.changed", 0, 1, `bad`)

	err := sendToDLQ(context.Background(), cfg, producer, message, errors.New("bad payload"))
	if err == nil {
		t.Fatal("sendToDLQ() = nil, want error for an unexpected delivery event")
	}
}

func TestSendToDLQReportsDeliveryFailure(t *testing.T) {
	cfg := dispatchConfig()
	deliveryErr := errors.New("broker unavailable")
	producer := &fakeDLQWriter{deliveries: []ckafka.Event{
		&ckafka.Message{TopicPartition: ckafka.TopicPartition{Error: deliveryErr}},
	}}
	message := messageAt("task.status.changed", 0, 1, `bad`)

	if err := sendToDLQ(context.Background(), cfg, producer, message, errors.New("bad payload")); !errors.Is(err, deliveryErr) {
		t.Fatalf("sendToDLQ() = %v, want %v", err, deliveryErr)
	}
}

type fakeDLQWriter struct {
	sent       []*ckafka.Message
	deliveries []ckafka.Event
}

func (w *fakeDLQWriter) Produce(msg *ckafka.Message, deliveryChan chan ckafka.Event) error {
	w.sent = append(w.sent, msg)
	if len(w.deliveries) > 0 {
		deliveryChan <- w.deliveries[0]
		w.deliveries = w.deliveries[1:]
		return nil
	}
	deliveryChan <- msg
	return nil
}

var _ port.TaskEventsNotifier = (*recordingNotifier)(nil)
