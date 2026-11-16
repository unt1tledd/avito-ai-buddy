package kafka

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"avito-ai-buddy/chat/internal/config"
	"avito-ai-buddy/chat/internal/port"

	ckafka "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"go.uber.org/zap"
)

type recordingNotifier struct {
	calls []port.TaskEvent
	errs  []error
}

func (n *recordingNotifier) PublishTaskStatus(_ context.Context, event port.TaskEvent) error {
	n.calls = append(n.calls, event)
	if len(n.calls) <= len(n.errs) {
		return n.errs[len(n.calls)-1]
	}
	return nil
}

func retryConfig(base, maxBackoff time.Duration, attempts int) *config.KafkaConfig {
	cfg := &config.KafkaConfig{}
	cfg.RetryBaseBackoff = base
	cfg.RetryMaxBackoff = maxBackoff
	cfg.MaxAttempts = attempts
	return cfg
}

func TestNotifyWithRetryWaitsExponentiallyBeforeFailing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const base = 100 * time.Millisecond
		notifier := &recordingNotifier{errs: []error{
			errors.New("hub unavailable"),
			errors.New("hub unavailable"),
			errors.New("hub unavailable"),
		}}
		event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
		started := time.Now()

		err := notifyWithRetry(context.Background(), zap.NewNop(), retryConfig(base, 10*time.Second, 3), notifier, event)
		if err == nil {
			t.Fatal("notify did not fail after exhausting attempts")
		}

		if len(notifier.calls) != 3 {
			t.Fatalf("attempts = %d, want 3", len(notifier.calls))
		}
		if notifier.calls[0] != event || notifier.calls[2] != event {
			t.Fatalf("attempts = %+v, want %+v", notifier.calls, event)
		}
		// Backoff doubles between attempts: 100ms then 200ms.
		want := []time.Duration{base, 200 * time.Millisecond}
		for i, gap := range want {
			if got := time.Since(started); got < gap {
				t.Fatalf("attempt %d happened too early: %v, want at least %v", i+1, got, gap)
			}
		}
		if got := time.Since(started); got < 300*time.Millisecond {
			t.Fatalf("total elapsed = %v, want at least 300ms", got)
		}
	})
}

func TestNotifyWithRetryCapsBackoffAtMaximum(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const base = 100 * time.Millisecond
		notifier := &recordingNotifier{errs: []error{
			errors.New("hub unavailable"),
			errors.New("hub unavailable"),
			errors.New("hub unavailable"),
		}}
		event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}
		started := time.Now()

		err := notifyWithRetry(context.Background(), zap.NewNop(), retryConfig(base, 150*time.Millisecond, 3), notifier, event)
		if err == nil {
			t.Fatal("notify did not fail after exhausting attempts")
		}

		// Second backoff would be 200ms without the cap, so the third
		// attempt must happen no later than 250ms after start.
		if got := time.Since(started); got > 250*time.Millisecond {
			t.Fatalf("third attempt at %v, want at most 250ms (backoff cap not applied)", got)
		}
	})
}

func TestNotifyWithRetrySucceedsAfterTransientFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		notifier := &recordingNotifier{errs: []error{errors.New("hub unavailable"), errors.New("hub unavailable")}}
		event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}

		err := notifyWithRetry(context.Background(), zap.NewNop(), retryConfig(time.Millisecond, 10*time.Millisecond, 3), notifier, event)
		if err != nil {
			t.Fatalf("notify failed after transient errors: %v", err)
		}
		if len(notifier.calls) != 3 {
			t.Fatalf("attempts = %d, want 3", len(notifier.calls))
		}
	})
}

func TestNotifyWithRetryStopsWhenContextIsCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		notifier := &recordingNotifier{errs: []error{errors.New("hub unavailable")}}
		event := port.TaskEvent{UserID: "7", TaskID: "task-42", Status: "approved"}

		err := notifyWithRetry(ctx, zap.NewNop(), retryConfig(time.Hour, time.Hour, 5), notifier, event)
		cancel()
		if err != nil {
			t.Fatalf("cancelled notify returned an error: %v", err)
		}
	})
}

func TestCommitOffsetsIncrementsPerPartition(t *testing.T) {
	topic := "task.status.changed"
	batch := []*ckafka.Message{
		{TopicPartition: ckafka.TopicPartition{Topic: &topic, Partition: 0, Offset: 10}},
		{TopicPartition: ckafka.TopicPartition{Topic: &topic, Partition: 0, Offset: 11}},
		{TopicPartition: ckafka.TopicPartition{Topic: &topic, Partition: 1, Offset: 5}},
	}

	offsets := commitOffsets(batch)
	if len(offsets) != 2 {
		t.Fatalf("offsets = %v, want one entry per partition", offsets)
	}
	seen := make(map[int32]int64, len(offsets))
	for _, offset := range offsets {
		if offset.Error != nil {
			t.Fatalf("offset %v carries an error", offset)
		}
		if offset.Topic == nil || *offset.Topic != topic {
			t.Fatalf("offset %v has wrong topic", offset)
		}
		seen[offset.Partition] = int64(offset.Offset)
	}
	if seen[0] != 12 || seen[1] != 6 {
		t.Fatalf("committed offsets = %v, want partition 0 -> 12 and partition 1 -> 6", seen)
	}
}

func TestCommitOffsetsKeepsHighestOffsetPerPartition(t *testing.T) {
	topic := "task.status.changed"
	batch := []*ckafka.Message{
		{TopicPartition: ckafka.TopicPartition{Topic: &topic, Partition: 2, Offset: 40}},
		{TopicPartition: ckafka.TopicPartition{Topic: &topic, Partition: 2, Offset: 7}},
	}

	offsets := commitOffsets(batch)
	if len(offsets) != 1 || int64(offsets[0].Offset) != 41 {
		got := make([]string, 0, len(offsets))
		for _, offset := range offsets {
			got = append(got, fmt.Sprintf("%d", offset.Offset))
		}
		t.Fatalf("offsets = [%s], want [41]", got)
	}
}
